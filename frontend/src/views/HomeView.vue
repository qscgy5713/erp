<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { getHealth } from '@/api/system'

const status = ref<'loading' | 'ok' | 'error'>('loading')
const message = ref('')

onMounted(async () => {
  try {
    await getHealth()
    status.value = 'ok'
  } catch (e) {
    status.value = 'error'
    message.value = e instanceof Error ? e.message : String(e)
  }
})
</script>

<template>
  <el-card header="系統狀態">
    <el-tag v-if="status === 'loading'" type="info">檢查中…</el-tag>
    <el-tag v-else-if="status === 'ok'" type="success">API 與資料庫正常</el-tag>
    <el-tag v-else type="danger">異常:{{ message }}</el-tag>
  </el-card>
</template>
