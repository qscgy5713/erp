// 側邊選單。perm 為可見所需權限(任一即可);disabled 為尚未實作的模組
export interface NavItem {
  title: string
  path?: string
  perm?: string[]
  disabled?: boolean
  children?: NavItem[]
}

const INVENTORY_ANY = [
  'inventory.stock.read',
  'inventory.stock.write',
  'inventory.stock.approve',
  'inventory.stock.post',
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
  { title: '採購', path: '/purchase', disabled: true },
  { title: '銷售', path: '/sales', disabled: true },
  {
    title: '庫存',
    children: [
      { title: '現有量', path: '/inventory/balances', perm: INVENTORY_ANY },
      { title: '收發存', path: '/inventory/movement-summary', perm: INVENTORY_ANY },
      { title: '庫存單據', path: '/inventory/documents', perm: INVENTORY_ANY },
    ],
  },
  { title: '應收應付', path: '/finance', disabled: true },
  { title: '會計', path: '/accounting', disabled: true },
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
