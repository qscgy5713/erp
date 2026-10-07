<script setup lang="ts">
import { useRoute } from 'vue-router'

const route = useRoute()

// 選單依 doc/plan.md 的模組規劃;尚未實作的模組先停用
const menus = [
  { index: '/', title: '首頁', enabled: true },
  { index: '/masterdata', title: '基本資料', enabled: false },
  { index: '/purchase', title: '採購', enabled: false },
  { index: '/sales', title: '銷售', enabled: false },
  { index: '/inventory', title: '庫存', enabled: false },
  { index: '/finance', title: '應收應付', enabled: false },
  { index: '/accounting', title: '會計', enabled: false },
  { index: '/system', title: '系統管理', enabled: false },
]
</script>

<template>
  <el-container class="layout">
    <el-aside width="200px" class="aside">
      <div class="brand">ERP</div>
      <el-menu :default-active="route.path" router>
        <el-menu-item v-for="m in menus" :key="m.index" :index="m.index" :disabled="!m.enabled">
          {{ m.title }}
        </el-menu-item>
      </el-menu>
    </el-aside>
    <el-container>
      <el-header class="header">
        <span>{{ route.meta.title }}</span>
      </el-header>
      <el-main>
        <RouterView />
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
  border-bottom: 1px solid var(--el-border-color);
}
</style>
