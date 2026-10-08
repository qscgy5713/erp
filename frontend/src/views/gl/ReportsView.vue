<script setup lang="ts">
// 會計報表:試算表、總分類帳、日記帳(只計已過帳傳票,金額為本位幣)
import { onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  acctTypeLabels,
  glApi,
  sourceLabels,
  type AccountOption,
  type AcctType,
  type GeneralLedger,
  type JournalRow,
  type TrialBalance,
} from '@/api/gl'
import type { PageMeta } from '@/api/http'
import { useApiError } from '@/composables/useApiError'

const router = useRouter()
const { handle } = useApiError()

const pad = (n: number) => String(n).padStart(2, '0')
const now = new Date()
const monthStart = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-01`
const todayStr = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`

const tab = ref('trial')
const range = ref<[string, string]>([monthStart, todayStr])
const loading = ref(false)
const accounts = ref<AccountOption[]>([])

const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })
const dc = (v: string | number, side: 'd' | 'c') => {
  const n = Number(v)
  if (n === 0) return ''
  return side === 'd' ? (n > 0 ? money(n) : '') : n < 0 ? money(-n) : ''
}

// ---- 試算表 ----
const trial = ref<TrialBalance | null>(null)
async function loadTrial() {
  loading.value = true
  try {
    trial.value = await glApi.trialBalance(range.value[0], range.value[1])
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

// ---- 總分類帳 ----
const ledgerAccount = ref<number | null>(null)
const ledger = ref<GeneralLedger | null>(null)
async function loadLedger() {
  if (!ledgerAccount.value) return
  loading.value = true
  try {
    ledger.value = await glApi.ledger(ledgerAccount.value, range.value[0], range.value[1])
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}
function openAccount(accountId: number) {
  ledgerAccount.value = accountId
  tab.value = 'ledger'
  loadLedger()
}

// ---- 日記帳 ----
const journal = ref<JournalRow[]>([])
const jMeta = ref<PageMeta>({ page: 1, size: 50, total: 0 })
const jPage = reactive({ page: 1, size: 50 })
async function loadJournal() {
  loading.value = true
  try {
    const res = await glApi.journal({ from: range.value[0], to: range.value[1], ...jPage })
    journal.value = res.items
    jMeta.value = res.meta
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function run() {
  if (tab.value === 'trial') loadTrial()
  else if (tab.value === 'ledger') loadLedger()
  else {
    jPage.page = 1
    loadJournal()
  }
}

const openVoucher = (id: number) => router.push({ name: 'gl-voucher', params: { id } })

onMounted(async () => {
  loadTrial()
  try {
    accounts.value = await glApi.accountOptions()
  } catch (e) {
    handle(e)
  }
})
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-date-picker
        v-model="range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :clearable="false"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 260px"
      />
      <el-select
        v-if="tab === 'ledger'"
        v-model="ledgerAccount"
        filterable
        placeholder="選擇科目"
        style="width: 220px"
      >
        <el-option v-for="a in accounts" :key="a.id" :label="`${a.code} ${a.name}`" :value="a.id" />
      </el-select>
      <el-button type="primary" @click="run">查詢</el-button>
    </div>

    <el-tabs v-model="tab" @tab-change="run">
      <el-tab-pane label="試算表" name="trial">
        <el-table v-loading="loading" :data="trial?.rows ?? []" border size="small">
          <el-table-column label="科目" min-width="220">
            <template #default="{ row }">
              <el-button link type="primary" @click="openAccount(row.account_id)">
                {{ row.code }}
              </el-button>
              {{ row.name }}
            </template>
          </el-table-column>
          <el-table-column label="類別" width="80">
            <template #default="{ row }">{{ acctTypeLabels[row.acct_type as AcctType] }}</template>
          </el-table-column>
          <el-table-column label="期初借方" width="120" align="right">
            <template #default="{ row }">{{ dc(row.opening, 'd') }}</template>
          </el-table-column>
          <el-table-column label="期初貸方" width="120" align="right">
            <template #default="{ row }">{{ dc(row.opening, 'c') }}</template>
          </el-table-column>
          <el-table-column label="本期借方" width="120" align="right">
            <template #default="{ row }">{{
              Number(row.period_debit) ? money(row.period_debit) : ''
            }}</template>
          </el-table-column>
          <el-table-column label="本期貸方" width="120" align="right">
            <template #default="{ row }">{{
              Number(row.period_credit) ? money(row.period_credit) : ''
            }}</template>
          </el-table-column>
          <el-table-column label="期末借方" width="120" align="right">
            <template #default="{ row }">{{ dc(row.closing, 'd') }}</template>
          </el-table-column>
          <el-table-column label="期末貸方" width="120" align="right">
            <template #default="{ row }">{{ dc(row.closing, 'c') }}</template>
          </el-table-column>
        </el-table>
        <div v-if="trial" class="sum">
          <span
            >本期借方合計 <strong>{{ money(trial.total_debit) }}</strong></span
          >
          <span
            >本期貸方合計 <strong>{{ money(trial.total_credit) }}</strong></span
          >
          <el-tag :type="trial.balanced ? 'success' : 'danger'">
            {{ trial.balanced ? '借貸平衡' : '借貸不平衡' }}
          </el-tag>
        </div>
      </el-tab-pane>

      <el-tab-pane label="總分類帳" name="ledger">
        <template v-if="ledger">
          <h3 class="head">
            {{ ledger.account_code }} {{ ledger.account_name }}
            <span class="hint">期初餘額 {{ money(ledger.opening) }}(借方為正)</span>
          </h3>
          <el-table v-loading="loading" :data="ledger.rows" border size="small">
            <el-table-column prop="date" label="日期" width="110" />
            <el-table-column label="傳票號" width="160">
              <template #default="{ row }">
                <el-button link type="primary" @click="openVoucher(row.voucher_id)">{{
                  row.doc_no
                }}</el-button>
              </template>
            </el-table-column>
            <el-table-column label="來源" width="170">
              <template #default="{ row }">
                {{ sourceLabels[row.source_type] ?? row.source_type }} {{ row.source_no }}
              </template>
            </el-table-column>
            <el-table-column
              prop="description"
              label="摘要"
              min-width="180"
              show-overflow-tooltip
            />
            <el-table-column label="借方" width="110" align="right">
              <template #default="{ row }">{{
                Number(row.debit) ? money(row.debit) : ''
              }}</template>
            </el-table-column>
            <el-table-column label="貸方" width="110" align="right">
              <template #default="{ row }">{{
                Number(row.credit) ? money(row.credit) : ''
              }}</template>
            </el-table-column>
            <el-table-column label="餘額" width="120" align="right">
              <template #default="{ row }">{{ money(row.balance) }}</template>
            </el-table-column>
          </el-table>
          <div class="sum">
            <span
              >借方合計 <strong>{{ money(ledger.total_debit) }}</strong></span
            >
            <span
              >貸方合計 <strong>{{ money(ledger.total_credit) }}</strong></span
            >
            <span
              >期末餘額 <strong>{{ money(ledger.closing) }}</strong></span
            >
          </div>
        </template>
        <el-empty v-else description="選擇科目與期間後查詢" />
      </el-tab-pane>

      <el-tab-pane label="日記帳" name="journal">
        <el-table v-loading="loading" :data="journal" border size="small">
          <el-table-column prop="date" label="日期" width="110" />
          <el-table-column label="傳票號" width="160">
            <template #default="{ row }">
              <el-button link type="primary" @click="openVoucher(row.voucher_id)">{{
                row.doc_no
              }}</el-button>
            </template>
          </el-table-column>
          <el-table-column label="科目" min-width="200">
            <template #default="{ row }">{{ row.account_code }} {{ row.account_name }}</template>
          </el-table-column>
          <el-table-column label="摘要" min-width="200" show-overflow-tooltip>
            <template #default="{ row }">{{ row.description || row.voucher_description }}</template>
          </el-table-column>
          <el-table-column label="借方" width="110" align="right">
            <template #default="{ row }">{{ Number(row.debit) ? money(row.debit) : '' }}</template>
          </el-table-column>
          <el-table-column label="貸方" width="110" align="right">
            <template #default="{ row }">{{
              Number(row.credit) ? money(row.credit) : ''
            }}</template>
          </el-table-column>
        </el-table>
        <div class="pager">
          <el-pagination
            v-model:current-page="jPage.page"
            v-model:page-size="jPage.size"
            :total="jMeta.total"
            :page-sizes="[50, 100, 200]"
            layout="total, sizes, prev, pager, next"
            @current-change="loadJournal"
            @size-change="run"
          />
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<style scoped>
.head {
  margin: 8px 0;
  font-size: 15px;
}
.hint {
  margin-left: 12px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: normal;
}
.sum {
  display: flex;
  gap: 24px;
  flex-wrap: wrap;
  justify-content: flex-end;
  align-items: center;
  margin-top: 12px;
}
</style>
