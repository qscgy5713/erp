package production

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"erp/internal/db"
	"erp/internal/platform/database"
	"erp/internal/platform/httpx"
	"erp/internal/shared/apperr"
	"erp/internal/shared/money"
	"erp/internal/shared/page"
	"erp/internal/shared/response"
	"erp/internal/system/audit"
)

const maxBomLines = 100

var (
	errBomDup   = apperr.Conflict("PRD-001", "這個成品在這個生效日已有 BOM,請直接修改,或換一個生效日")
	errBomCycle = apperr.New(http.StatusUnprocessableEntity, "PRD-002", "BOM 不可循環:材料的 BOM 又用到這個成品")
	errBomInUse = apperr.Conflict("PRD-003", "已有工單使用這個成品,不可刪除 BOM;不再生產請改為停用")
)

// WouldCycle 把 parent 的材料改成 children 之後,BOM 關係(成品 → 材料)是否出現循環:
// 任何一個材料(含其下層材料)用到 parent 就是循環。edges 為目前全公司的關係(parent 自己的舊關係會被忽略)。
func WouldCycle(edges map[int64][]int64, parent int64, children []int64) bool {
	seen := map[int64]bool{}
	var reach func(id int64) bool
	reach = func(id int64) bool {
		if id == parent {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, c := range edges[id] {
			if reach(c) {
				return true
			}
		}
		return false
	}
	for _, c := range children {
		if reach(c) {
			return true
		}
	}
	return false
}

type bomLineDTO struct {
	LineNo   int32           `json:"line_no"`
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	Qty      decimal.Decimal `json:"qty"`
	ScrapPct decimal.Decimal `json:"scrap_pct"` // 損耗率(%)
	Note     string          `json:"note"`
}

type bomDTO struct {
	ID       int64           `json:"id"`
	ItemID   int64           `json:"item_id"`
	ItemCode string          `json:"item_code"`
	ItemName string          `json:"item_name"`
	UnitName string          `json:"unit_name"`
	YieldQty decimal.Decimal `json:"yield_qty"`
	// 生效日(YYYY-MM-DD):工單日期當天或之後適用;同一成品可有多份不同生效日的 BOM
	EffectiveFrom string       `json:"effective_from"`
	IsActive      bool         `json:"is_active"`
	Note          string       `json:"note"`
	Lines         []bomLineDTO `json:"lines"`
	Version       int32        `json:"version"`
}

func loadBom(c *gin.Context, q *db.Queries, companyID, id int64) (bomDTO, error) {
	ctx := c.Request.Context()
	b, err := q.GetBom(ctx, db.GetBomParams{ID: id, CompanyID: companyID})
	if database.IsNoRows(err) {
		return bomDTO{}, apperr.ErrNotFound
	}
	if err != nil {
		return bomDTO{}, err
	}
	rows, err := q.ListBomLines(ctx, id)
	if err != nil {
		return bomDTO{}, err
	}
	out := bomDTO{ID: b.ID, ItemID: b.ItemID, ItemCode: b.ItemCode, ItemName: b.ItemName, UnitName: b.UnitName,
		YieldQty: b.YieldQty, EffectiveFrom: b.EffectiveFrom.Format(time.DateOnly), IsActive: b.IsActive, Note: b.Note, Lines: make([]bomLineDTO, len(rows)), Version: b.Version}
	for i, l := range rows {
		out.Lines[i] = bomLineDTO{LineNo: l.LineNo, ItemID: l.ItemID, ItemCode: l.ItemCode, ItemName: l.ItemName, UnitName: l.UnitName, Qty: l.Qty, ScrapPct: l.ScrapPct, Note: l.Note}
	}
	return out, nil
}

type bomListDTO struct {
	ID            int64           `json:"id"`
	ItemID        int64           `json:"item_id"`
	ItemCode      string          `json:"item_code"`
	ItemName      string          `json:"item_name"`
	UnitName      string          `json:"unit_name"`
	YieldQty      decimal.Decimal `json:"yield_qty"`
	EffectiveFrom string          `json:"effective_from"`
	IsActive      bool            `json:"is_active"`
	LineCount     int64           `json:"line_count"`
}

func (m *Module) listBoms(c *gin.Context) {
	pg := page.Parse(c.Query("page"), c.Query("size"))
	keyword := httpx.QueryString(c, "keyword")
	companyID := actor(c).CompanyID
	ctx := c.Request.Context()
	rows, err := m.store.ListBoms(ctx, db.ListBomsParams{CompanyID: companyID, Keyword: keyword, Lim: pg.Limit(), Off: pg.Offset()})
	if err != nil {
		response.Error(c, err)
		return
	}
	total, err := m.store.CountBoms(ctx, db.CountBomsParams{CompanyID: companyID, Keyword: keyword})
	if err != nil {
		response.Error(c, err)
		return
	}
	out := make([]bomListDTO, len(rows))
	for i, r := range rows {
		out[i] = bomListDTO{ID: r.ID, ItemID: r.ItemID, ItemCode: r.ItemCode, ItemName: r.ItemName, UnitName: r.UnitName,
			YieldQty: r.YieldQty, EffectiveFrom: r.EffectiveFrom.Format(time.DateOnly), IsActive: r.IsActive, LineCount: r.LineCount}
	}
	response.List(c, out, pg.Meta(total))
}

func (m *Module) getBom(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	dto, err := loadBom(c, m.store.Queries, actor(c).CompanyID, id)
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, dto)
}

