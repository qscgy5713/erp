<script setup lang="ts">
// 應收 / 應付帳款(route meta.ledger):由出貨 / 退回、進貨 / 退出過帳產生(退回 / 退出為負數);
// 收付款沖帳於 M5 加入。應收依客戶負責業務套用資料範圍。
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { financeApi, type LedgerEntry, type LedgerKind } from '@/api/finance'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { DELIVERY_ANY, RECEIPT_ANY } from '@/navigation'
import { useApiError } from '@/composables/useApiError'
import PartnerPicker from '@/components/PartnerPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { handle } = useApiError()

const kind = route.meta.ledger as LedgerKind
const isAR = kind === 'receivable'
const partnerKey = isAR ? 'customer' : 'supplier'

const sourceLabels: Record<LedgerEntry['source_type'], string> = {
  goods_receipt: '進貨',
  purchase_return: '退出',
  delivery: '出貨',
  sales_return: '退回',
}

const query = reactive({
  partner_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  open_only: true,
  page: 1,
  size: 50,
})
const rows = ref<LedgerEntry[]>([])
const meta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const baseSum = ref('0')
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, partner_id, ...rest } = query
    const res = await financeApi.ledger(kind, {
      ...rest,
      [`${partnerKey}_id`]: partner_id,
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

// 沒有來源單據檢視權限時只顯示單號
const canOpenSource = computed(() => auth.can(isAR ? DELIVERY_ANY : RECEIPT_ANY))

function openSource(row: LedgerEntry) {
  if (!canOpenSource.value) return
  router.push({ name: isAR ? 'sales-delivery' : 'purchase-receipt', params: { id: row.source_id } })
}

const partnerOf = (row: LedgerEntry) =>
  isAR ? `${row.customer_code} ${row.customer_name}` : `${row.supplier_code} ${row.supplier_name}`

const money = (v: string, places = 0) =>
  Number(v).toLocaleString('zh-TW', { minimumFractionDigits: places, maximumFractionDigits: 4 })

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <div style="width: 200px">
        <PartnerPicker v-model="query.partner_id" :kind="partnerKey" @update:model-value="search" />
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
      <el-button v-if="query.partner_id" link @click="((query.partner_id = null), search())">
        清除{{ isAR ? '客戶' : '供應商' }}
      </el-button>
      <span class="spacer" />
      <span
        >本位幣合計:<strong>TWD {{ money(baseSum) }}</strong></span
      >
    </div>

    <el-table v-loading="loading" :data="rows" border size="small">
      <el-table-column label="來源" width="170">
        <template #default="{ row }">
          {{ sourceLabels[row.source_type as LedgerEntry['source_type']] }}
          <el-button v-if="canOpenSource" link type="primary" @click="openSource(row)">
            {{ row.source_no }}
          </el-button>
          <span v-else>{{ row.source_no }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="單據日期" width="100" />
      <el-table-column prop="due_date" label="到期日" width="100" />
      <el-table-column :label="isAR ? '客戶' : '供應商'" min-width="180">
        <template #default="{ row }">{{ partnerOf(row) }}</template>
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
