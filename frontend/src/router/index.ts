import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { INVENTORY_ANY, ORDER_ANY, RECEIPT_ANY } from '@/navigation'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 不需登入 */
    public?: boolean
    /** 不顯示側邊選單版面 */
    blank?: boolean
    /** 需要任一權限 */
    perm?: string[]
    /** 採購編輯頁:採購單(order)或進貨 / 退出單(receipt) */
    kind?: 'order' | 'receipt'
  }
}

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { title: '登入', public: true, blank: true },
    },
    {
      path: '/change-password',
      name: 'change-password',
      component: () => import('@/views/ChangePasswordView.vue'),
      meta: { title: '變更密碼' },
    },
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
      meta: { title: '首頁' },
    },
    {
      path: '/masterdata/items',
      component: () => import('@/views/masterdata/ItemsView.vue'),
      meta: { title: '料品', perm: ['masterdata.item.read'] },
    },
    {
      path: '/masterdata/item-categories',
      component: () => import('@/views/masterdata/CategoriesView.vue'),
      meta: { title: '料品分類', perm: ['masterdata.item.read'] },
    },
    {
      path: '/masterdata/units',
      component: () => import('@/views/masterdata/UnitsView.vue'),
      meta: { title: '單位', perm: ['masterdata.item.read'] },
    },
    {
      path: '/masterdata/warehouses',
      component: () => import('@/views/masterdata/WarehousesView.vue'),
      meta: { title: '倉庫', perm: ['masterdata.warehouse.read'] },
    },
    {
      path: '/masterdata/customers',
      component: () => import('@/views/masterdata/PartnersView.vue'),
      props: { kind: 'customer' },
      meta: { title: '客戶', perm: ['masterdata.customer.read'] },
    },
    {
      path: '/masterdata/suppliers',
      component: () => import('@/views/masterdata/PartnersView.vue'),
      props: { kind: 'supplier' },
      meta: { title: '供應商', perm: ['masterdata.supplier.read'] },
    },
    {
      path: '/masterdata/finance',
      component: () => import('@/views/masterdata/FinanceSettingsView.vue'),
      meta: { title: '財務設定', perm: ['masterdata.finance.read', 'masterdata.finance.write'] },
    },
    {
      path: '/inventory/balances',
      component: () => import('@/views/inventory/BalancesView.vue'),
      meta: { title: '現有量', perm: INVENTORY_ANY },
    },
    {
      path: '/inventory/movement-summary',
      component: () => import('@/views/inventory/MovementSummaryView.vue'),
      meta: { title: '收發存', perm: INVENTORY_ANY },
    },
    {
      path: '/inventory/documents',
      name: 'inventory-documents',
      component: () => import('@/views/inventory/DocumentsView.vue'),
      meta: { title: '庫存單據', perm: INVENTORY_ANY },
    },
    {
      path: '/inventory/documents/new',
      name: 'inventory-document-new',
      component: () => import('@/views/inventory/DocumentEditView.vue'),
      meta: { title: '新增庫存單據', perm: ['inventory.stock.write'] },
    },
    {
      path: '/inventory/documents/:id(\\d+)',
      name: 'inventory-document',
      component: () => import('@/views/inventory/DocumentEditView.vue'),
      meta: { title: '庫存單據', perm: INVENTORY_ANY },
    },
    {
      path: '/purchase/orders',
      name: 'purchase-orders',
      component: () => import('@/views/purchase/OrdersView.vue'),
      meta: { title: '採購單', perm: ORDER_ANY },
    },
    {
      path: '/purchase/orders/new',
      name: 'purchase-order-new',
      component: () => import('@/views/purchase/PurchaseEditView.vue'),
      meta: { title: '新增採購單', perm: ['purchase.order.write'], kind: 'order' },
    },
    {
      path: '/purchase/orders/:id(\\d+)',
      name: 'purchase-order',
      component: () => import('@/views/purchase/PurchaseEditView.vue'),
      meta: { title: '採購單', perm: ORDER_ANY, kind: 'order' },
    },
    {
      path: '/purchase/receipts',
      name: 'purchase-receipts',
      component: () => import('@/views/purchase/ReceiptsView.vue'),
      meta: { title: '進貨 / 退出', perm: RECEIPT_ANY },
    },
    {
      path: '/purchase/receipts/new',
      name: 'purchase-receipt-new',
      component: () => import('@/views/purchase/PurchaseEditView.vue'),
      meta: { title: '新增進貨 / 退出單', perm: ['purchase.receipt.write'], kind: 'receipt' },
    },
    {
      path: '/purchase/receipts/:id(\\d+)',
      name: 'purchase-receipt',
      component: () => import('@/views/purchase/PurchaseEditView.vue'),
      meta: { title: '進貨 / 退出單', perm: RECEIPT_ANY, kind: 'receipt' },
    },
    {
      path: '/purchase/outstanding',
      component: () => import('@/views/purchase/OutstandingView.vue'),
      meta: { title: '未交貨清單', perm: [...ORDER_ANY, 'purchase.receipt.write'] },
    },
    {
      path: '/finance/payables',
      component: () => import('@/views/finance/PayablesView.vue'),
      meta: { title: '應付帳款', perm: ['finance.payable.read'] },
    },
    {
      path: '/system/departments',
      component: () => import('@/views/system/DepartmentsView.vue'),
      meta: { title: '部門', perm: ['system.department.read'] },
    },
    {
      path: '/system/users',
      component: () => import('@/views/system/UsersView.vue'),
      meta: { title: '使用者', perm: ['system.user.read'] },
    },
    {
      path: '/system/roles',
      component: () => import('@/views/system/RolesView.vue'),
      meta: { title: '角色權限', perm: ['system.role.read'] },
    },
    {
      path: '/system/doc-number-rules',
      component: () => import('@/views/system/DocNumberRulesView.vue'),
      meta: { title: '單號規則', perm: ['system.docno.read', 'system.docno.write'] },
    },
    {
      path: '/system/audit-logs',
      component: () => import('@/views/system/AuditLogsView.vue'),
      meta: { title: '稽核日誌', perm: ['system.audit.read'] },
    },
    {
      path: '/forbidden',
      name: 'forbidden',
      component: () => import('@/views/ForbiddenView.vue'),
      meta: { title: '沒有權限' },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { title: '找不到頁面' },
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()
  await auth.init()

  if (to.meta.public) {
    // 已登入就不必再看登入頁
    return auth.isLoggedIn && to.name === 'login' ? '/' : true
  }
  if (!auth.isLoggedIn) {
    return { name: 'login', query: to.fullPath !== '/' ? { redirect: to.fullPath } : {} }
  }
  if (auth.mustChangePassword && to.name !== 'change-password') {
    return { name: 'change-password' }
  }
  if (!auth.can(to.meta.perm)) {
    return { name: 'forbidden' }
  }
  return true
})

router.afterEach((to) => {
  document.title = to.meta.title ? `${to.meta.title} - ERP` : 'ERP'
})

export default router
