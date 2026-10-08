package inventory

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
)

// 批號與效期(D63)。非批號管理的料品完全不受影響。
//
// 規則:
//   - 入庫:批號必填(效期管理的料品另須有效期);批號不存在就建立,已存在則效期必須一致;
//   - 出庫:指定批號就從該批號出;沒指定則「先到期先出」(沒有效期的排最後、同效期依建立順序);
//   - 批號庫存不可為負(即使倉庫允許負庫存);
//   - 出貨不可出已過期的批號,報廢(調整)、調撥與沖銷可以。

// posting 展開後的一筆實際分錄。
type posting struct {
	item, wh   int64
	qty        decimal.Decimal
	cost       *decimal.Decimal
	line       *int64
	reversalOf *int64
	lotID      *int64
	binID      *int64
}

type allocKey struct{ item, wh int64 }

type lotAvail struct {
	lotID  int64
	qty    decimal.Decimal
	lotNo  string
	expiry *time.Time
}

// NormalizeLotNo 批號統一為大寫、不含空白,最多 40 字元。
func NormalizeLotNo(s string) (string, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" || len(s) > 40 || strings.ContainsAny(s, " \t\r\n") {
		return s, errLotBadFormat
	}
	return s, nil
}

func lotControlled(it db.ListStockItemsRow) bool { return it.LotControl != "none" }

func itemLabel(it db.ListStockItemsRow) string { return it.Code + " " + it.Name }

// expand 把異動展開成逐批號的分錄。
func expand(ctx context.Context, q *db.Queries, opt Options, src Source, moves []Movement,
	items map[int64]db.ListStockItemsRow, whs map[int64]db.ListWarehouseFlagsRow) ([]posting, error) {
	var out []posting
	avail := map[allocKey][]lotAvail{} // 先到期先出的剩餘量(同一張單據內多行共用)
	jointAvail := map[allocKey][]lotBinAvail{}
	for _, m := range moves {
		if m.Qty.IsZero() {
			continue
		}
		it := items[m.ItemID]
		if !lotControlled(it) {
			out = append(out, posting{item: m.ItemID, wh: m.WarehouseID, qty: m.Qty, cost: m.UnitCost, line: m.SourceLineID, reversalOf: m.reversalOf, binID: m.binID})
			if m.TransferTo != nil {
				out = append(out, posting{item: m.ItemID, wh: *m.TransferTo, qty: m.Qty.Neg(), cost: m.UnitCost, line: m.SourceLineID, binID: m.toBinID})
			}
			continue
		}
		switch {
		case m.Qty.IsPositive():
			lotID := m.lotID
			if lotID == nil {
				id, err := resolveInboundLot(ctx, q, opt.CompanyID, it, m.LotNo, m.Expiry)
				if err != nil {
					return nil, err
				}
				lotID = &id
			}
			out = append(out, posting{item: m.ItemID, wh: m.WarehouseID, qty: m.Qty, cost: m.UnitCost, line: m.SourceLineID, reversalOf: m.reversalOf, lotID: lotID, binID: m.binID})
		default:
			parts, err := outboundParts(ctx, q, opt, src, m, it, whs[m.WarehouseID], avail, jointAvail)
			if err != nil {
				return nil, err
			}
			for _, p := range parts {
				lot := p.lotID
				out = append(out, posting{item: m.ItemID, wh: m.WarehouseID, qty: p.qty.Neg(), cost: m.UnitCost, line: m.SourceLineID, reversalOf: m.reversalOf, lotID: &lot, binID: p.binID})
				if m.TransferTo != nil {
					out = append(out, posting{item: m.ItemID, wh: *m.TransferTo, qty: p.qty, cost: m.UnitCost, line: m.SourceLineID, lotID: &lot, binID: m.toBinID})
				}
			}
		}
	}
	return out, nil
}

type part struct {
	lotID int64
	binID *int64          // 啟用儲位的倉庫:這一份來自哪個儲位
	qty   decimal.Decimal // 正數
}

// lotBinAvail 啟用儲位的倉庫:某批號在某儲位的剩餘量(先到期先出的聯合分配用)。
type lotBinAvail struct {
	lotID, binID int64
	qty          decimal.Decimal
	expiry       *time.Time
	lotNo, bin   string
}

