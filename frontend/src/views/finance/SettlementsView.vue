<script setup lang="ts">
// 收款單 / 付款單列表(route meta.settle)
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { DocStatus } from '@/api/inventory'
import { methodLabels, settlementApi, type SettleSide, type SettlementRow } from '@/api/finance'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { statusLabels } from '@/utils/docstate'
import DocStatusTag from '@/components/DocStatusTag.vue'
import PartnerPicker from '@/components/PartnerPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { handle } = useApiError()

const side = route.meta.settle as SettleSide
const isCollection = side === 'collection'
const canWrite = computed(() => auth.can([`finance.${side}.write`]))
// 收付款單不使用結案
const statuses = Object.fromEntries(
  Object.entries(statusLabels).filter(([k]) => k !== 'closed'),
) as Partial<Record<DocStatus, string>>

const query = reactive({
  status: '' as DocStatus | '',
  partner_id: null as number | null,
  keyword: '',
  range: null as [string, string] | null,
  page: 1,
  size: 20,
})
const rows = ref<SettlementRow[]>([])
const meta = ref<PageMeta>({ page: 1, size: 20, total: 0 })
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    const { range, ...rest } = query
    const res = await settlementApi.list(side, {
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

const open = (row: SettlementRow) => router.push({ name: side, params: { id: row.id } })
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
        <el-option v-for="(label, v) in statuses" :key="v" :label="label" :value="v" />
      </el-select>
      <div style="width: 200px">
        <PartnerPicker
          v-model="query.partner_id"
          :kind="isCollection ? 'customer' : 'supplier'"
          @update:model-value="search"
        />
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
        placeholder="單號 / 備查"
        clearable
        style="width: 160px"
        @keyup.enter="search"
        @clear="search"
      />
      <el-button @click="search">查詢</el-button>
      <el-button v-if="query.partner_id" link @click="((query.partner_id = null), search())">
        清除{{ isCollection ? '客戶' : '供應商' }}
      </el-button>
      <span class="spacer" />
      <el-button v-if="canWrite" type="primary" @click="router.push({ name: `${side}-new` })">
        新增{{ isCollection ? '收款單' : '付款單' }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" border highlight-current-row @row-dblclick="open">
      <el-table-column label="單號" width="160">
        <template #default="{ row }">
          <el-button link type="primary" @click="open(row)">{{ row.doc_no }}</el-button>
        </template>
      </el-table-column>
      <el-table-column prop="doc_date" label="日期" width="110" />
      <el-table-column :label="isCollection ? '客戶' : '供應商'" min-width="180">
        <template #default="{ row }">{{ row.partner_code }} {{ row.partner_name }}</template>
      </el-table-column>
      <el-table-column v-if="isCollection" prop="sales_user_name" label="業務" width="100" />
      <el-table-column label="金額" width="150" align="right">
        <template #default="{ row }">{{ row.currency }} {{ money(row.amount) }}</template>
      </el-table-column>
      <el-table-column label="方式" width="80">
        <template #default="{ row }">
          {{ methodLabels[row.method as keyof typeof methodLabels] }}
        </template>
      </el-table-column>
      <el-table-column prop="reference" label="備查" width="130" />
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
