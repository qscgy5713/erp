// Package imports 為 Excel 資料匯入(D54):主檔(料品、客戶、供應商)與期初資料
// (庫存、應收、應付、科目餘額)。每次匯入先「預檢」(dry run)列出所有錯誤,通過後才寫入;
// 寫入為全有或全無的單一交易。期初庫存 / 應收 / 應付 / 科目餘額可整批撤銷。
package imports

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/xuri/excelize/v2"

	"erp/internal/auth"
	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/shared/apperr"
	"erp/internal/shared/authctx"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
	"erp/internal/system/permission"
)

const (
	maxFileBytes = 5 << 20
	maxRows      = 5000
	maxErrors    = 200
	dataSheet    = "匯入資料"
)

type Module struct {
	store *database.Store
}

func New(store *database.Store) *Module { return &Module{store: store} }

// Register 掛上 /imports 路由;r 須已套用 auth.Authenticate。
func (m *Module) Register(r *gin.RouterGroup) {
	g := r.Group("/imports", auth.Require(permission.ImportRun))
	g.GET("/types", m.listTypes)
	g.GET("/batches", m.listBatches)
	g.GET("/types/:type/template", m.template)
	g.POST("/types/:type", m.upload)
	g.POST("/batches/:id/undo", m.undo)
}

func actor(c *gin.Context) *authctx.Actor { return authctx.ActorFrom(c.Request.Context()) }

var errRollback = errors.New("rollback")

// ---- 匯入類型定義 ----

type column struct {
	name     string
	required bool
	note     string
}

type importer struct {
	key, label, desc string
	columns          []column
	sample           [][]string
	needsDate        bool // 期初類:須指定期初日期(date 參數)
	undoable         bool
	exec             func(r *run) error
	undo             func(ctx context.Context, q *db.Queries, a *authctx.Actor, b db.ImportBatch) error
}

func (im *importer) headers() []string {
	h := make([]string, len(im.columns))
	for i, c := range im.columns {
		h[i] = c.name
		if c.required {
			h[i] += "*"
		}
	}
	return h
}

var importers = []*importer{
	itemsImporter, customersImporter, suppliersImporter,
	openingStockImporter, openingARImporter, openingAPImporter, openingBalanceImporter,
}

func find(key string) *importer {
	for _, im := range importers {
		if im.key == key {
			return im
		}
	}
	return nil
}

// ---- 執行期上下文 ----

type sheetRow struct {
	n     int // Excel 列號
	cells []string
}

func (r sheetRow) get(i int) string {
	if i < 0 || i >= len(r.cells) {
		return ""
	}
	return strings.TrimSpace(r.cells[i])
}

type RowError struct {
	Row     int    `json:"row"`
	Column  string `json:"column"`
	Message string `json:"message"`
}

type run struct {
	ctx     context.Context
	q       *db.Queries
	a       *authctx.Actor
	im      *importer
	rows    []sheetRow
	date    time.Time
	batch   db.ImportBatch
	errs    []RowError
	summary string
}

// fail 記錄一個錯誤;col 為欄位名稱(不含星號),空字串表示整列。
func (r *run) fail(row int, col, format string, args ...any) {
	r.errs = append(r.errs, RowError{Row: row, Column: col, Message: fmt.Sprintf(format, args...)})
}

func parseDecimal(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(strings.NewReplacer(",", "", " ", "").Replace(s))
}

