<script setup lang="ts">
// 財務報表:損益表(期間)、資產負債表(截止日),可與去年同期比較並匯出 Excel;只計已過帳傳票,金額為本位幣
import { onMounted, ref } from 'vue'
import { glApi, type DeptPnl, type Statement, type StmtLine } from '@/api/gl'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()

const today = new Date()
const pad = (n: number) => String(n).padStart(2, '0')
const iso = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`

const tab = ref<'income' | 'balance' | 'cashflow' | 'dept'>('income')
const isRange = () => tab.value !== 'balance'
const range = ref<[string, string]>([`${today.getFullYear()}-01-01`, iso(today)])
const asOf = ref(iso(today))
const compare = ref(false)
const loading = ref(false)
const stmt = ref<Statement | null>(null)
const dept = ref<DeptPnl | null>(null)

const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })

const rowClass = ({ row }: { row: StmtLine }) => `k-${row.kind}`

let seq = 0
async function run() {
  const mine = ++seq // 連續查詢時,只接受最後一次的回應
  loading.value = true
  try {
    if (tab.value === 'dept') {
      const res = await glApi.departmentPnl(range.value[0], range.value[1])
      if (mine === seq) dept.value = res
      return
    }
    const res =
      tab.value === 'income'
        ? await glApi.incomeStatement(range.value[0], range.value[1], compare.value)
        : tab.value === 'cashflow'
          ? await glApi.cashFlow(range.value[0], range.value[1], compare.value)
          : await glApi.balanceSheet(asOf.value, compare.value)
    if (mine === seq) stmt.value = res
  } catch (e) {
    if (mine !== seq) return
    stmt.value = null
    dept.value = null
    handle(e)
  } finally {
    if (mine === seq) loading.value = false
  }
}

async function exportXlsx() {
  try {
    if (tab.value === 'income' || tab.value === 'cashflow') {
      const income = tab.value === 'income'
      await glApi.exportStatement(
        income ? 'income-statement' : 'cash-flow',
        { from: range.value[0], to: range.value[1], compare: compare.value },
        income ? '損益表.xlsx' : '現金流量表.xlsx',
      )
    } else if (tab.value === 'balance') {
      await glApi.exportStatement(
        'balance-sheet',
        { as_of: asOf.value, compare: compare.value },
        '資產負債表.xlsx',
      )
    }
  } catch (e) {
    handle(e)
  }
}

const deptCols = [
  { key: 'revenue', label: '收入' },
  { key: 'cost', label: '成本' },
  { key: 'expense', label: '費用' },
  { key: 'profit', label: '損益' },
] as const

onMounted(run)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-date-picker
        v-if="isRange()"
        v-model="range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :clearable="false"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 260px"
      />
      <el-date-picker
        v-else
        v-model="asOf"
        type="date"
        value-format="YYYY-MM-DD"
        :clearable="false"
        placeholder="截止日期"
        style="width: 160px"
      />
      <el-checkbox v-if="tab !== 'dept'" v-model="compare">{{
        tab === 'balance' ? '與去年同日比較' : '與去年同期比較'
      }}</el-checkbox>
      <el-button type="primary" @click="run">查詢</el-button>
      <el-button v-if="tab !== 'dept'" :disabled="!stmt" @click="exportXlsx">匯出 Excel</el-button>
    </div>

    <el-tabs v-model="tab" @tab-change="run">
      <el-tab-pane label="損益表" name="income" />
      <el-tab-pane label="資產負債表" name="balance" />
      <el-tab-pane label="現金流量表" name="cashflow" />
      <el-tab-pane label="部門別損益" name="dept" />
    </el-tabs>

    <el-alert
      v-if="tab !== 'dept' && stmt?.note"
      type="warning"
      :closable="false"
      show-icon
      class="mb"
      :title="stmt.note"
    />
    <el-alert
      v-if="tab === 'balance' && stmt && stmt.balanced === false"
      type="error"
      :closable="false"
      show-icon
      class="mb"
      title="資產負債表不平衡(資產 ≠ 負債 + 權益),請到「會計 → 對帳檢查」確認傳票借貸是否平衡。"
    />

    <div v-if="tab === 'dept'" v-loading="loading" class="dept">
      <p class="hint">
        依傳票明細的輔助核算「部門」分組;沒有填部門的(包含系統自動拋轉的傳票)歸在「未指定部門」。
      </p>
      <el-table :data="dept ? [...dept.rows, dept.total] : []" border size="small">
        <el-table-column label="部門" min-width="180">
          <template #default="{ row }">{{ row.code }} {{ row.name }}</template>
        </el-table-column>
        <el-table-column
          v-for="c in deptCols"
          :key="c.key"
          :label="c.label"
          width="130"
          align="right"
        >
          <template #default="{ row }">{{ money(row[c.key]) }}</template>
        </el-table-column>
      </el-table>
    </div>

    <div v-else v-loading="loading" class="stmt">
      <div v-if="stmt" class="stmt-head">
        <strong>{{ stmt.title }}</strong>
        <span> {{ stmt.from ? `${stmt.from} ~ ${stmt.to}` : `截至 ${stmt.to}` }}(新台幣) </span>
      </div>
      <el-table
        v-if="stmt"
        :data="stmt.lines"
        border
        size="small"
        :show-header="true"
        :row-class-name="rowClass"
      >
        <el-table-column label="項目" min-width="280">
          <template #default="{ row }">
            <span :style="{ paddingLeft: row.level * 20 + 'px' }">
              <span v-if="row.code" class="code">{{ row.code }}</span>
              {{ row.label }}
            </span>
          </template>
        </el-table-column>
        <el-table-column label="金額" width="150" align="right">
          <template #default="{ row }">{{
            row.kind === 'header' ? '' : money(row.amount)
          }}</template>
        </el-table-column>
        <el-table-column
          v-if="stmt.compare"
          :label="stmt.from ? '去年同期' : '去年同日'"
          width="150"
          align="right"
        >
          <template #default="{ row }">{{ row.kind === 'header' ? '' : money(row.prev) }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="stmt && !stmt.lines.length" description="這個期間沒有資料" />
    </div>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
.stmt {
  max-width: 760px;
}
.dept {
  max-width: 860px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.stmt-head {
  display: flex;
  justify-content: space-between;
  margin-bottom: 8px;
}
.code {
  color: var(--el-text-color-secondary);
  margin-right: 8px;
  font-variant-numeric: tabular-nums;
}
:deep(.k-header td) {
  background: var(--el-fill-color-light);
  font-weight: 600;
}
:deep(.k-total td) {
  font-weight: 600;
  border-top: 1px solid var(--el-border-color-darker);
}
</style>
