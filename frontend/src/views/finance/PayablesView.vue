<script setup lang="ts">
// 應付帳款:由進貨 / 退出過帳產生(退出為負數);付款沖帳於 M5 加入
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { financeApi, type Payable } from '@/api/finance'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { RECEIPT_ANY } from '@/navigation'
import { useApiError } from '@/composables/useApiError'
import SupplierPicker from '@/components/SupplierPicker.vue'

const router = useRouter()
const auth = useAuthStore()
const { handle } = useApiError()

const sourceLabels: Record<Payable['source_type'], string> = {
  goods_receipt: '進貨',
  purchase_return: '退出',
}

const query = reactive({
  supplier_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  open_only: true,
  page: 1,
  size: 50,
})
const rows = ref<Payable[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const baseSum = ref('0')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await financeApi.payables({
      ...rest,
      keyword: rest.keyword.trim(),
      from: range?.[0],
      to: range?.[1],
    })
    rows.value = res.items
    meta.value = res.meta
    baseSum.value = res.meta.base_amount_sum ?? '0'
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

function openSource(row: Payable) {
  // 沒有進貨單檢視權限時只顯示單號
  if (!auth.can(RECEIPT_ANY)) return
  router.push({ name: 'purchase-receipt', params: { id: row.source_id } })
}

const money = (v: string, places = 0) =>
  Number(v).toLocaleString('zh-TW', { minimumFractionDigits: places, maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
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
        placeholder="來源單號"
        clearable
        style="width: 160px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-checkbox v-model="query.open_only" @change="search">只看未沖清</el-checkbox>
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.supplier_id" link @click="((query.supplier_id = null), search())">
        清除供應商
      </el-button>
      <span class="spacer" />
      <span
        >本位幣合計:<strong>TWD {{ money(baseSum) }}</strong></span
      >
    </div>

    <el-table v-loading="loading" :data="rows" border size="small">
      <el-table-column label="來源" width="170">
        <template #default="{ row }">
          {{ sourceLabels[row.source_type as Payable['source_type']] }}
          <el-button link type="primary" @click="openSource(row)">{{ row.source_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="單據日期" width="100" />
      <el-table-column prop="due_date" label="到期日" width="100" />
      <el-table-column label="供應商" min-width="180">
        <template #default="{ row }">{{ row.supplier_code }} {{ row.supplier_name }}</template>
      </el-table-column>
      <el-table-column label="金額(原幣)" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.amount) }}</template>
      </el-table-column>
      <el-table-column label="已沖帳" width="120" align="right">
        <template #default="{ row }">{{ money(row.paid_amount) }}</template>
      </el-table-column>
      <el-table-column label="未沖餘額" width="130" align="right">
        <template #default="{ row }">
          <strong>{{ money(row.balance) }}</strong>
        </template>
      </el-table-column>
      <el-table-column label="本位幣金額" width="130" align="right">
        <template #default="{ row }">{{ money(row.base_amount) }}</template>
      </el-table-column>
    </el-table>
    <div class="pager">
      <el-pagination
        v-model:current-page="query.page"
        v-model:page-size="query.size"
        :total="meta.total"
        :page-sizes="[50, 100]"
        layout="total, sizes, prev, pager, next"
        @current-change="load"
        @size-change="search"
      />
    </div>
  </div>
</template>
