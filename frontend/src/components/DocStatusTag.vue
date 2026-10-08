<script setup lang="ts">
import type { DocStatus } from '@/api/inventory'
import { statusLabels, statusTagType } from '@/utils/docstate'

defineProps<{ status: DocStatus }>()
</script>

<template>
  <el-tag :type="statusTagType[status]">{{ statusLabels[status] }}</el-tag>
</template>