// outboundParts 決定一筆出庫從哪些批號出:沖銷沿用原批號、指定批號、否則先到期先出。
func outboundParts(ctx context.Context, q *db.Queries, opt Options, src Source, m Movement, it db.ListStockItemsRow,
	wh db.ListWarehouseFlagsRow, avail map[allocKey][]lotAvail, jointAvail map[allocKey][]lotBinAvail) ([]part, error) {
	need := m.Qty.Neg()
	var onlyLot *int64 // 指定的批號
	switch {
	case m.lotID != nil:
		return []part{{*m.lotID, m.binID, need}}, nil
	case strings.TrimSpace(m.LotNo) != "":
		no, err := NormalizeLotNo(m.LotNo)
		if err != nil {
			return nil, err
		}
		lot, err := q.GetLotByNo(ctx, db.GetLotByNoParams{CompanyID: opt.CompanyID, ItemID: m.ItemID, LotNo: no})
		if err != nil {
			return nil, errLotUnknown.WithDetails(map[string]string{"item": itemLabel(it), "lot_no": no})
		}
		if !m.AllowExpired && expired(lot.ExpiryDate, src.DocDate) {
			return nil, errLotExpired.WithDetails(map[string]string{"item": itemLabel(it), "lot_no": no})
		}
		if !wh.UseBins {
			return []part{{lot.ID, nil, need}}, nil
		}
		onlyLot = &lot.ID
	}
	if wh.UseBins {
		return jointParts(ctx, q, src, m, it, wh, onlyLot, jointAvail)
	}
	k := allocKey{m.ItemID, m.WarehouseID}
	if _, ok := avail[k]; !ok {
		rows, err := q.ListLotBalancesForAllocation(ctx, db.ListLotBalancesForAllocationParams{ItemID: m.ItemID, WarehouseID: m.WarehouseID})
		if err != nil {
			return nil, err
		}
		list := make([]lotAvail, len(rows))
		for i, r := range rows {
			list[i] = lotAvail{lotID: r.LotID, qty: r.Qty, lotNo: r.LotNo, expiry: r.ExpiryDate}
		}
		avail[k] = list
	}
	var parts []part
	remain := need
	skipped := decimal.Zero // 因已過期而略過的庫存
	list := avail[k]
	for i := range list {
		if !remain.IsPositive() {
			break
		}
		if list[i].qty.IsZero() {
			continue
		}
		if !m.AllowExpired && expired(list[i].expiry, src.DocDate) {
			skipped = skipped.Add(list[i].qty)
			continue
		}
		take := decimal.Min(list[i].qty, remain)
		list[i].qty = list[i].qty.Sub(take)
		remain = remain.Sub(take)
		parts = append(parts, part{list[i].lotID, m.binID, take})
	}
	if remain.IsPositive() {
		msg := fmt.Sprintf("%s 在 %s 可出庫的批號庫存不足,需要 %s,還差 %s", itemLabel(it), wh.Name, need.String(), remain.String())
		if skipped.IsPositive() {
			msg += fmt.Sprintf("(另有已過期批號庫存 %s 不可出庫)", skipped.String())
		}
		return nil, errLotShort.WithMessage(msg).WithDetails(map[string]string{"item": itemLabel(it)})
	}
	return parts, nil
}

