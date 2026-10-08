// Package inventory 為庫存核心:寫入流水帳、維護現有量,以及庫存單據(調整、調撥、盤點)。
// Post / Reverse 供各模組(進貨、出貨…)在自己的過帳交易內呼叫,庫存規則只寫在這裡。
package inventory

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/gl"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
)

var (
	errCostClosed       = apperr.New(http.StatusConflict, "INV-008", "該月份已月結成本,不可異動庫存;如需更正請先取消月結")
	errNotStockItem     = apperr.New(http.StatusUnprocessableEntity, "INV-002", "服務類料品不能有庫存異動")
	errBadWarehouse     = apperr.New(http.StatusUnprocessableEntity, "INV-003", "倉庫不存在或已停用")
	errNothingToPost    = apperr.New(http.StatusUnprocessableEntity, "INV-004", "沒有可過帳的庫存異動")
	errItemNotFound     = apperr.New(http.StatusUnprocessableEntity, "INV-006", "料品不存在")
	ErrNothingToReverse = apperr.New(http.StatusConflict, "INV-007", "找不到可沖銷的庫存分錄")
	errLotRequired      = apperr.New(http.StatusUnprocessableEntity, "INV-009", "批號管理的料品須輸入批號")
	errExpiryRequired   = apperr.New(http.StatusUnprocessableEntity, "INV-010", "效期管理的料品須輸入效期")
	errLotExpiryClash   = apperr.New(http.StatusConflict, "INV-011", "批號已存在,效期與輸入不同")
	errLotShort         = apperr.New(http.StatusUnprocessableEntity, "INV-012", "批號庫存不足")
	errLotExpired       = apperr.New(http.StatusUnprocessableEntity, "INV-013", "批號已過期,不可出庫")
	errLotUnknown       = apperr.New(http.StatusUnprocessableEntity, "INV-014", "找不到這個批號")
	errLotBadFormat     = apperr.New(http.StatusUnprocessableEntity, "INV-015", "批號不可含空白,最多 40 個字元")
)

// Source 異動來源單據。
type Source struct {
	Type    string // 例:stock_adjustment、goods_receipt
	ID      int64
	No      string
	DocDate time.Time
}

// Movement 一筆庫存異動(基本單位,正入負出)。
type Movement struct {
	ItemID       int64
	WarehouseID  int64
	Qty          decimal.Decimal
	UnitCost     *decimal.Decimal
	SourceLineID *int64

	// 批號管理的料品:入庫須填批號(效期管理的料品另須填效期);出庫可指定批號,
	// 空白則依「先到期先出」自動分配,可能拆成多筆分錄。非批號管理的料品忽略這兩個欄位。
	LotNo  string
	Expiry *time.Time
	// TransferTo 調撥:出庫量(Qty 須為負)同時入到這個倉庫,沿用相同批號。
	TransferTo *int64
	// AllowExpired 允許出庫已過期的批號(報廢、調整、調撥、沖銷);出貨不允許。
	AllowExpired bool

	// 儲位:倉庫有啟用儲位時,入庫須指定;出庫可指定,留空自動分配(庫存多的儲位先出)。
	// ToBinCode 為調撥目的倉庫的儲位(目的倉有啟用儲位時必填);同一倉庫的調撥來源與目的儲位須不同。
	BinCode   string
	ToBinCode string

	reversalOf *int64
	lotID      *int64 // 沖銷時沿用原分錄的批號
	binID      *int64 // 沖銷時沿用原分錄的儲位;一般異動由 expandBins 決定
	toBinID    *int64
}

// Options 過帳選項。
type Options struct {
	CompanyID int64
	ActorID   *int64
	// ExcludeCountDocID 盤點單自身過帳時排除自己,不被自己的盤點凍結擋下
	ExcludeCountDocID int64
}

type balanceKey struct{ item, warehouse int64 }

