<script setup lang="ts">
// 明細列的儲位:倉庫有啟用儲位才顯示。入庫必填(從該倉庫的儲位挑選);出庫可留空,系統自動分配(庫存多的儲位先出)。
import { computed, ref, watch } from 'vue'
import type { Bin } from '@/api/masterdata'
import { useBinStore } from '@/composables/useBinStore'

const props = defineProps<{
  warehouseId: number | null | undefined
  /** in 入庫必填 / out 出庫可留空 */
  mode: 'in' | 'out'
  editable: boolean
  placeholder?: string
}>()
const code = defineModel<string>('code', { default: '' })

const store = useBinStore()
const bins = ref<Bin[]>([])
const enabled = computed(() => !!props.warehouseId && store.usesBins(props.warehouseId))

watch(
  () => props.warehouseId,
  async (id) => {
    bins.value = []
    if (!id) return
    await store.ensureWarehouses()
    if (store.usesBins(id)) bins.value = await store.binsOf(id)
  },
  { immediate: true },
)

// 倉庫換了、原本的儲位不屬於新倉庫就清掉
watch(bins, (list) => {
  if (code.value && list.length && !list.some((b) => b.code === code.value)) code.value = ''
})

const options = computed(() => bins.value.filter((b) => b.is_active))
</script>

<template>
  <template v-if="enabled">
    <el-select
      v-if="editable"
      v-model="code"
      :clearable="mode === 'out'"
      size="small"
      :placeholder="placeholder ?? (mode === 'in' ? '儲位(必填)' : '自動分配')"
      style="width: 100%"
    >
      <el-option
        v-for="b in options"
        :key="b.id"
        :value="b.code"
        :label="b.name ? `${b.code} ${b.name}` : b.code"
      />
    </el-select>
    <span v-else>{{ code }}</span>
  </template>
</template>
