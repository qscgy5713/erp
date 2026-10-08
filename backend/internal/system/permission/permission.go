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
	ImportRun       = "system.import.run" // 資料匯入(主檔、期初餘額)
	CompanyRead     = "system.company.read"
	CompanyWrite    = "system.company.write" // 公司資料(統一編號、稅籍編號)
	ApprovalRead    = "system.approval.read"
	ApprovalWrite   = "system.approval.write" // 簽核規則(誰在多大金額要幾層核准)

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
	// 銷售
	SalesOrderRead    = "sales.order.read" // 報價單與訂單
	SalesOrderWrite   = "sales.order.write"
	SalesOrderApprove = "sales.order.approve"
	DeliveryRead      = "sales.delivery.read"  // 出貨單、銷貨退回單
	DeliveryWrite     = "sales.delivery.write" // 含登錄發票號碼
	DeliveryApprove   = "sales.delivery.approve"
	DeliveryPost      = "sales.delivery.post"
	// 月結成本與對帳
	CostRead  = "costing.read"  // 月結結果、對帳檢查
	CostClose = "costing.close" // 月結 / 取消月結
	// 會計
	AccountRead  = "gl.account.read" // 科目表、拋轉規則
	AccountWrite = "gl.account.write"
	VoucherRead  = "gl.voucher.read"
	VoucherWrite = "gl.voucher.write" // 建立/修改草稿、作廢草稿
	VoucherPost  = "gl.voucher.post"  // 傳票過帳、沖銷
	PeriodRead   = "gl.period.read"
	PeriodClose  = "gl.period.close" // 關帳 / 重開
	ReportRead   = "gl.report.read"  // 試算表、總分類帳、日記帳
	// 應收應付
	PayableRead    = "finance.payable.read"    // 應付帳款、付款對帳單、應付帳齡
	ReceivableRead = "finance.receivable.read" // 應收帳款、收款對帳單、應收帳齡
	// 收款單 / 付款單(沖帳)
	CollectionRead    = "finance.collection.read"
	CollectionWrite   = "finance.collection.write" // 建立/修改草稿、送審、作廢草稿
	CollectionApprove = "finance.collection.approve"
	CollectionPost    = "finance.collection.post" // 過帳(沖帳)、反過帳
	PaymentRead       = "finance.payment.read"
	PaymentWrite      = "finance.payment.write"
	PaymentApprove    = "finance.payment.approve"
	PaymentPost       = "finance.payment.post"
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
	{"系統管理", "公司資料", []Permission{{CompanyRead, "檢視"}, {CompanyWrite, "修改(統一編號、稅籍編號)"}}},
	{"系統管理", "簽核規則", []Permission{{ApprovalRead, "檢視"}, {ApprovalWrite, "新增/修改/刪除"}}},
	{"系統管理", "資料匯入", []Permission{{ImportRun, "匯入 Excel(主檔、期初餘額)"}}},
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
	{"銷售", "報價單與訂單", []Permission{
		{SalesOrderRead, "檢視"}, {SalesOrderWrite, "開單/送審"}, {SalesOrderApprove, "核准/退回/作廢/結案"},
	}},
	{"銷售", "出貨單與銷貨退回單", []Permission{
		{DeliveryRead, "檢視"}, {DeliveryWrite, "開單/送審/登錄發票"}, {DeliveryApprove, "核准/退回/作廢"}, {DeliveryPost, "過帳/反過帳"},
	}},
	{"庫存", "月結成本與對帳檢查", []Permission{{CostRead, "檢視"}, {CostClose, "月結/取消月結"}}},
	{"會計", "會計科目與拋轉規則", []Permission{{AccountRead, "檢視"}, {AccountWrite, "新增/修改"}}},
	{"會計", "傳票", []Permission{{VoucherRead, "檢視"}, {VoucherWrite, "開單"}, {VoucherPost, "過帳/沖銷"}}},
	{"會計", "會計期間", []Permission{{PeriodRead, "檢視"}, {PeriodClose, "關帳/重開"}}},
	{"會計", "會計報表", []Permission{{ReportRead, "試算表/總分類帳/日記帳"}}},
	{"應收應付", "應收帳款(含對帳單、帳齡)", []Permission{{ReceivableRead, "檢視"}}},
	{"應收應付", "應付帳款(含對帳單、帳齡)", []Permission{{PayableRead, "檢視"}}},
	{"應收應付", "收款單", []Permission{
		{CollectionRead, "檢視"}, {CollectionWrite, "開單/送審"}, {CollectionApprove, "核准/退回/作廢"}, {CollectionPost, "過帳(沖帳)/反過帳"},
	}},
	{"應收應付", "付款單", []Permission{
		{PaymentRead, "檢視"}, {PaymentWrite, "開單/送審"}, {PaymentApprove, "核准/退回/作廢"}, {PaymentPost, "過帳(沖帳)/反過帳"},
	}},
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