type bomLineInput struct {
	ItemID int64           `json:"item_id" binding:"required"`
	Qty    decimal.Decimal `json:"qty"`
	// 損耗率(%),0 ≤ x < 100,最多 2 位小數;空值視為 0
	ScrapPct decimal.Decimal `json:"scrap_pct"`
	Note     string          `json:"note" binding:"max=255"`
}

type bomInput struct {
	ItemID   int64           `json:"item_id" binding:"required"`
	YieldQty decimal.Decimal `json:"yield_qty"`
	// 生效日(YYYY-MM-DD),空白表示一直有效(2000-01-01)
	EffectiveFrom string         `json:"effective_from"`
	IsActive      bool           `json:"is_active"`
	Note          string         `json:"note" binding:"max=2000"`
	Lines         []bomLineInput `json:"lines" binding:"dive"`
	Version       int32          `json:"version"`
}

func qtyOK(q decimal.Decimal) bool {
	return q.IsPositive() && q.Exponent() >= -money.QuantityPlaces
}

// validateBom 成品與材料須為啟用的商品類料品;材料不可重複、不可是成品自己,也不可造成循環。
func validateBom(c *gin.Context, q *db.Queries, companyID, selfID int64, in *bomInput) error {
	ctx := c.Request.Context()
	if _, err := bomEffectiveDate(in.EffectiveFrom); err != nil {
		return fieldErr("effective_from", "生效日格式須為 YYYY-MM-DD")
	}
	if !qtyOK(in.YieldQty) {
		return fieldErr("yield_qty", "須大於 0,最多 4 位小數")
	}
	if len(in.Lines) == 0 || len(in.Lines) > maxBomLines {
		return fieldErr("lines", "請輸入材料明細(最多 100 筆)")
	}
	ids := []int64{in.ItemID}
	for _, l := range in.Lines {
		ids = append(ids, l.ItemID)
	}
	items, err := q.ListProductionItems(ctx, db.ListProductionItemsParams{CompanyID: companyID, Ids: ids})
	if err != nil {
		return err
	}
	byID := map[int64]db.ListProductionItemsRow{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if it, ok := byID[in.ItemID]; !ok || it.ItemType != "goods" || !it.IsActive {
		return fieldErr("item_id", "成品須為啟用的商品類料品")
	}
	fields := map[string]string{}
	seen := map[int64]bool{}
	children := make([]int64, 0, len(in.Lines))
	for i, l := range in.Lines {
		key := "lines." + itoa(i)
		it, ok := byID[l.ItemID]
		switch {
		case !ok || it.ItemType != "goods" || !it.IsActive:
			fields[key] = "材料須為啟用的商品類料品"
		case l.ItemID == in.ItemID:
			fields[key] = "材料不可是成品自己"
		case seen[l.ItemID]:
			fields[key] = it.Code + " 重複"
		case !qtyOK(l.Qty):
			fields[key] = "用量須大於 0,最多 4 位小數"
		case l.ScrapPct.IsNegative() || l.ScrapPct.GreaterThanOrEqual(decimal.NewFromInt(100)) || l.ScrapPct.Exponent() < -2:
			fields[key] = "損耗率須介於 0 到 100(不含 100),最多 2 位小數"
		}
		seen[l.ItemID] = true
		children = append(children, l.ItemID)
	}
	if len(fields) > 0 {
		return apperr.Validation(fields)
	}
	edges, err := q.AllBomEdges(ctx, companyID)
	if err != nil {
		return err
	}
	graph := map[int64][]int64{}
	for _, e := range edges {
		if e.BomID != selfID { // 修改中的這份 BOM 原本的關係會被新的取代;同一成品其他版本的關係保留
			graph[e.ParentID] = append(graph[e.ParentID], e.ChildID)
		}
	}
	if WouldCycle(graph, in.ItemID, children) {
		return errBomCycle
	}
	return nil
}

func saveBomLines(c *gin.Context, q *db.Queries, bomID int64, lines []bomLineInput) error {
	ctx := c.Request.Context()
	if err := q.DeleteBomLines(ctx, bomID); err != nil {
		return err
	}
	for i, l := range lines {
		if err := q.AddBomLine(ctx, db.AddBomLineParams{BomID: bomID, LineNo: int32(i + 1), ItemID: l.ItemID, Qty: l.Qty, ScrapPct: l.ScrapPct, Note: l.Note}); err != nil {
			return err
		}
	}
	return nil
}

func (m *Module) createBom(c *gin.Context) {
	var in bomInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out bomDTO
	err := m.store.InTx(ctx, func(q *db.Queries) error {
		if err := validateBom(c, q, a.CompanyID, 0, &in); err != nil {
			return err
		}
		eff, _ := bomEffectiveDate(in.EffectiveFrom)
		b, err := q.CreateBom(ctx, db.CreateBomParams{CompanyID: a.CompanyID, ItemID: in.ItemID, YieldQty: in.YieldQty, EffectiveFrom: eff,
			IsActive: in.IsActive, Note: in.Note, ActorID: &a.UserID})
		if database.IsUniqueViolation(err, "boms_company_item_effective_key") {
			return errBomDup
		}
		if err != nil {
			return err
		}
		if err := saveBomLines(c, q, b.ID, in.Lines); err != nil {
			return err
		}
		if out, err = loadBom(c, q, a.CompanyID, b.ID); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Create, EntityType: "bom", EntityID: &b.ID,
			Summary: "新增 BOM " + out.ItemCode + " " + out.ItemName, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.Created(c, out)
}

func (m *Module) updateBom(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	var in bomInput
	if err := httpx.BindJSON(c, &in); err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	var out bomDTO
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := loadBom(c, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		if in.ItemID != before.ItemID {
			return fieldErr("item_id", "BOM 的成品不能更換,請新增另一份 BOM")
		}
		if err := validateBom(c, q, a.CompanyID, id, &in); err != nil {
			return err
		}
		eff, _ := bomEffectiveDate(in.EffectiveFrom)
		if _, err := q.UpdateBom(ctx, db.UpdateBomParams{ID: id, CompanyID: a.CompanyID, YieldQty: in.YieldQty, EffectiveFrom: eff,
			IsActive: in.IsActive, Note: in.Note, ActorID: &a.UserID, Version: in.Version}); database.IsNoRows(err) {
			return apperr.ErrVersionConflict
		} else if err != nil {
			return err
		}
		if err := saveBomLines(c, q, id, in.Lines); err != nil {
			return err
		}
		if out, err = loadBom(c, q, a.CompanyID, id); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Update, EntityType: "bom", EntityID: &id,
			Summary: "修改 BOM " + out.ItemCode + " " + out.ItemName, Before: before, After: out})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.OK(c, out)
}

func (m *Module) deleteBom(c *gin.Context) {
	id, err := httpx.ParamID(c, "id")
	if err != nil {
		response.Error(c, err)
		return
	}
	a := actor(c)
	ctx := c.Request.Context()
	err = m.store.InTx(ctx, func(q *db.Queries) error {
		before, err := loadBom(c, q, a.CompanyID, id)
		if err != nil {
			return err
		}
		if used, err := q.ItemHasWorkOrders(ctx, db.ItemHasWorkOrdersParams{CompanyID: a.CompanyID, ItemID: before.ItemID}); err != nil {
			return err
		} else if used {
			return errBomInUse
		}
		if _, err := q.DeleteBom(ctx, db.DeleteBomParams{ID: id, CompanyID: a.CompanyID}); err != nil {
			return err
		}
		return audit.Record(ctx, q, audit.Entry{Action: audit.Delete, EntityType: "bom", EntityID: &id,
			Summary: "刪除 BOM " + before.ItemCode + " " + before.ItemName, Before: before})
	})
	if err != nil {
		response.Error(c, err)
		return
	}
	response.NoContent(c)
}

func itoa(i int) string { return strconv.Itoa(i) }

// bomEffectiveDate 解析生效日;空白為 2000-01-01(一直有效)。
func bomEffectiveDate(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), nil
	}
	return time.Parse(time.DateOnly, strings.TrimSpace(s))
}