// Post 寫入庫存異動並更新現有量。須在呼叫端的交易內執行。
func Post(ctx context.Context, q *db.Queries, opt Options, src Source, moves []Movement) error {
	if len(moves) == 0 {
		return errNothingToPost
	}
	return apply(ctx, q, opt, src, moves)
}

// Reverse 以反向分錄沖銷某張單據尚未被沖銷的全部分錄(反過帳)。
func Reverse(ctx context.Context, q *db.Queries, opt Options, src Source) error {
	txs, err := q.ListOpenTransactionsBySource(ctx, db.ListOpenTransactionsBySourceParams{SourceType: src.Type, SourceID: src.ID})
	if err != nil {
		return err
	}
	if len(txs) == 0 {
		return ErrNothingToReverse
	}
	moves := make([]Movement, len(txs))
	for i, t := range txs {
		moves[i] = Movement{
			ItemID: t.ItemID, WarehouseID: t.WarehouseID, Qty: t.Qty.Neg(), UnitCost: t.UnitCost,
			SourceLineID: t.SourceLineID, reversalOf: &t.ID, lotID: t.LotID, binID: t.BinID, AllowExpired: true,
		}
	}
	// 沖銷分錄沿用原單據日期,收發存報表才會在同一期間互相抵銷
	src.DocDate = txs[0].DocDate
	return apply(ctx, q, opt, src, moves)
}

