import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import {
  COLLECTION_ANY,
  DELIVERY_ANY,
  INVENTORY_ANY,
  ORDER_ANY,
  PAYMENT_ANY,
  RECEIPT_ANY,
  SALES_ORDER_ANY,
  VOUCHER_ANY,
} from '@/navigation'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 不需登入 */
    public?: boolean
    /** 不顯示側邊選單版面 */
    blank?: boolean
    /** 需要任一權限 */
    perm?: string[]
    /** 採購 / 銷售共用編輯頁的流程,見 views/trade/flows.ts */
    kind?: 'purchase-order' | 'receipt' | 'sales-order' | 'delivery'
    /** 應收 / 應付帳款、對帳單、帳齡頁 */
    ledger?: 'receivable' | 'payable'
    /** 收款單 / 付款單頁 */
    settle?: 'collection' | 'payment'
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
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '新增採購單', perm: ['purchase.order.write'], kind: 'purchase-order' },
    },
    {
      path: '/purchase/orders/:id(\\d+)',
      name: 'purchase-order',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '採購單', perm: ORDER_ANY, kind: 'purchase-order' },
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
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '新增進貨 / 退出單', perm: ['purchase.receipt.write'], kind: 'receipt' },
    },
    {
      path: '/purchase/receipts/:id(\\d+)',
      name: 'purchase-receipt',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '進貨 / 退出單', perm: RECEIPT_ANY, kind: 'receipt' },
    },
    {
      path: '/purchase/outstanding',
      component: () => import('@/views/purchase/OutstandingView.vue'),
      meta: { title: '未交貨清單', perm: [...ORDER_ANY, 'purchase.receipt.write'] },
    },
    {
      path: '/sales/orders',
      name: 'sales-orders',
      component: () => import('@/views/sales/SalesOrdersView.vue'),
      meta: { title: '報價 / 訂單', perm: SALES_ORDER_ANY },
    },
    {
      path: '/sales/orders/new',
      name: 'sales-order-new',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '新增報價 / 訂單', perm: ['sales.order.write'], kind: 'sales-order' },
    },
    {
      path: '/sales/orders/:id(\\d+)',
      name: 'sales-order',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '報價 / 訂單', perm: SALES_ORDER_ANY, kind: 'sales-order' },
    },
    {
      path: '/sales/deliveries',
      name: 'sales-deliveries',
      component: () => import('@/views/sales/DeliveriesView.vue'),
      meta: { title: '出貨 / 退回', perm: DELIVERY_ANY },
    },
    {
      path: '/sales/deliveries/new',
      name: 'sales-delivery-new',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '新增出貨 / 退回單', perm: ['sales.delivery.write'], kind: 'delivery' },
    },
    {
      path: '/sales/deliveries/:id(\\d+)',
      name: 'sales-delivery',
      component: () => import('@/views/trade/TradeEditView.vue'),
      meta: { title: '出貨 / 退回單', perm: DELIVERY_ANY, kind: 'delivery' },
    },
    {
      path: '/sales/unshipped',
      component: () => import('@/views/sales/UnshippedView.vue'),
      meta: { title: '未出貨清單', perm: [...SALES_ORDER_ANY, 'sales.delivery.write'] },
    },
    {
      path: '/finance/collections',
      name: 'collections',
      component: () => import('@/views/finance/SettlementsView.vue'),
      meta: { title: '收款單', perm: COLLECTION_ANY, settle: 'collection' },
    },
    {
      path: '/finance/collections/new',
      name: 'collection-new',
      component: () => import('@/views/finance/SettlementEditView.vue'),
      meta: { title: '新增收款單', perm: ['finance.collection.write'], settle: 'collection' },
    },
    {
      path: '/finance/collections/:id(\\d+)',
      name: 'collection',
      component: () => import('@/views/finance/SettlementEditView.vue'),
      meta: { title: '收款單', perm: COLLECTION_ANY, settle: 'collection' },
    },
    {
      path: '/finance/payments',
      name: 'payments',
      component: () => import('@/views/finance/SettlementsView.vue'),
      meta: { title: '付款單', perm: PAYMENT_ANY, settle: 'payment' },
    },
    {
      path: '/finance/payments/new',
      name: 'payment-new',
      component: () => import('@/views/finance/SettlementEditView.vue'),
      meta: { title: '新增付款單', perm: ['finance.payment.write'], settle: 'payment' },
    },
    {
      path: '/finance/payments/:id(\\d+)',
      name: 'payment',
      component: () => import('@/views/finance/SettlementEditView.vue'),
      meta: { title: '付款單', perm: PAYMENT_ANY, settle: 'payment' },
    },
    {
      path: '/finance/statement/receivable',
      component: () => import('@/views/finance/StatementView.vue'),
      meta: { title: '應收對帳單', perm: ['finance.receivable.read'], ledger: 'receivable' },
    },
    {
      path: '/finance/statement/payable',
      component: () => import('@/views/finance/StatementView.vue'),
      meta: { title: '應付對帳單', perm: ['finance.payable.read'], ledger: 'payable' },
    },
    {
      path: '/finance/aging/receivable',
      component: () => import('@/views/finance/AgingView.vue'),
      meta: { title: '應收帳齡', perm: ['finance.receivable.read'], ledger: 'receivable' },
    },
    {
      path: '/finance/aging/payable',
      component: () => import('@/views/finance/AgingView.vue'),
      meta: { title: '應付帳齡', perm: ['finance.payable.read'], ledger: 'payable' },
    },
    {
      path: '/finance/receivables',
      component: () => import('@/views/finance/LedgerView.vue'),
      meta: { title: '應收帳款', perm: ['finance.receivable.read'], ledger: 'receivable' },
    },
    {
      path: '/finance/payables',
      component: () => import('@/views/finance/LedgerView.vue'),
      meta: { title: '應付帳款', perm: ['finance.payable.read'], ledger: 'payable' },
    },
    {
      path: '/costing/closings',
      component: () => import('@/views/costing/ClosingsView.vue'),
      meta: { title: '月結成本', perm: ['costing.read', 'costing.close'] },
    },
    {
      path: '/costing/reconcile',
      component: () => import('@/views/costing/ReconcileView.vue'),
      meta: { title: '對帳檢查', perm: ['costing.read', 'costing.close'] },
    },
    {
      path: '/gl/vouchers',
      name: 'gl-vouchers',
      component: () => import('@/views/gl/VouchersView.vue'),
      meta: { title: '傳票', perm: VOUCHER_ANY },
    },
    {
      path: '/gl/vouchers/new',
      name: 'gl-voucher-new',
      component: () => import('@/views/gl/VoucherEditView.vue'),
      meta: { title: '新增傳票', perm: ['gl.voucher.write'] },
    },
    {
      path: '/gl/vouchers/:id(\\d+)',
      name: 'gl-voucher',
      component: () => import('@/views/gl/VoucherEditView.vue'),
      meta: { title: '傳票', perm: VOUCHER_ANY },
    },
    {
      path: '/gl/reports',
      component: () => import('@/views/gl/ReportsView.vue'),
      meta: { title: '會計報表', perm: ['gl.report.read'] },
    },
    {
      path: '/gl/statements',
      component: () => import('@/views/gl/StatementsView.vue'),
      meta: { title: '財務報表', perm: ['gl.report.read'] },
    },
    {
      path: '/gl/vat',
      component: () => import('@/views/gl/VatView.vue'),
      meta: { title: '營業稅申報', perm: ['gl.report.read'] },
    },
    {
      path: '/gl/periods',
      component: () => import('@/views/gl/PeriodsView.vue'),
      meta: { title: '會計期間', perm: ['gl.period.read', 'gl.period.close'] },
    },
    {
      path: '/gl/accounts',
      component: () => import('@/views/gl/AccountsView.vue'),
      meta: { title: '會計科目', perm: ['gl.account.read', 'gl.account.write'] },
    },
    {
      path: '/gl/mappings',
      component: () => import('@/views/gl/MappingsView.vue'),
      meta: { title: '拋轉規則', perm: ['gl.account.read', 'gl.account.write'] },
    },
    {
      path: '/system/imports',
      component: () => import('@/views/system/ImportsView.vue'),
      meta: { title: '資料匯入', perm: ['system.import.run'] },
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
