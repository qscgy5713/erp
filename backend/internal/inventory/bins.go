package inventory

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/shared/apperr"
)

// 儲位(D65)。倉庫啟用儲位(use_bins)後:
//   - 入庫(含調撥的目的倉)須指定儲位;
//   - 出庫可指定儲位,留空則「庫存多的儲位先出」自動分配,不足時可拆到多個儲位;
//   - 儲位庫存不可為負(即使倉庫允許負庫存);
//   - 沒有啟用儲位的倉庫不可指定儲位。
// 批號管理的料品在啟用儲位的倉庫,批號與儲位一起記錄(inventory_lot_bin_balances,D70):
// 出庫可指定批號、儲位或兩者,未指定的部分由 expand 聯合分配(先到期先出、同批號內庫存多的儲位先出)。
// 盤點:每個有庫存的(料品, 儲位)或(料品, 批號, 儲位)一行。

var (
	errBinNotUsed  = apperr.New(http.StatusUnprocessableEntity, "INV-016", "這個倉庫沒有啟用儲位,不能指定儲位")
	errBinRequired = apperr.New(http.StatusUnprocessableEntity, "INV-017", "這個倉庫有啟用儲位,入庫須指定儲位")
	errBinShort    = apperr.New(http.StatusUnprocessableEntity, "INV-018", "儲位庫存不足")
	errBinUnknown  = apperr.New(http.StatusUnprocessableEntity, "INV-019", "找不到這個儲位,或儲位已停用")
	errBinSameMove = apperr.New(http.StatusUnprocessableEntity, "INV-020", "同一倉庫的調撥須指定不同的來源與目的儲位")
)