// jointParts 啟用儲位的倉庫:從(批號, 儲位)庫存聯合分配出庫。
// 指定批號只取該批號、指定儲位只取該儲位;都沒指定則先到期先出,同批號內庫存多的儲位先出。
// 同倉的儲位間調撥不會從目的儲位取貨。
func jointParts(ctx context.Context, q *db.Queries, src Source, m Movement, it db.ListStockItemsRow,
	wh db.ListWarehouseFlagsRow, onlyLot *int64, jointAvail map[allocKey][]lotBinAvail) ([]part, error) {
	need := m.Qty.Neg()
	k := allocKey{m.ItemID, m.WarehouseID}
	if _, ok := jointAvail[k]; !ok {
		rows, err := q.ListLotBinBalancesForAllocation(ctx, db.ListLotBinBalancesForAllocationParams{ItemID: m.ItemID, WarehouseID: m.WarehouseID})
		if err != nil {
			return nil, err
		}
		list := make([]lotBinAvail, len(rows))
		for i, r := range rows {
			list[i] = lotBinAvail{lotID: r.LotID, binID: r.BinID, qty: r.Qty, expiry: r.ExpiryDate, lotNo: r.LotNo, bin: r.BinCode}
		}
		jointAvail[k] = list
	}
	var parts []part
	remain := need
	skipped := decimal.Zero
	list := jointAvail[k]
	for i := range list {
		if !remain.IsPositive() {
			break
		}
		a := &list[i]
		switch {
		case a.qty.IsZero(),
			onlyLot != nil && a.lotID != *onlyLot,
			m.binID != nil && a.binID != *m.binID,
			m.TransferTo != nil && *m.TransferTo == m.WarehouseID && m.toBinID != nil && a.binID == *m.toBinID:
			continue
		}
		if onlyLot == nil && !m.AllowExpired && expired(a.expiry, src.DocDate) {
			skipped = skipped.Add(a.qty)
			continue
		}
		take := decimal.Min(a.qty, remain)
		a.qty = a.qty.Sub(take)
		remain = remain.Sub(take)
		bin := a.binID
		parts = append(parts, part{a.lotID, &bin, take})
	}
	if remain.IsPositive() {
		msg := fmt.Sprintf("%s 在 %s 可出庫的批號庫存不足,需要 %s,還差 %s", itemLabel(it), wh.Name, need.String(), remain.String())
		if m.binID != nil || onlyLot != nil {
			msg += "(已依指定的批號 / 儲位限縮範圍)"
		}
		if skipped.IsPositive() {
			msg += fmt.Sprintf("(另有已過期批號庫存 %s 不可出庫)", skipped.String())
		}
		return nil, errLotShort.WithMessage(msg).WithDetails(map[string]string{"item": itemLabel(it)})
	}
	return parts, nil
}

func expired(expiry *time.Time, on time.Time) bool {
	return expiry != nil && expiry.Before(on)
}

// resolveInboundLot 取得(必要時建立)入庫批號並檢查效期。
func resolveInboundLot(ctx context.Context, q *db.Queries, companyID int64, it db.ListStockItemsRow, lotNo string, expiry *time.Time) (int64, error) {
	if strings.TrimSpace(lotNo) == "" {
		return 0, errLotRequired.WithDetails(map[string]string{"item": itemLabel(it)})
	}
	no, err := NormalizeLotNo(lotNo)
	if err != nil {
		return 0, err
	}
	var exp *time.Time
	if it.LotControl == "lot_expiry" {
		exp = expiry
		if exp == nil { // 批號已存在時可省略效期(例如銷貨退回、再次進同一批)
			lot, err := q.GetLotByNo(ctx, db.GetLotByNoParams{CompanyID: companyID, ItemID: it.ID, LotNo: no})
			if err != nil || lot.ExpiryDate == nil {
				return 0, errExpiryRequired.WithDetails(map[string]string{"item": itemLabel(it), "lot_no": no})
			}
			return lot.ID, nil
		}
	}
	lot, err := q.FindOrCreateLot(ctx, db.FindOrCreateLotParams{CompanyID: companyID, ItemID: it.ID, LotNo: no, ExpiryDate: exp})
	if err != nil {
		return 0, err
	}
	if exp != nil && (lot.ExpiryDate == nil || !lot.ExpiryDate.Equal(*exp)) {
		have := "無"
		if lot.ExpiryDate != nil {
			have = lot.ExpiryDate.Format(time.DateOnly)
		}
		return 0, errLotExpiryClash.WithMessage(fmt.Sprintf("%s 的批號 %s 已存在,效期為 %s,與輸入的 %s 不同",
			itemLabel(it), no, have, exp.Format(time.DateOnly)))
	}
	return lot.ID, nil
}

type lotKey struct{ lot, wh int64 }

