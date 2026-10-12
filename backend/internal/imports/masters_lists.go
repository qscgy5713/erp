package imports

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"erp/internal/db"
)

// yesNo 解析「是 / 否」欄位;空白為預設值。
func yesNo(r *run, row sheetRow, i int, col string, def bool) (bool, bool) {
	switch row.get(i) {
	case "":
		return def, true
	case "是":
		return true, true
	case "否":
		return false, true
	}
	r.fail(row.n, col, "須為「是」或「否」")
	return def, false
}

// codeAndName 檢查代號(必填、長度、不與既有或檔內重複)與名稱。
func codeAndName(r *run, row sheetRow, label string, codeMax, nameMax int, taken map[string]bool) bool {
	code, name := row.get(0), row.get(1)
	ok := true
	switch {
	case code == "":
		r.fail(row.n, label+"代號", "必填")
		ok = false
	case utf8.RuneCountInString(code) > codeMax:
		r.fail(row.n, label+"代號", "最多 %d 字", codeMax)
		ok = false
	case taken[strings.ToUpper(code)]:
		r.fail(row.n, label+"代號", "「%s」已存在或在檔內重複", code)
		ok = false
	}
	if name == "" {
		r.fail(row.n, "名稱", "必填")
		ok = false
	} else {
		ok = checkLen(r, row, "名稱", name, nameMax) && ok
	}
	return ok
}

var warehousesImporter = &importer{
	key: "warehouses", label: "倉庫", desc: "匯入倉庫主檔(新增用,已存在的代號視為錯誤)。期初庫存要先有倉庫,可先匯入倉庫再匯入料品與期初。",
	columns: []column{
		{"倉庫代號", true, "最多 20 字,不可重複"},
		{"名稱", true, "最多 100 字"},
		{"地址", false, "最多 255 字"},
		{"允許負庫存", false, "是 / 否;空白為否"},
		{"使用儲位", false, "是 / 否;空白為否。啟用後該倉的異動都要指定儲位"},
	},
	sample: [][]string{{"WH-A", "主倉", "台北市中山區 1 號", "否", "否"}, {"WH-B", "成品倉", "", "否", "是"}},
	exec: func(r *run) error {
		existing, err := r.q.ListWarehouses(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		codes := map[string]bool{}
		for _, w := range existing {
			codes[strings.ToUpper(w.Code)] = true
		}
		var todo []db.CreateWarehouseParams
		for _, row := range r.rows {
			ok := codeAndName(r, row, "倉庫", 20, 100, codes)
			ok = checkLen(r, row, "地址", row.get(2), 255) && ok
			neg, c1 := yesNo(r, row, 3, "允許負庫存", false)
			bins, c2 := yesNo(r, row, 4, "使用儲位", false)
			if !ok || !c1 || !c2 {
				continue
			}
			codes[strings.ToUpper(row.get(0))] = true
			todo = append(todo, db.CreateWarehouseParams{
				CompanyID: r.a.CompanyID, Code: row.get(0), Name: row.get(1), Address: row.get(2),
				AllowNegative: neg, UseBins: bins, CreatedBy: &r.a.UserID,
			})
		}
		if len(r.errs) > 0 {
			return nil
		}
		for _, p := range todo {
			if _, err := r.q.CreateWarehouse(r.ctx, p); err != nil {
				return err
			}
		}
		r.summary = "新增 " + itoa(len(todo)) + " 座倉庫"
		return nil
	},
}

var categoriesImporter = &importer{
	key: "categories", label: "料品分類", desc: "匯入料品分類(新增用,已存在的代號視為錯誤)。上層分類須已存在,或在檔內排在它前面。",
	columns: []column{
		{"分類代號", true, "最多 20 字,不可重複"},
		{"名稱", true, "最多 100 字"},
		{"上層分類代號", false, "已存在的分類,或本檔上方已出現的分類;空白為頂層"},
		{"排序", false, "整數,空白為 0"},
	},
	sample: [][]string{{"FOOD", "食品", "", "1"}, {"DAIRY", "乳製品", "FOOD", "1"}},
	exec: func(r *run) error {
		existing, err := r.q.ListItemCategories(r.ctx, r.a.CompanyID)
		if err != nil {
			return err
		}
		codes := map[string]bool{}
		ids := map[string]int64{} // 可作為上層的分類(含本檔剛建立的)
		for _, c := range existing {
			codes[strings.ToUpper(c.Code)] = true
			ids[strings.ToUpper(c.Code)] = c.ID
		}
		n := 0
		for _, row := range r.rows {
			ok := codeAndName(r, row, "分類", 20, 100, codes)
			var parent *int64
			if p := row.get(2); p != "" {
				id, found := ids[strings.ToUpper(p)]
				if !found {
					r.fail(row.n, "上層分類代號", "「%s」不存在(須已存在或排在本列之前)", p)
					ok = false
				} else {
					parent = &id
				}
			}
			sort := 0
			if v := row.get(3); v != "" {
				x, err := strconv.Atoi(v)
				if err != nil || x < 0 || x > 100000 {
					r.fail(row.n, "排序", "須為 0 ~ 100000 的整數")
					ok = false
				}
				sort = x
			}
			if !ok {
				// 失敗列的代號仍保留,避免後續列因找不到上層而重複報錯
				codes[strings.ToUpper(row.get(0))] = true
				continue
			}
			c, err := r.q.CreateItemCategory(r.ctx, db.CreateItemCategoryParams{
				CompanyID: r.a.CompanyID, ParentID: parent, Code: row.get(0), Name: row.get(1), SortOrder: int32(sort), CreatedBy: &r.a.UserID,
			})
			if err != nil {
				return err
			}
			codes[strings.ToUpper(c.Code)] = true
			ids[strings.ToUpper(c.Code)] = c.ID
			n++
		}
		r.summary = "新增 " + itoa(n) + " 個分類"
		return nil
	},
}