// NormalizeBinCode 儲位代號統一為大寫。
func NormalizeBinCode(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

type binAvail struct {
	id  int64
	qty decimal.Decimal
}

func resolveBin(ctx context.Context, q *db.Queries, companyID, whID int64, code string) (int64, error) {
	b, err := q.GetBinByCode(ctx, db.GetBinByCodeParams{CompanyID: companyID, WarehouseID: whID, Code: NormalizeBinCode(code)})
	if err != nil || !b.IsActive {
		return 0, errBinUnknown.WithDetails(map[string]string{"bin": NormalizeBinCode(code)})
	}
	return b.ID, nil
}

// expandBins 把異動依儲位拆開:啟用儲位的倉庫的入庫要有儲位,出庫依指定或自動分配。
// 沒有啟用儲位的倉庫維持原樣;沖銷分錄(已帶 binID)不處理。
func expandBins(ctx context.Context, q *db.Queries, opt Options, moves []Movement, items map[int64]db.ListStockItemsRow,
	whs map[int64]db.ListWarehouseFlagsRow) ([]Movement, error) {
	var out []Movement
	avail := map[allocKey][]binAvail{}
	for _, m := range moves {
		if m.Qty.IsZero() {
			continue
		}
		src := whs[m.WarehouseID]
		var toID *int64
		if m.TransferTo != nil {
			dst := whs[*m.TransferTo]
			switch {
			case dst.UseBins && strings.TrimSpace(m.ToBinCode) == "":
				return nil, errBinRequired.WithDetails(map[string]string{"warehouse": dst.Code})
			case !dst.UseBins && strings.TrimSpace(m.ToBinCode) != "":
				return nil, errBinNotUsed.WithDetails(map[string]string{"warehouse": dst.Code})
			case dst.UseBins:
				id, err := resolveBin(ctx, q, opt.CompanyID, dst.ID, m.ToBinCode)
				if err != nil {
					return nil, err
				}
				toID = &id
			}
		}
		if m.binID != nil || (!src.UseBins && strings.TrimSpace(m.BinCode) == "") {
			m.toBinID = toID
			out = append(out, m)
			continue
		}
		if !src.UseBins {
			return nil, errBinNotUsed.WithDetails(map[string]string{"warehouse": src.Code})
		}
		it := items[m.ItemID]
		switch {
		case m.Qty.IsPositive():
			if strings.TrimSpace(m.BinCode) == "" {
				return nil, errBinRequired.WithDetails(map[string]string{"warehouse": src.Code, "item": itemLabel(it)})
			}
			id, err := resolveBin(ctx, q, opt.CompanyID, src.ID, m.BinCode)
			if err != nil {
				return nil, err
			}
			m.binID, m.toBinID = &id, toID
			out = append(out, m)
		case strings.TrimSpace(m.BinCode) != "":
			id, err := resolveBin(ctx, q, opt.CompanyID, src.ID, m.BinCode)
			if err != nil {
				return nil, err
			}
			if toID != nil && src.ID == *m.TransferTo && id == *toID {
				return nil, errBinSameMove
			}
			m.binID, m.toBinID = &id, toID
			out = append(out, m)
		case lotControlled(it):
			// 批號管理的料品:儲位與批號一起分配(expand 的聯合分配,先到期先出、同批號內庫存多的儲位先出)
			m.toBinID = toID
			out = append(out, m)
		default:
			k := allocKey{m.ItemID, m.WarehouseID}
			if _, ok := avail[k]; !ok {
				rows, err := q.ListBinBalancesForAllocation(ctx, db.ListBinBalancesForAllocationParams{ItemID: m.ItemID, WarehouseID: m.WarehouseID})
				if err != nil {
					return nil, err
				}
				list := make([]binAvail, len(rows))
				for i, r := range rows {
					list[i] = binAvail{r.BinID, r.Qty}
				}
				avail[k] = list
			}
			remain := m.Qty.Neg()
			list := avail[k]
			for i := range list {
				if !remain.IsPositive() {
					break
				}
				take := decimal.Min(list[i].qty, remain)
				if !take.IsPositive() {
					continue
				}
				list[i].qty = list[i].qty.Sub(take)
				remain = remain.Sub(take)
				part := m
				part.Qty = take.Neg()
				id := list[i].id
				part.binID, part.toBinID = &id, toID
				out = append(out, part)
			}
			if remain.IsPositive() {
				return nil, errBinShort.WithMessage(fmt.Sprintf("%s 在 %s 的儲位庫存不足,需要 %s,還差 %s",
					itemLabel(it), src.Name, m.Qty.Neg().String(), remain.String()))
			}
		}
	}
	return out, nil
}

type binItemKey struct{ bin, item int64 }

// applyBinBalances 更新儲位現有量;儲位庫存不可為負。
func applyBinBalances(ctx context.Context, q *db.Queries, opt Options, posts []posting, items map[int64]db.ListStockItemsRow) error {
	net := map[binItemKey]decimal.Decimal{}
	whOf := map[int64]int64{}
	for _, p := range posts {
		if p.binID == nil {
			continue
		}
		k := binItemKey{*p.binID, p.item}
		net[k] = net[k].Add(p.qty)
		whOf[*p.binID] = p.wh
	}
	if len(net) == 0 {
		return nil
	}
	ordered := make([]binItemKey, 0, len(net))
	ids := make([]int64, 0, len(net))
	for k := range net {
		ordered = append(ordered, k)
		ids = append(ids, k.bin)
	}
	slices.SortFunc(ordered, func(a, b binItemKey) int { return cmp.Or(cmp.Compare(a.bin, b.bin), cmp.Compare(a.item, b.item)) })
	bins, err := q.ListBinsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	code := map[int64]string{}
	for _, b := range bins {
		code[b.ID] = b.Code
	}
	for _, k := range ordered {
		if net[k].IsZero() {
			continue
		}
		if err := q.EnsureBinBalance(ctx, db.EnsureBinBalanceParams{CompanyID: opt.CompanyID, ItemID: k.item, WarehouseID: whOf[k.bin], BinID: k.bin}); err != nil {
			return err
		}
		cur, err := q.LockBinBalance(ctx, db.LockBinBalanceParams{BinID: k.bin, ItemID: k.item})
		if err != nil {
			return err
		}
		next := cur.Add(net[k])
		if next.IsNegative() {
			return errBinShort.WithMessage(fmt.Sprintf("%s 在儲位 %s 現有 %s,需要 %s", itemLabel(items[k.item]), code[k.bin], cur.String(), net[k].Neg().String())).
				WithDetails(map[string]string{"bin": code[k.bin]})
		}
		if err := q.SetBinBalance(ctx, db.SetBinBalanceParams{Qty: next, BinID: k.bin, ItemID: k.item}); err != nil {
			return err
		}
	}
	return nil
}

// BinInput 單據明細上輸入的儲位,供開單時檢查。
type BinInput struct {
	ItemID      int64
	WarehouseID int64
	BinCode     string
	Inbound     bool // 入庫:啟用儲位的倉庫須指定;出庫可留空
	// 調撥的目的倉庫與儲位(只有調撥使用)
	ToWarehouseID *int64
	ToBinCode     string
}

// CheckBinInputs 開單時檢查儲位:回傳整理後(大寫)的代號與各行的錯誤訊息(索引 → 訊息)。
// 檢查倉庫是否啟用儲位、儲位是否存在且啟用、入庫是否漏填;實際庫存是否足夠留給過帳時判斷。
func CheckBinInputs(ctx context.Context, q *db.Queries, companyID int64, in []BinInput) (codes, toCodes []string, errs map[int]string, err error) {
	codes, toCodes, errs = make([]string, len(in)), make([]string, len(in)), map[int]string{}
	ids := map[int64]bool{}
	for _, l := range in {
		ids[l.WarehouseID] = true
		if l.ToWarehouseID != nil {
			ids[*l.ToWarehouseID] = true
		}
	}
	idList := make([]int64, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: companyID, Ids: idList})
	if err != nil {
		return nil, nil, nil, err
	}
	use := map[int64]bool{}
	for _, w := range whs {
		use[w.ID] = w.UseBins
	}
	check := func(i int, wh int64, raw string, required bool, field string) string {
		code := NormalizeBinCode(raw)
		switch {
		case !use[wh] && code != "":
			errs[i] = "這個倉庫沒有啟用儲位,不能指定" + field
		case use[wh] && code == "" && required:
			errs[i] = "這個倉庫有啟用儲位,請指定" + field
		case use[wh] && code != "":
			if _, err := resolveBin(ctx, q, companyID, wh, code); err != nil {
				errs[i] = field + "「" + code + "」不存在或已停用"
			}
		}
		return code
	}
	for i, l := range in {
		codes[i] = check(i, l.WarehouseID, l.BinCode, l.Inbound, "儲位")
		if l.ToWarehouseID != nil {
			toCodes[i] = check(i, *l.ToWarehouseID, l.ToBinCode, true, "目的儲位")
			if _, bad := errs[i]; !bad && *l.ToWarehouseID == l.WarehouseID && codes[i] == toCodes[i] {
				errs[i] = "同一倉庫的調撥須指定不同的來源與目的儲位"
			}
		}
	}
	return codes, toCodes, errs, nil
}
