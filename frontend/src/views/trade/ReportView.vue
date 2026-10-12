<script setup lang="ts">
// 銷售 / 採購報表(route meta.side):依客戶(供應商)排行、依料品排行、明細;銷售端另含毛利
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  reportApi,
  type ReportDetailRow,
  type ReportResult,
  type ReportGroup,
  type ReportSide,
} from '@/api/reports'
import { useApiError } from '@/composables/useApiError'

const route = useRoute()
const { handle } = useApiError()

const side = route.meta.side as ReportSide
const isSales = side === 'sales'
const partyLabel = isSales ? '客戶' : '供應商'

const ymd = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
const now = new Date()
const range = ref<[string, string]>([ymd(new Date(now.getFullYear(), now.getMonth(), 1)), ymd(now)])
const group = ref<ReportGroup>('party')
const loading = ref(false)
const result = ref<ReportResult | null>(null)
const detail = ref<ReportDetailRow[]>([])
const page = ref(1)
const size = ref(50)
const total = ref(0)

// 從排行點進明細時的篩選(客戶 / 供應商或料品)
const filter = ref<{ key: string; id: number; label: string } | null>(null)

const query = () => ({ from: range.value[0], to: range.value[1] })
const detailQuery = () => ({
  ...query(),
  ...(filter.value ? { [filter.value.key]: filter.value.id } : {}),
})

async function load() {
  if (!range.value) return
  loading.value = true
  try {
    if (group.value === 'detail') {
      const r = await reportApi.detail(side, {
        ...detailQuery(),
        page: page.value,
        size: size.value,
      })
      detail.value = r.items
      total.value = r.meta.total
    } else {
      result.value = await reportApi.summary(side, group.value, query())
    }
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function drill(row: { id: number; code: string; name: string }) {
  const key = group.value === 'item' ? 'item_id' : isSales ? 'customer_id' : 'supplier_id'
  filter.value = { key, id: row.id, label: `${row.code} ${row.name}` }
  group.value = 'detail'
  change()
}

function onGroup() {
  filter.value = null
  change()
}

function clearFilter() {
  filter.value = null
  change()
}

function change() {
  page.value = 1
  result.value = null
  detail.value = []
  load()
}

async function exportXlsx() {
  try {
    await reportApi.exportXlsx(
      side,
      group.value,
      group.value === 'detail' ? detailQuery() : query(),
      `${isSales ? '銷售' : '採購'}報表_${range.value[0]}_${range.value[1]}.xlsx`,
    )
  } catch (e) {
    handle(e)
  }
}

const money = (v?: string | null) =>
  v == null ? '' : Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 2 })
const pct = (v?: string | null) => (v == null ? '—' : `${Number(v).toFixed(1)}%`)
const docType = (t: string) => ({ delivery: '出貨', receipt: '進貨' })[t] ?? '退回'

