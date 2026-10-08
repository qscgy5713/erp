<script setup lang="ts">
// 首頁儀表板:銷售、待審、庫存警示、應收應付;各卡片依權限出現,銷售與應收依資料範圍
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { dashboardApi, type Dashboard } from '@/api/dashboard'
import { getHealth } from '@/api/system'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const status = ref<'loading' | 'ok' | 'error'>('loading')
const message = ref('')
const data = ref<Dashboard | null>(null)
const failed = ref('')

const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })
const qty = (v: string) => Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 4 })

// 近 7 天的長條:以最大值為 100%;沒有資料時全部為 0
const bars = computed(() => {
  const days = data.value?.sales?.daily ?? []
  const max = Math.max(...days.map((d) => Math.abs(Number(d.amount))), 1)
  return days.map((d) => ({
    label: d.date.slice(5),
    amount: Number(d.amount),
    pct: (Math.abs(Number(d.amount)) / max) * 100,
  }))
})

const closingHint = computed(() => {
  const c = data.value?.costing
  if (!c) return ''
  const lastMonth = new Date(`${c.current_month}-01T00:00:00`)
  lastMonth.setMonth(lastMonth.getMonth() - 1)
  const target = `${lastMonth.getFullYear()}-${String(lastMonth.getMonth() + 1).padStart(2, '0')}`
  return c.last_closing >= target ? '' : `上個月(${target})尚未月結成本`
})

const hasAnyCard = computed(
  () =>
    !!data.value &&
    (!!data.value.sales ||
      data.value.pending.length > 0 ||
      !!data.value.low_stock ||
      !!data.value.expiry ||
      !!data.value.receivable ||
      !!data.value.payable),
)

onMounted(async () => {
  getHealth()
    .then(() => (status.value = 'ok'))
    .catch((e) => {
      status.value = 'error'
      message.value = e instanceof Error ? e.message : String(e)
    })
  try {
    data.value = await dashboardApi.get()
  } catch (e) {
    failed.value = e instanceof Error ? e.message : String(e)
  }
})

const go = (path: string) => router.push(path)
</script>

