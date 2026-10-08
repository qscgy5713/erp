<script setup lang="ts">
// 帳齡分析(route meta.ledger):依到期日分組的未沖餘額(本位幣),依客戶 / 供應商彙總
import { onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { settlementApi, type AgingRow, type LedgerKind } from '@/api/finance'
import { useApiError } from '@/composables/useApiError'

const route = useRoute()
const { handle } = useApiError()

const kind = route.meta.ledger as LedgerKind
const isAR = kind === 'receivable'

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const asOf = ref(today())
const rows = ref<AgingRow[]>([])
const loading = ref(false)

async function load() {
  loading.value = true
  try {
    rows.value = (await settlementApi.aging(kind, asOf.value)).rows
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })

const cols = [
  { key: 'not_due', label: '未到期' },
  { key: 'd1_30', label: '逾期 1–30 天' },
  { key: 'd31_60', label: '逾期 31–60 天' },
  { key: 'd61_90', label: '逾期 61–90 天' },
  { key: 'd90_plus', label: '逾期 90 天以上' },
  { key: 'total', label: '合計' },
] as const

function totals(): Record<string, string> {
  const t: Record<string, number> = {}
  for (const c of cols) t[c.key] = rows.value.reduce((s, r) => s + Number(r[c.key]), 0)
  return Object.fromEntries(Object.entries(t).map(([k, v]) => [k, money(v)]))
}

onMounted(load)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <span>截至</span>
      <el-date-picker
        v-model="asOf"
        value-format="YYYY-MM-DD"
        :clearable="false"
        style="width: 160px"
        @change="load"
      />
      <el-button @click="load">重新整理</el-button>
      <span class="hint">本位幣(TWD)、目前未沖餘額,依到期日與截止日的差距分組</span>
    </div>
    <el-table v-loading="loading" :data="rows" border size="small">
      <el-table-column :label="isAR ? '客戶' : '供應商'" min-width="200">
        <template #default="{ row }">{{ row.partner_code }} {{ row.partner_name }}</template>
      </el-table-column>
      <el-table-column v-for="c in cols" :key="c.key" :label="c.label" width="130" align="right">
        <template #default="{ row }">
          <strong v-if="c.key === 'total'">{{ money(row[c.key]) }}</strong>
          <span v-else :class="{ over: c.key === 'd90_plus' && Number(row[c.key]) > 0 }">
            {{ money(row[c.key]) }}
          </span>
        </template>
      </el-table-column>
    </el-table>
    <div v-if="rows.length" class="total">
      <span v-for="c in cols" :key="c.key"
        >{{ c.label }} <strong>{{ totals()[c.key] }}</strong></span
      >
    </div>
  </div>
</template>

<style scoped>
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.over {
  color: var(--el-color-danger);
  font-weight: 600;
}
.total {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  justify-content: flex-end;
  margin-top: 12px;
}
</style>