// applyLotBalances 更新批號現有量;批號庫存不可為負。
func applyLotBalances(ctx context.Context, q *db.Queries, opt Options, posts []posting,
	items map[int64]db.ListStockItemsRow, whs map[int64]db.ListWarehouseFlagsRow) error {
	net := map[lotKey]decimal.Decimal{}
	itemOf := map[int64]int64{}
	for _, p := range posts {
		if p.lotID == nil {
			continue
		}
		k := lotKey{*p.lotID, p.wh}
		net[k] = net[k].Add(p.qty)
		itemOf[*p.lotID] = p.item
	}
	if len(net) == 0 {
		return nil
	}
	ordered := make([]lotKey, 0, len(net))
	ids := make([]int64, 0, len(net))
	for k := range net {
		ordered = append(ordered, k)
		ids = append(ids, k.lot)
	}
	slices.SortFunc(ordered, func(a, b lotKey) int { return cmp.Or(cmp.Compare(a.lot, b.lot), cmp.Compare(a.wh, b.wh)) })
	lots, err := q.ListLotsByIDs(ctx, ids)
	if err != nil {
		return err
	}
	lotNo := map[int64]string{}
	for _, l := range lots {
		lotNo[l.ID] = l.LotNo
	}
	for _, k := range ordered {
		if net[k].IsZero() {
			continue
		}
		if err := q.EnsureLotBalance(ctx, db.EnsureLotBalanceParams{CompanyID: opt.CompanyID, ItemID: itemOf[k.lot], WarehouseID: k.wh, LotID: k.lot}); err != nil {
			return err
		}
		cur, err := q.LockLotBalance(ctx, db.LockLotBalanceParams{LotID: k.lot, WarehouseID: k.wh})
		if err != nil {
			return err
		}
		next := cur.Add(net[k])
		if next.IsNegative() {
			it := items[itemOf[k.lot]]
			return errLotShort.WithMessage(fmt.Sprintf("%s 批號 %s 在 %s 現有 %s,需要 %s",
				itemLabel(it), lotNo[k.lot], whs[k.wh].Name, cur.String(), net[k].Neg().String())).
				WithDetails(map[string]string{"item": itemLabel(it), "lot_no": lotNo[k.lot]})
		}
		if err := q.SetLotBalance(ctx, db.SetLotBalanceParams{Qty: next, LotID: k.lot, WarehouseID: k.wh}); err != nil {
			return err
		}
	}
	return nil
}

type lotBinKey struct{ lot, bin int64 }

// applyLotBinBalances 更新(批號, 儲位)現有量:批號管理料品在啟用儲位的倉庫,不可為負。
func applyLotBinBalances(ctx context.Context, q *db.Queries, opt Options, posts []posting,
	items map[int64]db.ListStockItemsRow, whs map[int64]db.ListWarehouseFlagsRow) error {
	net := map[lotBinKey]decimal.Decimal{}
	itemOf, whOf := map[int64]int64{}, map[int64]int64{}
	for _, p := range posts {
		if p.lotID == nil || p.binID == nil {
			continue
		}
		k := lotBinKey{*p.lotID, *p.binID}
		net[k] = net[k].Add(p.qty)
		itemOf[*p.lotID], whOf[*p.binID] = p.item, p.wh
	}
	if len(net) == 0 {
		return nil
	}
	ordered := make([]lotBinKey, 0, len(net))
	lotIDs, binIDs := make([]int64, 0, len(net)), make([]int64, 0, len(net))
	for k := range net {
		ordered = append(ordered, k)
		lotIDs, binIDs = append(lotIDs, k.lot), append(binIDs, k.bin)
	}
	slices.SortFunc(ordered, func(a, b lotBinKey) int { return cmp.Or(cmp.Compare(a.lot, b.lot), cmp.Compare(a.bin, b.bin)) })
	lots, err := q.ListLotsByIDs(ctx, lotIDs)
	if err != nil {
		return err
	}
	bins, err := q.ListBinsByIDs(ctx, binIDs)
	if err != nil {
		return err
	}
	lotNo, binCode := map[int64]string{}, map[int64]string{}
	for _, l := range lots {
		lotNo[l.ID] = l.LotNo
	}
	for _, b := range bins {
		binCode[b.ID] = b.Code
	}
	for _, k := range ordered {
		if net[k].IsZero() {
			continue
		}
		if err := q.EnsureLotBinBalance(ctx, db.EnsureLotBinBalanceParams{CompanyID: opt.CompanyID, ItemID: itemOf[k.lot], WarehouseID: whOf[k.bin], LotID: k.lot, BinID: k.bin}); err != nil {
			return err
		}
		cur, err := q.LockLotBinBalance(ctx, db.LockLotBinBalanceParams{LotID: k.lot, BinID: k.bin})
		if err != nil {
			return err
		}
		next := cur.Add(net[k])
		if next.IsNegative() {
			return errLotShort.WithMessage(fmt.Sprintf("%s 批號 %s 在儲位 %s 現有 %s,需要 %s",
				itemLabel(items[itemOf[k.lot]]), lotNo[k.lot], binCode[k.bin], cur.String(), net[k].Neg().String())).
				WithDetails(map[string]string{"lot_no": lotNo[k.lot], "bin": binCode[k.bin]})
		}
		if err := q.SetLotBinBalance(ctx, db.SetLotBinBalanceParams{Qty: next, LotID: k.lot, BinID: k.bin}); err != nil {
			return err
		}
	}
	return nil
}

