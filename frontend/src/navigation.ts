// 側邊選單。perm 為可見所需權限(任一即可);disabled 為尚未實作的模組
export interface NavItem {
  title: string
  path?: string
  perm?: string[]
  disabled?: boolean
  children?: NavItem[]
}

export const navigation: NavItem[] = [
  { title: '首頁', path: '/' },
  { title: '基本資料', path: '/masterdata', disabled: true },
  { title: '採購', path: '/purchase', disabled: true },
  { title: '銷售', path: '/sales', disabled: true },
  { title: '庫存', path: '/inventory', disabled: true },
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
