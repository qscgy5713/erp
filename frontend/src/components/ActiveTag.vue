<script setup lang="ts">
defineProps<{ active: boolean }>()
</script>

<template>
  <el-tag :type="active ? 'success' : 'info'">{{ active ? '啟用' : '停用' }}</el-tag>
</template>
