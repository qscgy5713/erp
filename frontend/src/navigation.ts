// 側邊選單。perm 為可見所需權限(任一即可);disabled 為尚未實作的模組
export interface NavItem {
  title: string
  path?: string
  perm?: string[]
  disabled?: boolean
  children?: NavItem[]
}

export const INVENTORY_ANY = [
  'inventory.stock.read',
  'inventory.stock.write',
  'inventory.stock.approve',
  'inventory.stock.post',
]
export const ORDER_ANY = ['purchase.order.read', 'purchase.order.write', 'purchase.order.approve']
export const COLLECTION_ANY = [
  'finance.collection.read',
  'finance.collection.write',
  'finance.collection.approve',
  'finance.collection.post',
]
export const PAYMENT_ANY = [
  'finance.payment.read',
  'finance.payment.write',
  'finance.payment.approve',
  'finance.payment.post',
]
export const VOUCHER_ANY = ['gl.voucher.read', 'gl.voucher.write', 'gl.voucher.post']
export const SALES_ORDER_ANY = ['sales.order.read', 'sales.order.write', 'sales.order.approve']
export const DELIVERY_ANY = [
  'sales.delivery.read',
  'sales.delivery.write',
  'sales.delivery.approve',
  'sales.delivery.post',
]
export const RECEIPT_ANY = [
  'purchase.receipt.read',
  'purchase.receipt.write',
  'purchase.receipt.approve',
  'purchase.receipt.post',
]

export const navigation: NavItem[] = [
  { title: '首頁', path: '/' },
  {
    title: '基本資料',
    children: [
      { title: '料品', path: '/masterdata/items', perm: ['masterdata.item.read'] },
      { title: '料品分類', path: '/masterdata/item-categories', perm: ['masterdata.item.read'] },
      { title: '單位', path: '/masterdata/units', perm: ['masterdata.item.read'] },
      { title: '倉庫', path: '/masterdata/warehouses', perm: ['masterdata.warehouse.read'] },
      { title: '客戶', path: '/masterdata/customers', perm: ['masterdata.customer.read'] },
      { title: '供應商', path: '/masterdata/suppliers', perm: ['masterdata.supplier.read'] },
      {
        title: '財務設定',
        path: '/masterdata/finance',
        perm: ['masterdata.finance.read', 'masterdata.finance.write'],
      },
    ],
  },
  {
    title: '採購',
    children: [
      { title: '採購單', path: '/purchase/orders', perm: ORDER_ANY },
      { title: '進貨 / 退出', path: '/purchase/receipts', perm: RECEIPT_ANY },
      {
        title: '未交貨清單',
        path: '/purchase/outstanding',
        perm: [...ORDER_ANY, 'purchase.receipt.write'],
      },
    ],
  },
  {
    title: '銷售',
    children: [
      { title: '報價 / 訂單', path: '/sales/orders', perm: SALES_ORDER_ANY },
      { title: '出貨 / 退回', path: '/sales/deliveries', perm: DELIVERY_ANY },
      {
        title: '未出貨清單',
        path: '/sales/unshipped',
        perm: [...SALES_ORDER_ANY, 'sales.delivery.write'],
      },
    ],
  },
  {
    title: '庫存',
    children: [
      { title: '現有量', path: '/inventory/balances', perm: INVENTORY_ANY },
      { title: '收發存', path: '/inventory/movement-summary', perm: INVENTORY_ANY },
      { title: '庫存單據', path: '/inventory/documents', perm: INVENTORY_ANY },
    ],
  },
  {
    title: '應收應付',
    children: [
      { title: '應收帳款', path: '/finance/receivables', perm: ['finance.receivable.read'] },
      { title: '收款單', path: '/finance/collections', perm: COLLECTION_ANY },
      {
        title: '應收對帳單',
        path: '/finance/statement/receivable',
        perm: ['finance.receivable.read'],
      },
      { title: '應收帳齡', path: '/finance/aging/receivable', perm: ['finance.receivable.read'] },
      { title: '應付帳款', path: '/finance/payables', perm: ['finance.payable.read'] },
      { title: '付款單', path: '/finance/payments', perm: PAYMENT_ANY },
      { title: '應付對帳單', path: '/finance/statement/payable', perm: ['finance.payable.read'] },
      { title: '應付帳齡', path: '/finance/aging/payable', perm: ['finance.payable.read'] },
    ],
  },
  {
    title: '會計',
    children: [
      { title: '傳票', path: '/gl/vouchers', perm: VOUCHER_ANY },
      { title: '會計報表', path: '/gl/reports', perm: ['gl.report.read'] },
      { title: '會計期間', path: '/gl/periods', perm: ['gl.period.read', 'gl.period.close'] },
      { title: '會計科目', path: '/gl/accounts', perm: ['gl.account.read', 'gl.account.write'] },
      { title: '拋轉規則', path: '/gl/mappings', perm: ['gl.account.read', 'gl.account.write'] },
    ],
  },
  {
    title: '系統管理',
    children: [
      { title: '部門', path: '/system/departments', perm: ['system.department.read'] },
      { title: '使用者', path: '/system/users', perm: ['system.user.read'] },
      { title: '角色權限', path: '/system/roles', perm: ['system.role.read'] },
      {
        title: '單號規則',
        path: '/system/doc-number-rules',
        perm: ['system.docno.read', 'system.docno.write'],
      },
      { title: '稽核日誌', path: '/system/audit-logs', perm: ['system.audit.read'] },
    ],
  },
]