// LotInput 單據明細上輸入的批號資料,供開單時檢查。
type LotInput struct {
	ItemID  int64
	LotNo   string
	Expiry  *time.Time
	Inbound bool // 入庫(進貨、銷貨退回、調整增加)批號必填;出庫可空白(先到期先出)
}

// LotChecked 檢查並整理後的批號與效期(沒有批號管理的料品一律為空)。
type LotChecked struct {
	LotNo  string
	Expiry *time.Time
}

// CheckLotInputs 開單時檢查批號資料:回傳整理後的內容(與輸入依索引對應)與各行的錯誤訊息(索引 → 訊息)。
//   - 沒有批號管理的料品不可填批號;
//   - 入庫:批號必填;效期管理的料品須有效期,但批號已存在時可省略(沿用該批號的效期);
//   - 出庫:批號可空白;有填則統一格式;
//   - 效期只對「批號與效期管理」的料品有意義,其餘清空。
func CheckLotInputs(ctx context.Context, q *db.Queries, companyID int64, in []LotInput) ([]LotChecked, map[int]string, error) {
	out := make([]LotChecked, len(in))
	errs := map[int]string{}
	ids := make([]int64, 0, len(in))
	for _, l := range in {
		ids = append(ids, l.ItemID)
	}
	items, err := q.ListStockItems(ctx, db.ListStockItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return nil, nil, err
	}
	byID := map[int64]db.ListStockItemsRow{}
	for _, it := range items {
		byID[it.ID] = it
	}
	for i, l := range in {
		it, ok := byID[l.ItemID]
		if !ok || !lotControlled(it) {
			if strings.TrimSpace(l.LotNo) != "" && ok && it.ItemType == "goods" {
				errs[i] = "這個料品沒有啟用批號管理,不能輸入批號"
			}
			continue // 服務類或不存在的料品由其他檢查處理
		}
		if strings.TrimSpace(l.LotNo) == "" {
			if l.Inbound {
				errs[i] = "批號管理的料品須輸入批號"
			}
			continue
		}
		no, err := NormalizeLotNo(l.LotNo)
		if err != nil {
			errs[i] = "批號不可含空白,最多 40 個字元"
			continue
		}
		exp := l.Expiry
		if it.LotControl == "lot" || !l.Inbound {
			exp = nil
		}
		if l.Inbound && it.LotControl == "lot_expiry" && exp == nil {
			lot, err := q.GetLotByNo(ctx, db.GetLotByNoParams{CompanyID: companyID, ItemID: it.ID, LotNo: no})
			if err != nil || lot.ExpiryDate == nil {
				errs[i] = "效期管理的料品須輸入效期(新批號)"
				continue
			}
		}
		out[i] = LotChecked{LotNo: no, Expiry: exp}
	}
	return out, errs, nil
}
