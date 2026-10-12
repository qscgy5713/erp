<script setup lang="ts">
import { useRoute } from 'vue-router'
import AppLayout from '@/layouts/AppLayout.vue'

const route = useRoute()
</script>

<template>
  <RouterView v-if="route.meta.blank" />
  <AppLayout v-else />
</template>

<style>
html,
body,
#app {
  height: 100%;
  margin: 0;
}
.page-toolbar {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
.page-toolbar .spacer {
  flex: 1;
}
.pager {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
@media print {
  .el-aside,
  .el-header,
  .page-toolbar,
  .doc-header .actions,
  .no-print {
    display: none !important;
  }
  html,
  body,
  #app,
  .el-container,
  .el-main {
    height: auto !important;
    overflow: visible !important;
  }
}
</style>
