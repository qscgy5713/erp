<script setup lang="ts">
// 供應商搜尋選擇:輸入代號、名稱或簡稱遠端搜尋;選中後回傳預設幣別、稅別、付款條件
import { ref, watch } from 'vue'
import { purchaseApi, type SupplierOption } from '@/api/purchase'

const props = defineProps<{
  modelValue: number | null
  /** 已選供應商的顯示文字(編輯既有單據時,選項尚未載入) */
  label?: string
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  select: [supplier: SupplierOption | null]
}>()

const options = ref<SupplierOption[]>([])
const loading = ref(false)
let seq = 0

async function search(keyword: string) {
  const mySeq = ++seq
  loading.value = true
  try {
    const res = await purchaseApi.supplierOptions(keyword.trim())
    if (mySeq === seq) options.value = res // 只採用最後一次搜尋的結果
  } finally {
    if (mySeq === seq) loading.value = false
  }
}

watch(
  () => [props.modelValue, props.label] as const,
  ([v, label]) => {
    if (v && label && !options.value.some((o) => o.id === v)) {
      options.value = [
        {
          id: v,
          code: '',
          name: label,
          short_name: '',
          currency: '',
          tax_type_id: null,
          payment_term_id: null,
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
    placeholder="代號 / 名稱"
    style="width: 100%"
    @update:model-value="onChange"
    @focus="options.length <= 1 && search('')"
  >
    <el-option
      v-for="o in options"
      :key="o.id"
      :value="o.id"
      :label="o.code ? `${o.code} ${o.name}` : o.name"
    />
  </el-select>
</template>
