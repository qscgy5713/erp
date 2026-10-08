<script setup lang="ts">
// 報價單與訂單列表(依資料範圍只列出自己範圍內的單據)
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import type { DocStatus } from '@/api/inventory'
import { salesApi, type SalesOrderRow, type SalesOrderType, type ShipState } from '@/api/sales'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'
import PartnerPicker from '@/components/PartnerPicker.vue'

const router = useRouter()
const auth = useAuthStore()
const canWrite = computed(() => auth.can(['sales.order.write']))
const { handle } = useApiError()

const typeLabels: Record<SalesOrderType, string> = { quotation: '報價', order: '訂單' }
const shipStates: Record<ShipState, { label: string; type: 'info' | 'warning' | 'success' }> = {
  none: { label: '未出貨', type: 'info' },
  partial: { label: '部分出貨', type: 'warning' },
  full: { label: '已出齊', type: 'success' },
}
// 報價單與訂單不過帳
const orderStatuses = Object.fromEntries(
  Object.entries(statusLabels).filter(([k]) => k !== 'posted'),
) as Partial<Record<DocStatus, string>>

const query = reactive({
  doc_type: '' as SalesOrderType | '',
  status: '' as DocStatus | '',
  customer_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<SalesOrderRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await salesApi.orders({
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

function open(row: SalesOrderRow) {
  router.push({ name: 'sales-order', params: { id: row.id } })
}

function create(type: SalesOrderType) {
  router.push({ name: 'sales-order-new', query: { type } })
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
        <el-option v-for="(label, v) in orderStatuses" :key="v" :label="label" :value="v" />
      </el-select>
      <div style="width: 200px">
        <PartnerPicker v-model="query.customer_id" kind="customer" @update:model-value="search" />
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
        placeholder="單號 / 客戶單號"
        clearable
        style="width: 160px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.customer_id" link @click="((query.customer_id = null), search())">
        清除客戶
      </el-button>
      <span class="spacer" />
      <template v-if="canWrite">
        <el-button type="primary" @click="create('quotation')">新增報價單</el-button>
        <el-button type="primary" @click="create('order')">新增訂單</el-button>
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
        <template #default="{ row }">{{ typeLabels[row.doc_type as SalesOrderType] }}</template>
      </el-table-column>
      <el-table-column label="客戶" min-width="180">
        <template #default="{ row }">{{ row.customer_code }} {{ row.customer_name }}</template>
      </el-table-column>
      <el-table-column prop="sales_user_name" label="業務" width="100" />
      <el-table-column label="合計" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.total_amount) }}</template>
      </el-table-column>
      <el-table-column label="出貨日 / 有效期限" width="130">
        <template #default="{ row }">{{ row.delivery_date ?? row.valid_until }}</template>
      </el-table-column>
      <el-table-column label="狀態" width="100">
        <template #default="{ row }"><DocStatusTag :status="row.status" /></template>
      </el-table-column>
      <el-table-column label="進度" width="100">
        <template #default="{ row }">
          <template v-if="row.doc_type === 'order'">
            <el-tag
              v-if="row.status === 'approved' || row.status === 'closed'"
              :type="shipStates[row.ship_state as ShipState].type"
              effect="plain"
            >
              {{ shipStates[row.ship_state as ShipState].label }}
            </el-tag>
          </template>
          <el-tag v-else-if="row.converted" type="success" effect="plain">已轉訂單</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="customer_po_no" label="客戶單號" width="120" />
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