// parseDate 接受 2026-09-30、2026/9/30、20260930 與 Excel 日期序號。
func parseDate(s string) (time.Time, error) {
	for _, layout := range []string{"2006-01-02", "2006/1/2", "2006/01/02", "20060102"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 20000 && f < 80000 {
		if t, err := excelize.ExcelDateToTime(f, false); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("日期格式不正確")
}

// ---- 讀取 Excel ----

func readSheet(data []byte, im *importer) ([]sheetRow, *apperr.Error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, apperr.New(http.StatusUnprocessableEntity, "IMP-001", "無法讀取檔案,請上傳 .xlsx 格式的 Excel 檔")
	}
	defer func() { _ = f.Close() }()
	sheet := f.GetSheetName(0)
	for _, n := range f.GetSheetList() {
		if n == dataSheet {
			sheet = n
		}
	}
	all, err := f.GetRows(sheet)
	if err != nil || len(all) == 0 {
		return nil, apperr.New(http.StatusUnprocessableEntity, "IMP-002", "檔案沒有資料,請使用系統提供的範本")
	}
	want := im.headers()
	for i, w := range want {
		got := ""
		if i < len(all[0]) {
			got = strings.TrimSpace(all[0][i])
		}
		if got != w {
			return nil, apperr.New(http.StatusUnprocessableEntity, "IMP-003",
				fmt.Sprintf("第 1 列第 %d 欄標題應為「%s」,目前是「%s」;請使用系統提供的範本,不要修改標題", i+1, w, got))
		}
	}
	var rows []sheetRow
	for i, cells := range all[1:] {
		empty := true
		for _, c := range cells {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if !empty {
			rows = append(rows, sheetRow{n: i + 2, cells: cells})
		}
	}
	if len(rows) == 0 {
		return nil, apperr.New(http.StatusUnprocessableEntity, "IMP-002", "檔案沒有資料列")
	}
	if len(rows) > maxRows {
		return nil, apperr.New(http.StatusUnprocessableEntity, "IMP-004", fmt.Sprintf("一次最多匯入 %d 列,請分批", maxRows))
	}
	return rows, nil
}

// ---- 端點 ----

type typeDTO struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Desc      string   `json:"desc"`
	Columns   []string `json:"columns"`
	NeedsDate bool     `json:"needs_date"`
	Undoable  bool     `json:"undoable"`
}

func (m *Module) listTypes(c *gin.Context) {
	out := make([]typeDTO, len(importers))
	for i, im := range importers {
		out[i] = typeDTO{Key: im.key, Label: im.label, Desc: im.desc, Columns: im.headers(), NeedsDate: im.needsDate, Undoable: im.undoable}
	}
	response.OK(c, out)
}

type batchDTO struct {
	ID            int64      `json:"id"`
	ImportType    string     `json:"import_type"`
	TypeLabel     string     `json:"type_label"`
	Filename      string     `json:"filename"`
	RowCount      int32      `json:"row_count"`
	Summary       string     `json:"summary"`
	CreatedByName *string    `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
	Undoable      bool       `json:"undoable"`
	UndoneAt      *time.Time `json:"undone_at"`
}

func (m *Module) listBatches(c *gin.Context) {
	rows, err := m.store.ListImportBatches(c.Request.Context(), actor(c).CompanyID)
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]batchDTO, len(rows))
	for i, r := range rows {
		d := batchDTO{ID: r.ID, ImportType: r.ImportType, Filename: r.Filename, RowCount: r.RowCount, Summary: r.Summary,
			CreatedByName: r.CreatedByName, CreatedAt: r.CreatedAt, UndoneAt: r.UndoneAt}
		if im := find(r.ImportType); im != nil {
			d.TypeLabel, d.Undoable = im.label, im.undoable && r.UndoneAt == nil
		}
		out[i] = d
	}
	response.OK(c, out)
}

// template GET /imports/types/:type/template 下載範本:「匯入資料」只有標題列,「說明」列出各欄規則,「範例」供參考。
func (m *Module) template(c *gin.Context) {
	im := find(c.Param("type"))
	if im == nil {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_ = f.SetSheetName("Sheet1", dataSheet)
	head, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true}, Fill: excelize.Fill{Type: "pattern", Color: []string{"#FFF2CC"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center"},
	})
	text, _ := f.NewStyle(&excelize.Style{NumFmt: 49}) // 文字格式,避免代號被轉成數字、日期被轉成序號
	for i, h := range im.headers() {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(dataSheet, cell, h)
		_ = f.SetColWidth(dataSheet, string(rune('A'+i)), string(rune('A'+i)), float64(max(14, len([]rune(h))*2+2)))
	}
	endCol, _ := excelize.CoordinatesToCellName(len(im.columns), 1)
	_ = f.SetCellStyle(dataSheet, "A1", endCol, head)
	lastCol, _ := excelize.ColumnNumberToName(len(im.columns))
	_ = f.SetColStyle(dataSheet, "A:"+lastCol, text)
	_ = f.SetCellStyle(dataSheet, "A1", endCol, head)
	_ = f.SetPanes(dataSheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"})

	_, _ = f.NewSheet("說明")
	_ = f.SetCellValue("說明", "A1", "欄位")
	_ = f.SetCellValue("說明", "B1", "必填")
	_ = f.SetCellValue("說明", "C1", "說明")
	for i, col := range im.columns {
		row := i + 2
		_ = f.SetCellValue("說明", fmt.Sprintf("A%d", row), col.name)
		req := ""
		if col.required {
			req = "是"
		}
		_ = f.SetCellValue("說明", fmt.Sprintf("B%d", row), req)
		_ = f.SetCellValue("說明", fmt.Sprintf("C%d", row), col.note)
	}
	_ = f.SetColWidth("說明", "A", "A", 18)
	_ = f.SetColWidth("說明", "C", "C", 90)
	_ = f.SetCellValue("說明", fmt.Sprintf("A%d", len(im.columns)+3), "用法")
	_ = f.SetCellValue("說明", fmt.Sprintf("C%d", len(im.columns)+3), im.desc+" 標題列不可修改;欄位標題的 * 表示必填;空白列會被略過;一次最多 5000 列。")

	_, _ = f.NewSheet("範例")
	for i, h := range im.headers() {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue("範例", cell, h)
	}
	for r, row := range im.sample {
		for i, v := range row {
			cell, _ := excelize.CoordinatesToCellName(i+1, r+2)
			_ = f.SetCellValue("範例", cell, v)
		}
	}
	f.SetActiveSheet(0)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		response.Error(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+urlEscape(im.label+"範本.xlsx"))
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func urlEscape(s string) string {
	var b strings.Builder
	for _, by := range []byte(s) {
		if (by >= 'a' && by <= 'z') || (by >= 'A' && by <= 'Z') || (by >= '0' && by <= '9') || by == '.' || by == '-' || by == '_' {
			b.WriteByte(by)
		} else {
			fmt.Fprintf(&b, "%%%02X", by)
		}
	}
	return b.String()
}

type resultDTO struct {
	Type       string     `json:"type"`
	DryRun     bool       `json:"dry_run"`
	OK         bool       `json:"ok"`
	RowCount   int        `json:"row_count"`
	Summary    string     `json:"summary"`
	ErrorCount int        `json:"error_count"`
	Errors     []RowError `json:"errors"`
	BatchID    *int64     `json:"batch_id"`
}

// upload POST /imports/types/:type?dry_run=true&date=YYYY-MM-DD (multipart file)
// dry_run=true 只檢查不寫入;否則檢查全部通過才寫入,任何錯誤整批不寫入(回 200 並列出錯誤)。
func (m *Module) upload(c *gin.Context) {
	im := find(c.Param("type"))
	if im == nil {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxFileBytes+1<<20)
	fh, err := c.FormFile("file")
	if err != nil {
		response.Error(c, apperr.New(http.StatusUnprocessableEntity, "IMP-001", "請選擇要上傳的 Excel 檔(.xlsx,5MB 以內)"))
		return
	}
	if fh.Size > maxFileBytes {
		response.Error(c, apperr.New(http.StatusRequestEntityTooLarge, "IMP-005", "檔案超過 5MB"))
		return
	}
	file, err := fh.Open()
	if err != nil {
		response.Error(c, err)
		return
	}
	defer func() { _ = file.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(file); err != nil {
		response.Error(c, err)
		return
	}
	rows, perr := readSheet(buf.Bytes(), im)
	if perr != nil {
		response.Error(c, perr)
		return
	}
	var date time.Time
	if im.needsDate {
		if date, err = time.Parse(time.DateOnly, c.Query("date")); err != nil {
			response.Error(c, apperr.Validation(map[string]string{"date": "請指定期初日期(YYYY-MM-DD),通常是上線前一天或上期期末"}))
			return
		}
	}
	dry := c.Query("dry_run") == "true"
	ctx := c.Request.Context()
	a := actor(c)
	res := resultDTO{Type: im.key, DryRun: dry, RowCount: len(rows), Errors: []RowError{}}

	err = m.store.InTx(ctx, func(q *db.Queries) error {
		r := &run{ctx: ctx, q: q, a: a, im: im, rows: rows, date: date}
		if im.needsDate {
			// 期初類先建批次紀錄取得 id(預檢時隨交易一起回滾)
			b, err := q.CreateImportBatch(ctx, db.CreateImportBatchParams{
				CompanyID: a.CompanyID, ImportType: im.key, Filename: fh.Filename, RowCount: int32(len(rows)), CreatedBy: &a.UserID,
			})
			if err != nil {
				return err
			}
			r.batch = b
		}
		if err := im.exec(r); err != nil {
			return err
		}
		res.ErrorCount = len(r.errs)
		res.Errors = r.errs[:min(len(r.errs), maxErrors)]
		res.Summary = r.summary
		if len(r.errs) > 0 || dry {
			return errRollback
		}
		if !im.needsDate {
			b, err := q.CreateImportBatch(ctx, db.CreateImportBatchParams{
				CompanyID: a.CompanyID, ImportType: im.key, Filename: fh.Filename, RowCount: int32(len(rows)),
				Summary: r.summary, CreatedBy: &a.UserID,
			})
			if err != nil {
				return err
			}
			r.batch = b
		} else if err := q.SetImportBatchSummary(ctx, db.SetImportBatchSummaryParams{ID: r.batch.ID, Summary: r.summary}); err != nil {
			return err
		}
		res.BatchID = &r.batch.ID
		return audit.Record(ctx, q, audit.Entry{
			Action: "import", EntityType: "import_batch", EntityID: &r.batch.ID,
			Summary: fmt.Sprintf("匯入%s:%s(%d 列)", im.label, r.summary, len(rows)),
		})
	})
	if err != nil && !errors.Is(err, errRollback) {
		response.Error(c, err)
		return
	}
	res.OK = res.ErrorCount == 0
	response.OK(c, res)
}

// undo POST /imports/batches/:id/undo 撤銷整批期初資料(庫存、應收、應付、科目餘額);主檔請直接在畫面停用或修改。
func (m *Module) undo(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, apperr.ErrNotFound)
		return
	}
	ctx := c.Request.Context()
	a := actor(c)
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		b, err := q.GetImportBatch(ctx, db.GetImportBatchParams{ID: id, CompanyID: a.CompanyID})
		if database.IsNoRows(err) {
			return apperr.ErrNotFound
		}
		if err != nil {
			return err
		}
		im := find(b.ImportType)
		switch {
		case im == nil || !im.undoable:
			return apperr.Conflict("IMP-006", "此類型的匯入不支援撤銷;主檔請直接在畫面修改或停用")
		case b.UndoneAt != nil:
			return apperr.Conflict("IMP-007", "此批次已撤銷")
		}
		if err := im.undo(ctx, q, a, b); err != nil {
			return err
		}
		if err := q.MarkImportBatchUndone(ctx, db.MarkImportBatchUndoneParams{ID: id, UndoneBy: &a.UserID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{
			Action: "undo", EntityType: "import_batch", EntityID: &id, Summary: fmt.Sprintf("撤銷匯入%s(批次 %d)", im.label, id),
		})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, gin.H{"id": id, "undone": true})
}
