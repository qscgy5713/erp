<script setup lang="ts">
// 成本差異:當月完工的成品,月結後的實際單位成本 vs 標準成本;差異 = 實際 − 標準,正數(紅)表示超支。
// 差異可拆成 材料用量 + 材料價格 + 加工費(展開看各材料明細)
import { onMounted, ref } from 'vue'
import { costingApi, type VarianceRow } from '@/api/costing'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()
const now = new Date()
const period = ref(`${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`)
const rows = ref<VarianceRow[]>([])
const loading = ref(false)
const num = (v: string | null) =>
  v === null ? '—' : Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })
const signed = (v: string) => `${Number(v) > 0 ? '+' : ''}${num(v)}`
const tone = (v: string) => ({
  color: Number(v) > 0 ? 'var(--el-color-danger)' : Number(v) < 0 ? 'var(--el-color-success)' : '',
})
const statusText = { ok: '', uncosted: '該月尚未月結', no_standard: '未設標準成本' } as const

async function load() {
  loading.value = true
  try {
    rows.value = (await costingApi.variance(period.value)).rows
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-date-picker
        v-model="period"
        type="month"
        value-format="YYYY-MM"
        :clearable="false"
        @change="load"
      />
      <span class="hint">只列當月有完工入庫的成品;實際成本要月結後才確定。</span>
    </div>
    <el-table v-loading="loading" :data="rows" border empty-text="該月沒有完工入庫">
      <el-table-column type="expand">
        <template #default="{ row }">
          <div v-if="row.breakdown" class="detail">
            <el-table :data="row.breakdown.materials" size="small" border>
              <el-table-column label="材料" min-width="200">
                <template #default="{ row: m }"
                  >{{ m.material_code }} {{ m.material_name }}</template
                >
              </el-table-column>
              <el-table-column label="標準用量" width="110" align="right">
                <template #default="{ row: m }"
                  >{{ num(m.standard_qty) }} {{ m.unit_name }}</template
                >
              </el-table-column>
              <el-table-column label="實際用量" width="110" align="right">
                <template #default="{ row: m }">{{ num(m.actual_qty) }} {{ m.unit_name }}</template>
              </el-table-column>
              <el-table-column label="標準單價" width="110" align="right">
                <template #default="{ row: m }">{{ num(m.standard_price) }}</template>
              </el-table-column>
              <el-table-column label="實際單價" width="110" align="right">
                <template #default="{ row: m }">{{ num(m.actual_price) }}</template>
              </el-table-column>
              <el-table-column label="用量差異" width="110" align="right">
                <template #default="{ row: m }"
                  ><span :style="tone(m.qty_variance)">{{ signed(m.qty_variance) }}</span></template
                >
              </el-table-column>
              <el-table-column label="價格差異" width="110" align="right">
                <template #default="{ row: m }"
                  ><span :style="tone(m.price_variance)">{{
                    signed(m.price_variance)
                  }}</span></template
                >
              </el-table-column>
            </el-table>
            <div class="muted">
              標準加工費 {{ num(row.breakdown.standard_processing) }},實際加工費
              {{ num(row.breakdown.actual_processing) }}。標準用量依
              BOM(含損耗率)計;材料沒設標準成本時以實際單價計,價格差異為 0;替代料不在 BOM
              內,會表現為用量差異。
            </div>
          </div>
          <span v-else class="muted">月結且設定標準成本後才能拆解</span>
        </template>
      </el-table-column>
      <el-table-column prop="item_code" label="料號" width="140" />
      <el-table-column prop="item_name" label="品名" min-width="180" />
      <el-table-column label="完工量" width="100" align="right">
        <template #default="{ row }">{{ num(row.produced_qty) }} {{ row.unit_name }}</template>
      </el-table-column>
      <el-table-column label="實際單位成本" width="120" align="right">
        <template #default="{ row }">{{ num(row.actual_unit) }}</template>
      </el-table-column>
      <el-table-column label="標準單位成本" width="120" align="right">
        <template #default="{ row }">{{ num(row.standard_unit) }}</template>
      </el-table-column>
      <el-table-column label="實際金額" width="120" align="right">
        <template #default="{ row }">{{ num(row.actual_amount) }}</template>
      </el-table-column>
      <el-table-column label="標準金額" width="120" align="right">
        <template #default="{ row }">{{ num(row.standard_amount) }}</template>
      </el-table-column>
      <el-table-column label="差異" width="120" align="right">
        <template #default="{ row }">
          <span
            v-if="row.variance !== null"
            :style="{
              color:
                Number(row.variance) > 0 ? 'var(--el-color-danger)' : 'var(--el-color-success)',
            }"
            >{{ Number(row.variance) > 0 ? '+' : '' }}{{ num(row.variance) }}</span
          >
          <span v-else class="muted">—</span>
        </template>
      </el-table-column>
      <el-table-column label="用量差異" width="100" align="right">
        <template #default="{ row }"
          ><span v-if="row.breakdown" :style="tone(row.breakdown.qty_variance)">{{
            signed(row.breakdown.qty_variance)
          }}</span
          ><span v-else class="muted">—</span></template
        >
      </el-table-column>
      <el-table-column label="價格差異" width="100" align="right">
        <template #default="{ row }"
          ><span v-if="row.breakdown" :style="tone(row.breakdown.price_variance)">{{
            signed(row.breakdown.price_variance)
          }}</span
          ><span v-else class="muted">—</span></template
        >
      </el-table-column>
      <el-table-column label="加工費差異" width="110" align="right">
        <template #default="{ row }"
          ><span v-if="row.breakdown" :style="tone(row.breakdown.processing_variance)">{{
            signed(row.breakdown.processing_variance)
          }}</span
          ><span v-else class="muted">—</span></template
        >
      </el-table-column>
      <el-table-column label="差異 %" width="90" align="right">
        <template #default="{ row }">{{
          row.variance_pct === null ? '—' : `${num(row.variance_pct)}%`
        }}</template>
      </el-table-column>
      <el-table-column label="說明" width="130">
        <template #default="{ row }"
          ><span class="muted">{{
            statusText[row.status as keyof typeof statusText]
          }}</span></template
        >
      </el-table-column>
    </el-table>
  </div>
</template>

<style scoped>
.detail {
  padding: 8px 16px;
}
.muted {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
</style>
