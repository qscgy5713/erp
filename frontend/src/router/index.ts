import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

declare module 'vue-router' {
  interface RouteMeta {
    title?: string
    /** 不需登入 */
    public?: boolean
    /** 不顯示側邊選單版面 */
    blank?: boolean
    /** 需要任一權限 */
    perm?: string[]
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
