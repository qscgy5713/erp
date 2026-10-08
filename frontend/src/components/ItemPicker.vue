<script setup lang="ts">
// 料品搜尋選擇:輸入料號、品名、規格或條碼遠端搜尋;選中後回傳完整料品(含換算單位)
import { ref, watch } from 'vue'
import { inventoryApi, type ItemOption } from '@/api/inventory'

const props = defineProps<{
  modelValue: number | null
  /** 已選料品的顯示文字(編輯既有單據時,選項尚未載入) */
  label?: string
  itemType?: 'goods' | 'service'
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  select: [item: ItemOption | null]
}>()

const options = ref<ItemOption[]>([])
const loading = ref(false)
let seq = 0

async function search(keyword: string) {
  const mySeq = ++seq
  loading.value = true
  try {
    const res = await inventoryApi.itemOptions(keyword.trim(), props.itemType)
    if (mySeq === seq) options.value = res // 只採用最後一次搜尋的結果,避免回應順序錯亂
  } finally {
    if (mySeq === seq) loading.value = false
  }
}

watch(
  () => props.modelValue,
  (v) => {
    if (v && props.label && !options.value.some((o) => o.id === v)) {
      // 讓 el-select 能顯示既有值
      options.value = [
        {
          id: v,
          code: '',
          name: props.label,
          spec: '',
          item_type: 'goods',
          base_unit_id: 0,
          base_unit_name: '',
          units: [],
        },
      ]
    }
  },
  { immediate: true },
)

function onChange(id: number | null) {
  emit('update:modelValue', id)
  emit('select', options.value.find((o) => o.id === id) ?? null)
}
</script>

<template>
  <el-select
    :model-value="modelValue"
    filterable
    remote
    :remote-method="search"
    :loading="loading"
    :disabled="disabled"
    placeholder="料號 / 品名 / 條碼"
    style="width: 100%"
    @update:model-value="onChange"
    @focus="options.length <= 1 && search('')"
  >
    <el-option
      v-for="o in options"
      :key="o.id"
      :value="o.id"
      :label="o.code ? `${o.code} ${o.name}` : o.name"
    >
      <span>{{ o.code }}</span>
      <span class="name">{{ o.name }}</span>
      <span v-if="o.spec" class="spec">{{ o.spec }}</span>
    </el-option>
  </el-select>
</template>

<style scoped>
.name {
  margin-left: 8px;
}
.spec {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