func apply(ctx context.Context, q *db.Queries, opt Options, src Source, moves []Movement) error {
	// 會計期間已關帳時不可異動庫存(與單據的傳票拋轉共用同一規則)
	if err := gl.CheckPeriodOpen(ctx, q, opt.CompanyID, src.DocDate); err != nil {
		return err
	}
	// 該月已月結成本時不可異動庫存(成本已定案;需先取消月結)
	if closed, err := q.CostClosingExists(ctx, db.CostClosingExistsParams{CompanyID: opt.CompanyID, Period: src.DocDate.Format("2006-01")}); err != nil {
		return err
	} else if closed {
		return errCostClosed.WithDetails(map[string]string{"period": src.DocDate.Format("2006-01")})
	}
	itemIDs, whIDs := map[int64]bool{}, map[int64]bool{}
	net := map[balanceKey]decimal.Decimal{}
	for _, m := range moves {
		if m.Qty.IsZero() {
			continue
		}
		itemIDs[m.ItemID] = true
		whIDs[m.WarehouseID] = true
		k := balanceKey{m.ItemID, m.WarehouseID}
		net[k] = net[k].Add(m.Qty)
		if m.TransferTo != nil { // 調撥:目的倉同時增加
			if !m.Qty.IsNegative() {
				return fmt.Errorf("inventory: 調撥異動的數量必須為負")
			}
			whIDs[*m.TransferTo] = true
			kt := balanceKey{m.ItemID, *m.TransferTo}
			net[kt] = net[kt].Sub(m.Qty)
		}
	}
	if len(net) == 0 {
		return errNothingToPost
	}

	items, err := q.ListStockItems(ctx, db.ListStockItemsParams{CompanyID: opt.CompanyID, Ids: keys(itemIDs)})
	if err != nil {
		return err
	}
	itemByID := map[int64]db.ListStockItemsRow{}
	for _, it := range items {
		if it.ItemType != "goods" {
			return errNotStockItem.WithDetails(map[string]string{"item": it.Code + " " + it.Name})
		}
		itemByID[it.ID] = it
	}
	if len(itemByID) != len(itemIDs) {
		return errItemNotFound
	}

	whs, err := q.ListWarehouseFlags(ctx, db.ListWarehouseFlagsParams{CompanyID: opt.CompanyID, Ids: keys(whIDs)})
	if err != nil {
		return err
	}
	whByID := map[int64]db.ListWarehouseFlagsRow{}
	for _, w := range whs {
		if !w.IsActive {
			return errBadWarehouse.WithDetails(map[string]string{"warehouse": w.Code + " " + w.Name})
		}
		whByID[w.ID] = w
	}
	if len(whByID) != len(whIDs) {
		return errBadWarehouse
	}

	// 盤點凍結:倉庫有進行中的盤點單時不可異動
	countNo, err := q.OpenCountDocNo(ctx, db.OpenCountDocNoParams{
		CompanyID: opt.CompanyID, WarehouseIds: keys(whIDs), ExcludeID: opt.ExcludeCountDocID,
	})
	if err == nil {
		return apperr.New(http.StatusConflict, "INV-005",
			fmt.Sprintf("倉庫盤點中(盤點單 %s),盤點完成或作廢前不可異動庫存", countNo))
	}
	if !database.IsNoRows(err) {
		return err
	}

	// 依 (料品, 倉庫) 排序後逐一鎖定,所有交易的上鎖順序一致,避免死結
	ordered := make([]balanceKey, 0, len(net))
	for k := range net {
		ordered = append(ordered, k)
	}
	slices.SortFunc(ordered, func(a, b balanceKey) int {
		return cmp.Or(cmp.Compare(a.item, b.item), cmp.Compare(a.warehouse, b.warehouse))
	})

	newQty := make(map[balanceKey]decimal.Decimal, len(ordered))
	var shortages []string
	details := map[string]string{}
	for _, k := range ordered {
		if err := q.EnsureBalance(ctx, db.EnsureBalanceParams{CompanyID: opt.CompanyID, ItemID: k.item, WarehouseID: k.warehouse}); err != nil {
			return err
		}
		cur, err := q.LockBalance(ctx, db.LockBalanceParams{ItemID: k.item, WarehouseID: k.warehouse})
		if err != nil {
			return err
		}
		next := cur.Add(net[k])
		w := whByID[k.warehouse]
		// 只擋「減少且結果為負」;本來就是負的再增加不擋
		if net[k].IsNegative() && next.IsNegative() && !w.AllowNegative {
			it := itemByID[k.item]
			msg := fmt.Sprintf("%s %s 在 %s 現有 %s,需要 %s", it.Code, it.Name, w.Name, cur.String(), net[k].Neg().String())
			shortages = append(shortages, msg)
			details[it.Code+"@"+w.Code] = msg
		}
		newQty[k] = next
	}
	if len(shortages) > 0 {
		return apperr.New(http.StatusUnprocessableEntity, "INV-001", "庫存不足:"+strings.Join(shortages, ";")).WithDetails(details)
	}

	// 批號:展開成逐批號的分錄(先到期先出、調撥沿用批號),再更新批號現有量。
	// (料品, 倉庫) 的鎖已取得,批號列的鎖在其後取得,上鎖順序一致
	moves, err = expandBins(ctx, q, opt, moves, itemByID, whByID)
	if err != nil {
		return err
	}
	posts, err := expand(ctx, q, opt, src, moves, itemByID, whByID)
	if err != nil {
		return err
	}
	if err := applyBinBalances(ctx, q, opt, posts, itemByID); err != nil {
		return err
	}
	if err := applyLotBalances(ctx, q, opt, posts, itemByID, whByID); err != nil {
		return err
	}
	if err := applyLotBinBalances(ctx, q, opt, posts, itemByID, whByID); err != nil {
		return err
	}

	for _, k := range ordered {
		if err := q.SetBalance(ctx, db.SetBalanceParams{Qty: newQty[k], ItemID: k.item, WarehouseID: k.warehouse}); err != nil {
			return err
		}
	}
	for _, p := range posts {
		if _, err := q.InsertInventoryTransaction(ctx, db.InsertInventoryTransactionParams{
			CompanyID: opt.CompanyID, ItemID: p.item, WarehouseID: p.wh, DocDate: src.DocDate,
			Qty: p.qty, UnitCost: p.cost, SourceType: src.Type, SourceID: src.ID,
			SourceLineID: p.line, SourceNo: src.No, ReversalOf: p.reversalOf, CreatedBy: opt.ActorID, LotID: p.lotID, BinID: p.binID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func keys(m map[int64]bool) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
