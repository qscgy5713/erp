<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { DocStatus } from '@/api/inventory'
import { purchaseApi, type OrderRow, type ReceiptState } from '@/api/purchase'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'
import SupplierPicker from '@/components/SupplierPicker.vue'

const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('purchase.order.write'))
const { handle } = useApiError()

const receiptStates: Record<ReceiptState, { label: string; type: 'info' | 'warning' | 'success' }> =
  {
    none: { label: '未交', type: 'info' },
    partial: { label: '部分交貨', type: 'warning' },
    full: { label: '已交齊', type: 'success' },
  }
// 採購單不過帳
const orderStatuses = Object.fromEntries(
  Object.entries(statusLabels).filter(([k]) => k !== 'posted'),
) as Partial<Record<DocStatus, string>>

const query = reactive({
  status: '' as DocStatus | '',
  supplier_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<OrderRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await purchaseApi.orders({
      ...rest,
      keyword: rest.keyword.trim(),
      from: range?.[0],
      to: range?.[1],
    })
    rows.value = res.items
    meta.value = res.meta
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function search() {
  query.page = 1
  load()
}

function open(row: OrderRow) {
  router.push({ name: 'purchase-order', params: { id: row.id } })
}

const money = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.status"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option v-for="(label, v) in orderStatuses" :key="v" :label="label" :value="v" />
      </el-select>
      <div style="width: 200px">
        <SupplierPicker v-model="query.supplier_id" @update:model-value="search" />
      </div>
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 240px"
        @change="search"
      />
      <el-input
        v-model="query.keyword"
        placeholder="單號"
        clearable
        style="width: 160px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.supplier_id" link @click="((query.supplier_id = null), search())">
        清除供應商
      </el-button>
      <span class="spacer" />
      <el-button
        v-if="canWrite"
        type="primary"
        @click="router.push({ name: 'purchase-order-new' })"
      >
        新增採購單
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="供應商" min-width="180">
        <template #default="{ row }">{{ row.supplier_code }} {{ row.supplier_name }}</template>
      </el-table-column>
      <el-table-column label="合計" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.total_amount) }}</template>
      </el-table-column>
      <el-table-column prop="expected_date" label="預定交貨" width="110" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><DocStatusTag :status="row.status" /></template>
      </el-table-column>
      <el-table-column label="交貨" width="100">
        <template #default="{ row }">
          <el-tag
            v-if="row.status === 'approved' || row.status === 'closed'"
            :type="receiptStates[row.receipt_state as ReceiptState].type"
            effect="plain"
          >
            {{ receiptStates[row.receipt_state as ReceiptState].label }}
          </el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="建立者" width="110" />
      <el-table-column prop="note" label="備註" min-width="160" show-overflow-tooltip />
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[20, 50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>
  </div>
</template>
