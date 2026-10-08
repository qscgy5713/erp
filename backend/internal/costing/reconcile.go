package costing

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/response"
)

// 對帳檢查狀態
const (
	statusOK    = "ok"
	statusError = "error"
	statusWarn  = "warn"
)

type checkDTO struct {
	Key       string           `json:"key"`
	Label     string           `json:"label"`
	Status    string           `json:"status"`
	Subledger *decimal.Decimal `json:"subledger"` // 子帳 / 計算值
	GL        *decimal.Decimal `json:"gl"`        // 總帳 / 對照值
	Diff      *decimal.Decimal `json:"diff"`
	Message   string           `json:"message"`
}

func amountCheck(key, label string, sub, ledger, tolerance decimal.Decimal, hint string) checkDTO {
	diff := sub.Sub(ledger)
	c := checkDTO{Key: key, Label: label, Status: statusOK, Subledger: &sub, GL: &ledger, Diff: &diff}
	if diff.Abs().GreaterThan(tolerance) {
		c.Status = statusError
		c.Message = hint
	}
	return c
}

func countCheck(key, label string, n int64, hint string) checkDTO {
	c := checkDTO{Key: key, Label: label, Status: statusOK}
	if n > 0 {
		c.Status = statusError
		c.Message = fmt.Sprintf("%d 筆異常。%s", n, hint)
	}
	return c
}

// mappedAccount 取得拋轉規則對應的科目。
func mappedAccount(ctx context.Context, q *db.Queries, companyID int64, key string) (int64, error) {
	rows, err := q.ListAccountMappings(ctx, companyID)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if r.MapKey == key {
			return r.AccountID, nil
		}
	}
	return 0, fmt.Errorf("尚未設定拋轉規則 %s", key)
}

// reconcile GET /costing/reconcile 自動對帳檢查:
//   - 存貨:最近一次月結的期末存貨金額 vs 存貨科目在該月底的總帳餘額
//   - 應收 / 應付:未沖餘額子帳(本位幣)vs 對應科目的總帳餘額
//   - 庫存現有量 vs 流水帳合計、已過帳傳票借貸平衡
//   - 提醒尚未月結成本的月份
//
// 差異容許每筆帳款 / 料品 1 元的四捨五入;M6 之前已過帳、沒有傳票的單據會造成差異(匯入期初餘額後消除)。
func (m *Module) reconcile(c *gin.Context) {
	ctx := c.Request.Context()
	companyID := actor(c).CompanyID
	q := m.store.Queries
	today := time.Now().In(tst)
	checks := []checkDTO{}

	// 存貨
	inv := checkDTO{Key: "inventory", Label: "存貨:月結期末金額 vs 總帳存貨科目", Status: statusWarn, Message: "尚未月結過成本,無法核對"}
	if latest, err := q.LatestCostClosing(ctx, companyID); err == nil {
		acct, err := mappedAccount(ctx, q, companyID, "cost.inventory")
		if err != nil {
			response.Error(c, err)
			return
		}
		_, end := monthRange(latest.Period)
		bal, err := q.GLBalanceAsOf(ctx, db.GLBalanceAsOfParams{CompanyID: companyID, AccountID: acct, AsOf: end})
		if err != nil {
			response.Error(c, err)
			return
		}
		inv = amountCheck("inventory", fmt.Sprintf("存貨:%s 月結期末金額 vs 總帳存貨科目", latest.Period),
			latest.InventoryValue, bal, decimal.NewFromInt(int64(max(latest.ItemCount, 1))),
			"差異超過四捨五入容許範圍。可能是月結前已過帳、沒有傳票的進貨單,或手動傳票直接調整了存貨科目")
	} else if !database.IsNoRows(err) {
		response.Error(c, err)
		return
	}
	checks = append(checks, inv)

	// 應收、應付
	for _, s := range []struct {
		key, label, mapKey string
		sign               int64
		sub                func(context.Context, int64) (decimal.Decimal, error)
	}{
		{"receivable", "應收帳款:未沖餘額子帳 vs 總帳", "sales.receivable", 1, q.ReceivableBaseBalance},
		{"payable", "應付帳款:未沖餘額子帳 vs 總帳", "purchase.payable", -1, q.PayableBaseBalance},
	} {
		sub, err := s.sub(ctx, companyID)
		if err != nil {
			response.Error(c, err)
			return
		}
		acct, err := mappedAccount(ctx, q, companyID, s.mapKey)
		if err != nil {
			response.Error(c, err)
			return
		}
		bal, err := q.GLBalanceAsOf(ctx, db.GLBalanceAsOfParams{CompanyID: companyID, AccountID: acct, AsOf: today})
		if err != nil {
			response.Error(c, err)
			return
		}
		checks = append(checks, amountCheck(s.key, s.label, sub, bal.Mul(decimal.NewFromInt(s.sign)), decimal.NewFromInt(1),
			"差異可能來自 M6 之前已過帳、沒有傳票的單據,或手動傳票直接調整了此科目;請匯入期初餘額或補傳票"))
	}

	// 完整性
	n, err := q.InventoryBalanceMismatches(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	checks = append(checks, countCheck("stock", "庫存現有量 vs 流水帳合計", n, "現有量與流水帳不一致,請立即通知系統管理員"))
	n, err = q.LotBalanceMismatches(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	checks = append(checks, countCheck("lots", "批號現有量 vs 流水帳 / 料品現有量", n, "批號庫存與流水帳或料品現有量不一致,請立即通知系統管理員"))
	n, err = q.BinBalanceMismatches(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	checks = append(checks, countCheck("bins", "儲位現有量 vs 流水帳 / 料品現有量", n, "儲位庫存與流水帳或料品現有量不一致,請立即通知系統管理員"))
	n, err = q.LotBinBalanceMismatches(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	checks = append(checks, countCheck("lotbins", "批號 × 儲位庫存 vs 批號 / 儲位現有量", n, "批號與儲位的庫存對應不一致,請立即通知系統管理員"))
	n, err = q.UnbalancedVouchers(ctx, companyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	checks = append(checks, countCheck("vouchers", "已過帳傳票借貸平衡", n, "有傳票借貸不平衡,請立即通知系統管理員"))

	// 提醒
	monthStart := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	n, err = q.UncostedStockMonths(ctx, db.UncostedStockMonthsParams{CompanyID: companyID, MonthStart: monthStart})
	if err != nil {
		response.Error(c, err)
		return
	}
	pending := checkDTO{Key: "uncosted", Label: "已結束但尚未月結成本的月份", Status: statusOK}
	if n > 0 {
		pending.Status = statusWarn
		pending.Message = fmt.Sprintf("還有 %d 個月份有庫存異動、尚未月結成本,銷貨成本與存貨損益傳票尚未產生", n)
	}
	checks = append(checks, pending)

	ok := true
	for _, ch := range checks {
		if ch.Status == statusError {
			ok = false
		}
	}
	response.OK(c, gin.H{"checked_at": today, "ok": ok, "checks": checks})
}
