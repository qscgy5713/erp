<script setup lang="ts">
// 對帳單(route meta.ledger):單一對象、單一幣別,期初 + 帳款 − 收付款 = 期末
import { computed, reactive, ref } from 'vue'
import { useRoute } from 'vue-router'
import { settlementApi, type LedgerKind, type Statement, type StatementRow } from '@/api/finance'
import { useApiError } from '@/composables/useApiError'
import PartnerPicker, { type PartnerOption } from '@/components/PartnerPicker.vue'

const route = useRoute()
const { handle } = useApiError()

const kind = route.meta.ledger as LedgerKind
const isAR = kind === 'receivable'

const kindLabels: Record<StatementRow['kind'], string> = {
  goods_receipt: '進貨',
  purchase_return: '退出',
  delivery: '出貨',
  sales_return: '退回',
  receipt: '收款',
  payment: '付款',
}

const monthStart = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-01`
}
const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const query = reactive({
  partner_id: null as number | null,
  currency: 'TWD',
  range: [monthStart(), today()] as [string, string],
})
const result = ref<Statement | null>(null)
const loading = ref(false)
const hint = ref('')

function onPick(p: PartnerOption | null) {
  if (p?.currency) query.currency = p.currency
}

async function run() {
  if (!query.partner_id) {
    hint.value = `請先選擇${isAR ? '客戶' : '供應商'}`
    return
  }
  hint.value = ''
  loading.value = true
  try {
    result.value = await settlementApi.statement({
      side: kind,
      partner_id: query.partner_id,
      currency: query.currency,
      from: query.range[0],
      to: query.range[1],
    })
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

const money = (v: string) =>
  Number(v).toLocaleString('zh-TW', { minimumFractionDigits: 0, maximumFractionDigits: 4 })
// 帳款增加為正(借方),收付款 / 退回退出為負(貸方)
const increase = (r: StatementRow) => (Number(r.delta) > 0 ? money(r.delta) : '')
const decrease = (r: StatementRow) => (Number(r.delta) < 0 ? money(String(-Number(r.delta))) : '')
const print = () => window.print()
const title = computed(() => (isAR ? '應收對帳單' : '應付對帳單'))
</script>

<template>
  <div>
    <div class="page-toolbar">
      <div style="width: 220px">
        <PartnerPicker
          v-model="query.partner_id"
          :kind="isAR ? 'customer' : 'supplier'"
          @select="onPick"
        />
      </div>
      <el-input v-model="query.currency" maxlength="3" style="width: 90px" placeholder="幣別" />
      <el-date-picker
        v-model="query.range"
        type="daterange"
        value-format="YYYY-MM-DD"
        :clearable="false"
        start-placeholder="開始日期"
        end-placeholder="結束日期"
        style="width: 240px"
      />
      <el-button type="primary" :loading="loading" @click="run">查詢</el-button>
      <span v-if="hint" class="err">{{ hint }}</span>
      <span class="spacer" />
      <el-button v-if="result" @click="print">列印</el-button>
    </div>

    <template v-if="result">
      <h3 class="head">
        {{ title }}:{{ result.partner_code }} {{ result.partner_name }}({{ result.currency }},
        {{ result.from }} ~ {{ result.to }})
      </h3>
      <el-table :data="result.rows" border size="small">
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column label="類別" width="80">
          <template #default="{ row }">{{ kindLabels[row.kind as StatementRow['kind']] }}</template>
        </el-table-column>
        <el-table-column prop="doc_no" label="單號" min-width="160" />
        <el-table-column :label="isAR ? '應收增加' : '應付增加'" width="140" align="right">
          <template #default="{ row }">{{ increase(row) }}</template>
        </el-table-column>
        <el-table-column :label="isAR ? '收款 / 退回' : '付款 / 退出'" width="140" align="right">
          <template #default="{ row }">{{ decrease(row) }}</template>
        </el-table-column>
        <el-table-column label="餘額" width="140" align="right">
          <template #default="{ row }">{{ money(row.balance) }}</template>
        </el-table-column>
      </el-table>
      <div class="sum">
        <span
          >期初餘額 <strong>{{ money(result.opening) }}</strong></span
        >
        <span
          >期末餘額 <strong>{{ money(result.closing) }}</strong></span
        >
      </div>
    </template>
    <el-empty v-else description="選擇對象與期間後查詢" />
  </div>
</template>

<style scoped>
.head {
  margin: 8px 0;
  font-size: 15px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.sum {
  display: flex;
  gap: 24px;
  justify-content: flex-end;
  margin-top: 12px;
}
@media print {
  .page-toolbar {
    display: none;
  }
}
</style>
