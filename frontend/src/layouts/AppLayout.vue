<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { navigation, type NavItem } from '@/navigation'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

// 只顯示有權限的選單;群組內全部沒權限則整組隱藏
const menus = computed(() =>
  navigation
    .map((item): NavItem | null => {
      if (!item.children) return auth.can(item.perm) ? item : null
      const children = item.children.filter((c) => auth.can(c.perm))
      return children.length ? { ...item, children } : null
    })
    .filter((m): m is NavItem => m !== null),
)

// 窄螢幕(<768px):側欄改成抽屜,以標題列的選單鈕開啟;換頁後自動收起
const narrowQuery = window.matchMedia('(max-width: 767px)')
const narrow = ref(narrowQuery.matches)
const drawer = ref(false)
const onNarrowChange = (e: MediaQueryListEvent) => {
  narrow.value = e.matches
  if (!e.matches) drawer.value = false
}
narrowQuery.addEventListener('change', onNarrowChange)
onBeforeUnmount(() => narrowQuery.removeEventListener('change', onNarrowChange))
watch(
  () => route.path,
  () => (drawer.value = false),
)

async function onCommand(cmd: string) {
  if (cmd === 'password') {
    router.push({ name: 'change-password' })
  } else if (cmd === 'notifications') {
    router.push({ name: 'account-notifications' })
  } else if (cmd === 'security') {
    router.push({ name: 'account-security' })
  } else if (cmd === 'logout') {
    await auth.logout()
    router.replace({ name: 'login' })
  }
}
</script>

<template>
  <el-container class="layout">
    <el-aside v-if="!narrow" width="200px" class="aside">
      <div class="brand">ERP</div>
      <el-menu :default-active="route.path" router>
        <template v-for="m in menus" :key="m.title">
          <el-sub-menu v-if="m.children" :index="m.title">
            <template #title>{{ m.title }}</template>
            <el-menu-item v-for="c in m.children" :key="c.path" :index="c.path!">
              {{ c.title }}
            </el-menu-item>
          </el-sub-menu>
          <el-menu-item v-else :index="m.path!" :disabled="m.disabled">
            {{ m.title }}
          </el-menu-item>
        </template>
      </el-menu>
    </el-aside>
    <el-drawer
      v-else
      v-model="drawer"
      direction="ltr"
      size="220px"
      :with-header="false"
      class="nav-drawer"
    >
      <div class="brand">ERP</div>
      <el-menu :default-active="route.path" router>
        <template v-for="m in menus" :key="m.title">
          <el-sub-menu v-if="m.children" :index="m.title">
            <template #title>{{ m.title }}</template>
            <el-menu-item v-for="c in m.children" :key="c.path" :index="c.path!">
              {{ c.title }}
            </el-menu-item>
          </el-sub-menu>
          <el-menu-item v-else :index="m.path!" :disabled="m.disabled">
            {{ m.title }}
          </el-menu-item>
        </template>
      </el-menu>
    </el-drawer>
    <el-container>
      <el-header class="header">
        <el-button v-if="narrow" text class="menu-btn" aria-label="選單" @click="drawer = true"
          >☰</el-button
        >
        <span class="title">{{ route.meta.title }}</span>
        <el-dropdown v-if="auth.user" trigger="click" @command="onCommand">
          <el-button text> {{ auth.user.name }}({{ auth.user.username }}) </el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="password">變更密碼</el-dropdown-item>
              <el-dropdown-item command="notifications">通知設定</el-dropdown-item>
              <el-dropdown-item command="security">帳號安全(雙因素驗證)</el-dropdown-item>
              <el-dropdown-item command="logout" divided>登出</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </el-header>
      <el-main>
        <!-- 採購 / 銷售單據共用編輯頁:換單據(轉單、開來源單)時要重建元件,不能沿用前一張的狀態 -->
        <RouterView v-slot="{ Component, route: r }">
          <component
            :is="Component"
            :key="
              r.meta.kind || r.meta.settle || r.meta.ledger || r.path.startsWith('/gl/vouchers')
                ? r.path
                : ''
            "
          />
        </RouterView>
      </el-main>
    </el-container>
  </el-container>
</template>

<style scoped>
.layout {
  height: 100%;
}
.aside {
  border-right: 1px solid var(--el-border-color);
}
.aside :deep(.el-menu) {
  border-right: none;
}
.brand {
  height: 60px;
  line-height: 60px;
  text-align: center;
  font-weight: 700;
  font-size: 20px;
}
.header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--el-border-color);
}
.title {
  font-weight: 600;
  flex: 1;
}
.menu-btn {
  font-size: 20px;
  padding: 0 8px;
  margin-right: 4px;
}
@media (max-width: 767px) {
  .header {
    padding: 0 8px;
  }
  :deep(.el-main) {
    padding: 12px 8px;
  }
}
</style>

<style>
/* 抽屜內不留預設邊距,選單撐滿 */
.nav-drawer .el-drawer__body {
  padding: 0;
}
.nav-drawer .el-menu {
  border-right: none;
}
@media (max-width: 767px) {
  /* 固定寬度的對話框不超出螢幕 */
  .el-dialog {
    width: 94vw !important;
    max-width: 94vw;
  }
}
</style>
