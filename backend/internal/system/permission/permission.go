// Package permission 定義所有權限點。權限點只存在程式碼中,
// 角色在資料庫只存代碼;新增模組時在此註冊。
// 代碼格式:模組.資源.動作
package permission

const (
	DepartmentRead  = "system.department.read"
	DepartmentWrite = "system.department.write"
	UserRead        = "system.user.read"
	UserWrite       = "system.user.write"
	RoleRead        = "system.role.read"
	RoleWrite       = "system.role.write"
	AuditRead       = "system.audit.read"
	DocNumberRead   = "system.docno.read"
	DocNumberWrite  = "system.docno.write"

	// 基本資料
	ItemRead       = "masterdata.item.read" // 料品、分類、單位
	ItemWrite      = "masterdata.item.write"
	WarehouseRead  = "masterdata.warehouse.read"
	WarehouseWrite = "masterdata.warehouse.write"
	CustomerRead   = "masterdata.customer.read"
	CustomerWrite  = "masterdata.customer.write"
	CustomerCredit = "masterdata.customer.credit" // 設定信用額度(風險控管,與一般維護分開)
	SupplierRead   = "masterdata.supplier.read"
	SupplierWrite  = "masterdata.supplier.write"
	FinanceRead    = "masterdata.finance.read" // 幣別、匯率、稅別、付款條件
	FinanceWrite   = "masterdata.finance.write"

	// 庫存
	InventoryRead    = "inventory.stock.read"    // 現有量、收發存、庫存單據
	InventoryWrite   = "inventory.stock.write"   // 建立/修改草稿、送審、作廢草稿
	InventoryApprove = "inventory.stock.approve" // 核准、退回、取消核准、作廢
	InventoryPost    = "inventory.stock.post"    // 過帳、反過帳
	// 採購
	PurchaseOrderRead    = "purchase.order.read"
	PurchaseOrderWrite   = "purchase.order.write"   // 建立/修改草稿、送審、作廢草稿
	PurchaseOrderApprove = "purchase.order.approve" // 核准、退回、取消核准、作廢、結案、重開
	ReceiptRead          = "purchase.receipt.read"  // 進貨單、進貨退出單
	ReceiptWrite         = "purchase.receipt.write"
	ReceiptApprove       = "purchase.receipt.approve"
	ReceiptPost          = "purchase.receipt.post" // 過帳、反過帳(異動庫存與應付)
	// 應收應付
	PayableRead = "finance.payable.read"
)

type Permission struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Group struct {
	Module      string       `json:"module"`
	Resource    string       `json:"resource"`
	Permissions []Permission `json:"permissions"`
}

// Groups 依模組 → 資源分組,供前端顯示權限勾選樹。
var Groups = []Group{
	{"系統管理", "部門", []Permission{{DepartmentRead, "檢視"}, {DepartmentWrite, "新增/修改"}}},
	{"系統管理", "使用者", []Permission{{UserRead, "檢視"}, {UserWrite, "新增/修改/重設密碼"}}},
	{"系統管理", "角色權限", []Permission{{RoleRead, "檢視"}, {RoleWrite, "新增/修改/刪除"}}},
	{"系統管理", "稽核日誌", []Permission{{AuditRead, "檢視"}}},
	{"系統管理", "單號規則", []Permission{{DocNumberRead, "檢視"}, {DocNumberWrite, "修改"}}},
	{"基本資料", "料品(含分類、單位)", []Permission{{ItemRead, "檢視"}, {ItemWrite, "新增/修改"}}},
	{"基本資料", "倉庫", []Permission{{WarehouseRead, "檢視"}, {WarehouseWrite, "新增/修改"}}},
	{"基本資料", "客戶", []Permission{{CustomerRead, "檢視"}, {CustomerWrite, "新增/修改"}, {CustomerCredit, "設定信用額度"}}},
	{"基本資料", "供應商", []Permission{{SupplierRead, "檢視"}, {SupplierWrite, "新增/修改"}}},
	{"基本資料", "財務設定(幣別、匯率、稅別、付款條件)", []Permission{{FinanceRead, "檢視"}, {FinanceWrite, "新增/修改"}}},
	{"庫存", "庫存單據與報表", []Permission{
		{InventoryRead, "檢視"}, {InventoryWrite, "開單/送審"}, {InventoryApprove, "核准/退回/作廢"}, {InventoryPost, "過帳/反過帳"},
	}},
	{"採購", "採購單", []Permission{
		{PurchaseOrderRead, "檢視"}, {PurchaseOrderWrite, "開單/送審"}, {PurchaseOrderApprove, "核准/退回/作廢/結案"},
	}},
	{"採購", "進貨單與進貨退出單", []Permission{
		{ReceiptRead, "檢視"}, {ReceiptWrite, "開單/送審"}, {ReceiptApprove, "核准/退回/作廢"}, {ReceiptPost, "過帳/反過帳"},
	}},
	{"應收應付", "應付帳款", []Permission{{PayableRead, "檢視"}}},
}

var known = func() map[string]struct{} {
	m := map[string]struct{}{}
	for _, g := range Groups {
		for _, p := range g.Permissions {
			m[p.Code] = struct{}{}
		}
	}
	return m
}()

// Known 是否為已註冊的權限點。
func Known(code string) bool {
	_, ok := known[code]
	return ok
}
