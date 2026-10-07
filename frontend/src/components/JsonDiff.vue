<script setup lang="ts">
import { computed } from 'vue'

type Row = { key: string; before: string; after: string; changed: boolean }

const props = defineProps<{
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
}>()

// 列出前後差異;只有一邊時(新增/刪除)列出全部欄位
const rows = computed(() => {
  const keys = new Set([...Object.keys(props.before ?? {}), ...Object.keys(props.after ?? {})])
  const out: Row[] = []
  for (const key of [...keys].sort()) {
    const b = props.before ? JSON.stringify(props.before[key] ?? null) : ''
    const a = props.after ? JSON.stringify(props.after[key] ?? null) : ''
    const changed = props.before !== null && props.after !== null && a !== b
    if (props.before && props.after && !changed && ['updated_at', 'version'].includes(key)) continue
    out.push({ key, before: b, after: a, changed })
  }
  return out
})
</script>

<template>
  <el-table
    :data="rows"
    size="small"
    border
    :row-class-name="({ row }: { row: Row }) => (row.changed ? 'changed' : '')"
  >
    <el-table-column prop="key" label="欄位" width="180" />
    <el-table-column v-if="before" prop="before" label="修改前" />
    <el-table-column v-if="after" prop="after" label="修改後" />
  </el-table>
</template>

<style scoped>
:deep(.changed) {
  --el-table-tr-bg-color: var(--el-color-warning-light-9);
  font-weight: 600;
}
</style>
