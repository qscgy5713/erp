// Package docno 依單號規則產生單號,例如 PO202610070001。
// Next 必須在建立單據的同一個交易內呼叫:計數列會鎖到交易結束,
// 交易回滾時號碼也一併回滾,不會跳號或重號。
package docno

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
)

var DateFormats = map[string]string{
	"YYYYMMDD": "20060102",
	"YYYYMM":   "200601",
	"YYYY":     "2006",
	"NONE":     "",
}

var ErrRuleNotFound = apperr.New(http.StatusUnprocessableEntity, "DOCNO-001", "找不到此單據類型的單號規則")

// PeriodKey 回傳流水號重置的期間代碼;NONE 不重置,回傳空字串。
func PeriodKey(dateFormat string, docDate time.Time) (string, error) {
	layout, ok := DateFormats[dateFormat]
	if !ok {
		return "", fmt.Errorf("docno: 未知的日期格式 %q", dateFormat)
	}
	if layout == "" {
		return "", nil
	}
	return docDate.Format(layout), nil
}

// Format 組出單號;流水號超過長度時照實輸出(不截斷),避免重號。
func Format(prefix, periodKey string, seqLength int, seq int64) string {
	return fmt.Sprintf("%s%s%0*d", prefix, periodKey, seqLength, seq)
}

// Preview 回傳規則套用在指定日期的第一號範例。
func Preview(r db.DocNumberRule, docDate time.Time) string {
	key, err := PeriodKey(r.DateFormat, docDate)
	if err != nil {
		return ""
	}
	return Format(r.Prefix, key, int(r.SeqLength), 1)
}

// Next 取得下一個單號。docDate 為單據日期(只取年月日)。
func Next(ctx context.Context, q *db.Queries, companyID int64, docType string, docDate time.Time) (string, error) {
	rule, err := q.GetDocNumberRule(ctx, db.GetDocNumberRuleParams{CompanyID: companyID, DocType: docType})
	if database.IsNoRows(err) {
		return "", ErrRuleNotFound.WithDetails(map[string]string{"doc_type": docType})
	}
	if err != nil {
		return "", fmt.Errorf("docno: 讀取規則: %w", err)
	}
	key, err := PeriodKey(rule.DateFormat, docDate)
	if err != nil {
		return "", err
	}
	seq, err := q.NextDocNumber(ctx, db.NextDocNumberParams{
		CompanyID: companyID, DocType: docType, PeriodKey: key,
	})
	if err != nil {
		return "", fmt.Errorf("docno: 取號: %w", err)
	}
	return Format(strings.ToUpper(rule.Prefix), key, int(rule.SeqLength), seq), nil
}
