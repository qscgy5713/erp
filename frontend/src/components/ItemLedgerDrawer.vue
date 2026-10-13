<script setup lang="ts">
// 料品異動明細:期初 + 每筆異動與結存
import { ref, watch } from 'vue'
import { inventoryApi, type LedgerEntry } from '@/api/inventory'
import { useApiError } from '@/composables/useApiError'

const props = defineProps<{
  itemId: number | null
  title: string
  warehouseId?: number | null
  from: string
  to: string
}>()
const visible = defineModel<boolean>({ required: true })
const { handle } = useApiError()

const opening = ref('0')
const entries = ref<LedgerEntry[]>([])
const truncated = ref(false)
const loading = ref(false)

const sourceLabels: Record<string, string> = {
  stock_adjustment: '調整',
  stock_transfer: '調撥',
  stock_count: '盤點',
  goods_receipt: '進貨',
  purchase_return: '進貨退出',
  delivery: '出貨',
  sales_return: '銷貨退回',
  opening_stock: '期初庫存',
  work_order_issue: '工單領料',
  work_order_receipt: '完工入庫',
  wo_completion_issue: '完工領料',
  wo_completion_receipt: '完工入庫',
  wo_wip_issue: '工單領料(在製品)',
  wo_wip_return: '工單退料(在製品)',
  sc_issue: '委外發料',
  sc_return: '委外退料',
  sc_receive_issue: '委外耗料',
  sc_receive_receipt: '委外收回入庫',
}

watch(
  () => [visible.value, props.itemId, props.warehouseId, props.from, props.to],
  async () => {
    if (!visible.value || !props.itemId) return
    loading.value = true
    try {
      const res = await inventoryApi.itemLedger(props.itemId, {
        from: props.from,
        to: props.to,
        warehouse_id: props.warehouseId,
      })
      opening.value = res.opening_qty
      entries.value = res.entries
      truncated.value = res.truncated
    } catch (e) {
      handle(e)
    } finally {
      loading.value = false
    }
  },
)
</script>

<template>
  <el-drawer v-model="visible" :title="title" size="60%">
    <el-alert
      v-if="truncated"
      type="warning"
      :closable="false"
      show-icon
      title="異動筆數過多,只顯示前 1000 筆,請縮小期間"
      style="margin-bottom: 8px"
    />
    <p class="muted">{{ from }} ~ {{ to }},期初結存 {{ opening }}</p>
    <el-table v-loading="loading" :data="entries" border size="small">
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="來源" width="160">
        <template #default="{ row }">
          {{ sourceLabels[row.source_type] ?? row.source_type }} {{ row.source_no }}
          <el-tag v-if="row.is_reversal" type="danger" size="small">沖銷</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="warehouse_name" label="倉庫" width="120" />
      <el-table-column label="收" width="100" align="right">
        <template #default="{ row }">{{ Number(row.qty) > 0 ? row.qty : '' }}</template>
      </el-table-column>
      <el-table-column label="發" width="100" align="right">
        <template #default="{ row }">{{
          Number(row.qty) < 0 ? String(-Number(row.qty)) : ''
        }}</template>
      </el-table-column>
      <el-table-column prop="balance_qty" label="結存" align="right" />
    </el-table>
  </el-drawer>
</template>

<style scoped>
.muted {
  color: var(--el-text-color-secondary);
  margin: 0 0 8px;
}
</style>