const uncosted = computed(() => Number(result.value?.totals.uncosted_amount ?? 0))

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-date-picker
        v-model="range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :clearable="false"
        range-separator="~"
        style="width: 260px"
        @change="change"
      />
      <el-radio-group v-model="group" @change="onGroup">
        <el-radio-button value="party">依{{ partyLabel }}排行</el-radio-button>
        <el-radio-button value="item">依料品排行</el-radio-button>
        <el-radio-button value="detail">明細</el-radio-button>
      </el-radio-group>
      <el-button @click="load">重新整理</el-button>
      <el-button type="primary" plain @click="exportXlsx">匯出 Excel</el-button>
      <span class="hint">僅計已過帳單據;本位幣、未稅;退回以負數扣除</span>
    </div>

    <el-alert
      v-if="isSales && group !== 'detail' && uncosted > 0"
      type="info"
      :closable="false"
      show-icon
      class="gap"
      :title="`有 ${money(result?.totals.uncosted_amount)} 的銷售額所屬月份尚未月結成本,不計入毛利`"
    />

    <template v-if="group !== 'detail'">
      <el-table
        v-loading="loading"
        :data="result?.rows ?? []"
        border
        size="small"
        row-class-name="clickable"
        @row-click="drill"
      >
        <el-table-column type="index" label="#" width="50" />
        <el-table-column :label="group === 'party' ? partyLabel : '料品'" min-width="200">
          <template #default="{ row }">{{ row.code }} {{ row.name }}</template>
        </el-table-column>
        <el-table-column :label="isSales ? '出貨額' : '進貨額'" width="120" align="right">
          <template #default="{ row }">{{
            money(row.delivery_amount ?? row.receipt_amount)
          }}</template>
        </el-table-column>
        <el-table-column label="退回額" width="110" align="right">
          <template #default="{ row }">{{ money(row.return_amount) }}</template>
        </el-table-column>
        <el-table-column :label="isSales ? '淨銷售額' : '淨進貨額'" width="130" align="right">
          <template #default="{ row }"
            ><strong>{{ money(row.net_amount) }}</strong></template
          >
        </el-table-column>
        <el-table-column label="占比" width="80" align="right">
          <template #default="{ row }">{{ pct(row.share_pct) }}</template>
        </el-table-column>
        <el-table-column
          v-if="!isSales && group === 'item'"
          label="平均單價"
          width="110"
          align="right"
        >
          <template #default="{ row }"
            >{{ money(row.avg_price)
            }}<span v-if="row.unit_name"> /{{ row.unit_name }}</span></template
          >
        </el-table-column>
        <template v-if="isSales">
          <el-table-column label="成本" width="120" align="right">
            <template #default="{ row }">{{ money(row.cost_amount) }}</template>
          </el-table-column>
          <el-table-column label="毛利" width="120" align="right">
            <template #default="{ row }">{{ money(row.margin) }}</template>
          </el-table-column>
          <el-table-column label="毛利率" width="80" align="right">
            <template #default="{ row }">{{ pct(row.margin_pct) }}</template>
          </el-table-column>
        </template>
      </el-table>
      <div v-if="result?.rows.length" class="total">
        <span
          >合計 淨額 <strong>{{ money(result.totals.net_amount) }}</strong></span
        >
        <template v-if="isSales">
          <span
            >成本 <strong>{{ money(result.totals.cost_amount) }}</strong></span
          >
          <span
            >毛利 <strong>{{ money(result.totals.margin) }}</strong> ({{
              pct(result.totals.margin_pct)
            }})</span
          >
        </template>
      </div>
    </template>

    <template v-else>
      <el-tag v-if="filter" closable class="gap" @close="clearFilter">{{ filter.label }}</el-tag>
      <el-table v-loading="loading" :data="detail" border size="small">
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column label="單號" width="170">
          <template #default="{ row }">{{ row.doc_no }}({{ docType(row.doc_type) }})</template>
        </el-table-column>
        <el-table-column :label="partyLabel" min-width="160">
          <template #default="{ row }">
            {{ row.customer_code ?? row.supplier_code }}
            {{ row.customer_name ?? row.supplier_name }}
          </template>
        </el-table-column>
        <el-table-column label="料品" min-width="160">
          <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
        </el-table-column>
        <el-table-column label="數量" width="110" align="right">
          <template #default="{ row }">{{ money(row.qty) }} {{ row.unit_name }}</template>
        </el-table-column>
        <el-table-column label="金額" width="120" align="right">
          <template #default="{ row }">{{ money(row.amount) }}</template>
        </el-table-column>
        <template v-if="isSales">
          <el-table-column label="成本" width="110" align="right">
            <template #default="{ row }">{{
              row.cost == null ? '未月結' : money(row.cost)
            }}</template>
          </el-table-column>
          <el-table-column label="毛利" width="110" align="right">
            <template #default="{ row }">{{ money(row.margin) }}</template>
          </el-table-column>
        </template>
      </el-table>
      <el-pagination
        v-model:current-page="page"
        v-model:page-size="size"
        class="pager"
        layout="total, sizes, prev, pager, next"
        :total="total"
        @change="load"
      />
    </template>
  </div>
</template>

<style scoped>
:deep(.clickable) {
  cursor: pointer;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.gap {
  margin-bottom: 12px;
}
.total {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  justify-content: flex-end;
  margin-top: 12px;
}
.pager {
  margin-top: 12px;
  justify-content: flex-end;
}
</style>
