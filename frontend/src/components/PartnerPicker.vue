<script setup lang="ts">
// 往來對象搜尋選擇:供應商或客戶(客戶依資料範圍過濾);選中後回傳預設幣別、稅別、付款條件
import { ref, watch } from 'vue'
import { purchaseApi } from '@/api/purchase'
import { salesApi } from '@/api/sales'

export interface PartnerOption {
  id: number
  code: string
  name: string
  currency: string
  tax_type_id: number | null
  payment_term_id: number | null
  sales_user_name?: string | null
}

const props = defineProps<{
  kind: 'supplier' | 'customer'
  modelValue: number | null
  /** 已選對象的顯示文字(編輯既有單據時,選項尚未載入) */
  label?: string
  disabled?: boolean
}>()
const emit = defineEmits<{
  'update:modelValue': [value: number | null]
  select: [partner: PartnerOption | null]
}>()

const options = ref<PartnerOption[]>([])
const loading = ref(false)
let seq = 0

async function search(keyword: string) {
  const mySeq = ++seq
  loading.value = true
  try {
    const k = keyword.trim()
    const res =
      props.kind === 'supplier'
        ? await purchaseApi.supplierOptions(k)
        : await salesApi.customerOptions(k)
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
        { id: v, code: '', name: label, currency: '', tax_type_id: null, payment_term_id: null },
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
    :placeholder="kind === 'supplier' ? '供應商代號 / 名稱' : '客戶代號 / 名稱'"
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