<template>
  <div>
    <div class="head">
      <h2>{{ auth.user?.name ?? '' }},您好</h2>
      <span class="spacer" />
      <el-tag v-if="status === 'loading'" type="info">檢查中…</el-tag>
      <el-tag v-else-if="status === 'ok'" type="success">API 與資料庫正常</el-tag>
      <el-tag v-else type="danger">異常:{{ message }}</el-tag>
    </div>

    <el-alert
      v-if="failed"
      type="error"
      :closable="false"
      show-icon
      :title="`儀表板載入失敗:${failed}`"
    />
    <el-alert
      v-if="closingHint"
      type="warning"
      :closable="false"
      show-icon
      class="mb"
      :title="closingHint"
    >
      <el-button
        v-if="auth.can(['costing.close'])"
        link
        type="primary"
        @click="go('/costing/closings')"
      >
        前往月結成本
      </el-button>
    </el-alert>

    <el-row v-if="data" :gutter="16">
      <el-col v-if="data.sales" :xs="24" :md="12">
        <el-card shadow="never" class="mb">
          <template #header
            >銷售(未稅,本位幣)<span class="hint">{{ data.sales.scope_label }}</span></template
          >
          <div class="kpis">
            <div>
              <div class="num">{{ money(data.sales.today) }}</div>
              <div class="label">今日({{ data.sales.today_count }} 張出貨單)</div>
            </div>
            <div>
              <div class="num">{{ money(data.sales.month) }}</div>
              <div class="label">本月累計({{ data.sales.month_count }} 張)</div>
            </div>
          </div>
          <div class="bars" role="img" aria-label="近 7 天銷售額">
            <div v-for="b in bars" :key="b.label" class="bar">
              <div class="col" :title="`${b.label}:${money(b.amount)}`">
                <div class="fill" :style="{ height: b.pct + '%' }" :class="{ neg: b.amount < 0 }" />
              </div>
              <div class="d">{{ b.label }}</div>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col :xs="24" :md="12">
        <el-card shadow="never" class="mb">
          <template #header>待處理單據</template>
          <el-empty
            v-if="data.pending.length === 0"
            :image-size="48"
            description="沒有待審的單據"
          />
          <div v-for="p in data.pending" :key="p.key" class="row">
            <span>{{ p.label }}</span>
            <el-button link type="primary" @click="go(p.path)">{{ p.count }} 筆待審</el-button>
          </div>
        </el-card>
      </el-col>

      <el-col v-if="data.receivable || data.payable" :xs="24" :md="12">
        <el-card shadow="never" class="mb">
          <template #header>應收 / 應付(本位幣,未沖餘額)</template>
          <div v-if="data.receivable" class="ar">
            <div class="title">
              應收帳款
              <el-button link type="primary" @click="go('/finance/receivables')">明細</el-button>
            </div>
            <div class="kpis">
              <div>
                <div class="num">{{ money(data.receivable.open_amount) }}</div>
                <div class="label">未沖合計</div>
              </div>
              <div>
                <div class="num" :class="{ bad: Number(data.receivable.overdue_amount) > 0 }">
                  {{ money(data.receivable.overdue_amount) }}
                </div>
                <div class="label">已逾期({{ data.receivable.overdue_count }} 筆)</div>
              </div>
            </div>
          </div>
          <div v-if="data.payable" class="ar">
            <div class="title">
              應付帳款
              <el-button link type="primary" @click="go('/finance/payables')">明細</el-button>
            </div>
            <div class="kpis">
              <div>
                <div class="num">{{ money(data.payable.open_amount) }}</div>
                <div class="label">未沖合計</div>
              </div>
              <div>
                <div class="num" :class="{ bad: Number(data.payable.overdue_amount) > 0 }">
                  {{ money(data.payable.overdue_amount) }}
                </div>
                <div class="label">已逾期({{ data.payable.overdue_count }} 筆)</div>
              </div>
              <div>
                <div class="num">{{ money(data.payable.due_soon_amount) }}</div>
                <div class="label">7 日內到期</div>
              </div>
            </div>
          </div>
        </el-card>
      </el-col>

      <el-col v-if="data.low_stock" :xs="24" :md="12">
        <el-card shadow="never" class="mb">
          <template #header>
            庫存警示<span class="hint">低於安全庫存 {{ data.low_stock.count }} 項</span>
          </template>
          <el-empty
            v-if="data.low_stock.count === 0"
            :image-size="48"
            description="沒有低於安全庫存的料品"
          />
          <div v-for="i in data.low_stock.items" :key="i.id" class="row">
            <span>{{ i.code }} {{ i.name }}</span>
            <span class="bad"
              >{{ qty(i.total) }} / {{ qty(i.safety_stock) }} {{ i.unit_name }}</span
            >
          </div>
          <el-button
            v-if="data.low_stock.count > 0"
            link
            type="primary"
            @click="go('/inventory/balances?below_safety=true')"
          >
            查看全部
          </el-button>
        </el-card>
      </el-col>

      <el-col v-if="data.expiry" :xs="24" :md="12">
        <el-card shadow="never" class="mb">
          <template #header>
            效期警示<span class="hint"
              >已過期 {{ data.expiry.expired }} 批、{{ data.expiry.days }} 天內到期
              {{ data.expiry.expiring }} 批</span
            >
          </template>
          <el-empty
            v-if="!data.expiry.items.length"
            :image-size="48"
            description="沒有即將到期或已過期的批號"
          />
          <div v-for="i in data.expiry.items" :key="i.lot_id" class="row">
            <span>{{ i.item_code }} {{ i.item_name }} · {{ i.lot_no }}</span>
            <span :class="{ bad: i.expired }">
              {{ i.expiry_date }} {{ i.expired ? '已過期' : '' }} · {{ qty(i.qty) }}
            </span>
          </div>
          <el-button
            v-if="data.expiry.items.length"
            link
            type="primary"
            @click="go(`/inventory/lots?expiry=${data.expiry.expired ? 'expired' : 'expiring'}`)"
          >
            查看全部
          </el-button>
        </el-card>
      </el-col>
    </el-row>

    <el-empty
      v-if="data && !hasAnyCard"
      description="目前沒有可顯示的資料;請使用左側選單開始作業"
    />
  </div>
</template>

<style scoped>
.head {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}
.head h2 {
  margin: 0;
  font-size: 18px;
}
.spacer {
  flex: 1;
}
.mb {
  margin-bottom: 16px;
}
.hint {
  margin-left: 10px;
  color: var(--el-text-color-secondary);
  font-size: 12px;
  font-weight: normal;
}
.kpis {
  display: flex;
  gap: 32px;
  flex-wrap: wrap;
}
.num {
  font-size: 26px;
  font-weight: 600;
  line-height: 1.2;
  font-variant-numeric: tabular-nums;
}
.label {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  margin-top: 2px;
}
.bad {
  color: var(--el-color-danger);
}
.row {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}
.ar + .ar {
  margin-top: 16px;
  padding-top: 12px;
  border-top: 1px solid var(--el-border-color-lighter);
}
.ar .title {
  font-weight: 600;
  margin-bottom: 6px;
}
.bars {
  display: flex;
  gap: 8px;
  margin-top: 16px;
}
.bar {
  flex: 1;
  text-align: center;
}
.col {
  height: 72px;
  display: flex;
  align-items: flex-end;
  background: var(--el-fill-color-lighter);
  border-radius: 3px;
  overflow: hidden;
}
.fill {
  width: 100%;
  min-height: 2px;
  background: var(--el-color-primary);
}
.fill.neg {
  background: var(--el-color-danger);
}
.d {
  margin-top: 4px;
  font-size: 11px;
  color: var(--el-text-color-secondary);
}
</style>
