<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DocStatus } from '@/api/inventory'
import { purchaseApi, type ReceiptDocType, type ReceiptRow } from '@/api/purchase'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'
import PartnerPicker from '@/components/PartnerPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can('purchase.receipt.write'))
const { handle } = useApiError()

const typeLabels: Record<ReceiptDocType, string> = { receipt: '進貨', return: '退出' }
// 進貨單不使用結案
const receiptStatuses = Object.fromEntries(
  Object.entries(statusLabels).filter(([k]) => k !== 'closed'),
) as Partial<Record<DocStatus, string>>

const query = reactive({
  doc_type: '' as ReceiptDocType | '',
  status: ((route.query.status as string | undefined) ?? '') as DocStatus | '',
  supplier_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<ReceiptRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await purchaseApi.receipts({
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

function open(row: ReceiptRow) {
  router.push({ name: 'purchase-receipt', params: { id: row.id } })
}

function create(type: ReceiptDocType) {
  router.push({ name: 'purchase-receipt-new', query: { type } })
}

const money = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select
        v-model="query.doc_type"
        clearable
        placeholder="類型"
        style="width: 100px"
        @change="search"
      >
        <el-option v-for="(label, v) in typeLabels" :key="v" :label="label" :value="v" />
      </el-select>
      <el-select
        v-model="query.status"
        clearable
        placeholder="狀態"
        style="width: 110px"
        @change="search"
      >
        <el-option v-for="(label, v) in receiptStatuses" :key="v" :label="label" :value="v" />
      </el-select>
      <div style="width: 200px">
        <PartnerPicker kind="supplier" v-model="query.supplier_id" @update:model-value="search" />
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
        placeholder="單號 / 發票號碼"
        clearable
        style="width: 170px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.supplier_id" link @click="((query.supplier_id = null), search())">
        清除供應商
      </el-button>
      <span class="spacer" />
      <template v-if="canWrite">
        <el-button type="primary" @click="create('receipt')">新增進貨單</el-button>
        <el-button type="primary" @click="create('return')">新增退出單</el-button>
      </template>
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column label="類型" width="70">
        <template #default="{ row }">{{ typeLabels[row.doc_type as ReceiptDocType] }}</template>
      </el-table-column>
      <el-table-column label="供應商" min-width="180">
        <template #default="{ row }">{{ row.supplier_code }} {{ row.supplier_name }}</template>
      </el-table-column>
      <el-table-column prop="warehouse_name" label="倉庫" width="120" />
      <el-table-column label="合計" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.total_amount) }}</template>
      </el-table-column>
      <el-table-column prop="invoice_no" label="發票號碼" width="120" />
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><DocStatusTag :status="row.status" /></template>
      </el-table-column>
      <el-table-column prop="created_by_name" label="建立者" width="110" />
      <el-table-column prop="note" label="備註" min-width="140" show-overflow-tooltip />
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
