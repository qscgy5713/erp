<script setup lang="ts">
// 營業稅申報(401)資料:依申報期別(雙月)彙總銷項與進項,列明細與待處理單據,可匯出 Excel
import { computed, onMounted, ref } from 'vue'
import { glApi, type MediaPreview, type VatReport } from '@/api/gl'
import { ApiRequestError } from '@/api/http'
import { useApiError } from '@/composables/useApiError'

const { handle } = useApiError()

const now = new Date()
const year = ref(now.getFullYear())
const period = ref(Math.floor(now.getMonth() / 2) + 1)
const loading = ref(false)
const report = ref<VatReport | null>(null)
const tab = ref<'sales' | 'purchases' | 'issues'>('sales')
const media = ref<MediaPreview | null>(null)
const mediaLoading = ref(false)
const mediaError = ref('')

const money = (v: string | number) =>
  Number(v).toLocaleString('zh-TW', { maximumFractionDigits: 0 })

const years = computed(() => Array.from({ length: 6 }, (_, i) => now.getFullYear() - i))
const periodLabel = (p: number) => `第 ${p} 期(${p * 2 - 1}–${p * 2} 月)`
const kindLabel = { taxable: '應稅', zero: '零稅率', exempt: '免稅' }
const typeLabel: Record<string, string> = {
  delivery: '出貨',
  receipt: '進貨',
  return: '退回 / 退出',
}
const issueLabel = {
  sales_no_invoice: '出貨 / 退回尚未登錄發票(未計入銷項)',
  purchase_no_invoice: '進貨 / 退出沒有供應商發票號碼(未計入進項)',
}

const rows = computed(() => {
  const s = report.value?.summary
  if (!s) return []
  return [
    { label: '銷項', head: true },
    { label: '應稅銷售額 — 三聯式(買方有統一編號)', v: s.taxable_triplicate },
    { label: '應稅銷售額 — 二聯式(買方無統一編號)', v: s.taxable_duplicate },
    { label: '應稅銷售額合計', v: s.taxable_sales, total: true },
    { label: '零稅率銷售額', v: s.zero_sales },
    { label: '免稅銷售額', v: s.exempt_sales },
    { label: '銷售額總計', v: s.total_sales, total: true },
    { label: '銷項稅額', v: s.output_tax, total: true },
    { label: '進項', head: true },
    { label: '可扣抵進貨金額(應稅)', v: s.deductible_goods },
    { label: '可扣抵費用及其他金額(應稅)', v: s.deductible_expense },
    { label: '可扣抵進項稅額', v: s.input_tax, total: true },
    { label: '零稅率 / 免稅進貨金額', v: s.non_tax_purchase },
    { label: '稅額計算', head: true },
    {
      label: `本期${Number(s.net_tax) < 0 ? '留抵' : '應納'}稅額(銷項稅額 − 進項稅額)`,
      v: s.net_tax,
      total: true,
    },
  ]
})

let seq = 0
async function run() {
  const mine = ++seq
  loading.value = true
  try {
    const res = await glApi.vat401(year.value, period.value)
    if (mine === seq) {
      report.value = res
      media.value = null // 換了期別,先前的申報檔檢查結果作廢
      mediaError.value = ''
    }
  } catch (e) {
    if (mine !== seq) return
    report.value = null
    handle(e)
  } finally {
    if (mine === seq) loading.value = false
  }
}

async function checkMedia() {
  mediaLoading.value = true
  mediaError.value = ''
  media.value = null
  try {
    media.value = await glApi.vat401Media(year.value, period.value)
  } catch (e) {
    // 公司資料沒填是可預期的狀況,直接顯示在區塊內並指引去哪裡設定
    if (e instanceof ApiRequestError && e.code === 'GL-040') mediaError.value = e.message
    else handle(e)
  } finally {
    mediaLoading.value = false
  }
}

async function downloadMedia() {
  if (!media.value) return
  try {
    await glApi.downloadVat401Media(year.value, period.value, media.value.file_name)
  } catch (e) {
    handle(e)
  }
}

async function exportXlsx() {
  try {
    await glApi.exportVat401(year.value, period.value)
  } catch (e) {
    handle(e)
  }
}

onMounted(run)
</script>

<template>
  <div>
    <div class="page-toolbar">
      <el-select v-model="year" style="width: 110px" @change="run">
        <el-option v-for="y in years" :key="y" :label="`${y} 年`" :value="y" />
      </el-select>
      <el-select v-model="period" style="width: 170px" @change="run">
        <el-option v-for="p in 6" :key="p" :label="periodLabel(p)" :value="p" />
      </el-select>
      <el-button :disabled="!report" @click="exportXlsx">匯出 Excel</el-button>
    </div>

    <el-alert
      type="info"
      :closable="false"
      show-icon
      class="mb"
      title="本表為依系統資料彙總的申報參考數字(已過帳單據、本位幣、退回已沖減),不含上期留抵稅額、扣抵比例、海關代徵等項目;正式申報請由會計核對後填報。"
    />
    <el-alert
      v-if="report?.issues.length"
      type="warning"
      :closable="false"
      show-icon
      class="mb"
      :title="`有 ${report.issues.length} 張單據沒有發票號碼,未計入申報,請到「待處理」補登或確認。`"
    />

    <div v-loading="loading" class="vat">
      <el-table
        v-if="report"
        :data="rows"
        border
        size="small"
        :show-header="false"
        :row-class-name="
          ({ row }: { row: { head?: boolean; total?: boolean } }) =>
            row.head ? 'k-head' : row.total ? 'k-total' : ''
        "
      >
        <el-table-column prop="label" min-width="320" />
        <el-table-column width="150" align="right">
          <template #default="{ row }">{{ row.head ? '' : money(row.v) }}</template>
        </el-table-column>
      </el-table>

      <el-tabs v-if="report" v-model="tab" class="tabs">
        <el-tab-pane :label="`銷項明細 (${report.sales.length})`" name="sales" />
        <el-tab-pane :label="`進項明細 (${report.purchases.length})`" name="purchases" />
        <el-tab-pane :label="`待處理 (${report.issues.length})`" name="issues" />
      </el-tabs>
      <el-table
        v-if="report && tab !== 'issues'"
        :data="tab === 'sales' ? report.sales : report.purchases"
        border
        size="small"
      >
        <el-table-column prop="doc_no" label="單號" width="170" />
        <el-table-column label="類型" width="100">
          <template #default="{ row }">{{ typeLabel[row.doc_type] }}</template>
        </el-table-column>
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column prop="invoice_no" label="發票號碼" width="130" />
        <el-table-column label="稅別" width="80">
          <template #default="{ row }">{{
            kindLabel[row.tax_kind as keyof typeof kindLabel]
          }}</template>
        </el-table-column>
        <el-table-column
          prop="partner"
          :label="tab === 'sales' ? '客戶' : '供應商'"
          min-width="160"
        />
        <el-table-column prop="partner_tax_id" label="統一編號" width="110" />
        <el-table-column label="金額(未稅)" width="120" align="right">
          <template #default="{ row }">{{ money(row.untaxed) }}</template>
        </el-table-column>
        <el-table-column label="稅額" width="100" align="right">
          <template #default="{ row }">{{ money(row.tax) }}</template>
        </el-table-column>
      </el-table>
      <el-table v-else-if="report" :data="report.issues" border size="small">
        <el-table-column label="問題" min-width="260">
          <template #default="{ row }">{{
            issueLabel[row.kind as keyof typeof issueLabel]
          }}</template>
        </el-table-column>
        <el-table-column prop="doc_no" label="單號" width="170" />
        <el-table-column prop="date" label="日期" width="110" />
        <el-table-column prop="partner" label="對象" min-width="160" />
        <el-table-column label="金額(未稅)" width="120" align="right">
          <template #default="{ row }">{{ money(row.untaxed) }}</template>
        </el-table-column>
        <el-table-column label="稅額" width="100" align="right">
          <template #default="{ row }">{{ money(row.tax) }}</template>
        </el-table-column>
      </el-table>

      <el-card v-if="report" shadow="never" class="media">
        <div class="media-head">
          <strong>媒體申報檔(第一版)</strong>
          <el-button :loading="mediaLoading" @click="checkMedia">檢查申報檔</el-button>
          <el-button v-if="media && media.count" type="primary" @click="downloadMedia">
            下載 {{ media.file_name }}
          </el-button>
        </div>
        <el-alert v-if="mediaError" type="warning" :closable="false" show-icon :title="mediaError">
          <router-link to="/system/company">前往公司資料</router-link>
        </el-alert>
        <template v-if="media">
          <p class="muted">{{ media.covered }}</p>
          <p>
            申報檔共 <strong>{{ media.count }}</strong> 筆:銷項
            {{ media.totals.sales_count }} 筆(銷售額 {{ money(media.totals.sales_amount) }}、稅額
            {{ money(media.totals.sales_tax) }}),進項 {{ media.totals.purchase_count }} 筆(金額
            {{ money(media.totals.purchase_amount) }}、稅額
            {{ money(media.totals.purchase_tax) }})。
          </p>
          <el-alert
            v-if="media.excluded.length"
            type="warning"
            :closable="false"
            show-icon
            :title="`有 ${media.excluded.length} 張單據沒有納入申報檔,與上方 401 彙總的差異來自這些單據`"
          />
          <el-table
            v-if="media.excluded.length"
            :data="media.excluded"
            border
            size="small"
            class="ex"
          >
            <el-table-column label="類別" width="80">
              <template #default="{ row }">{{ row.side === 'sales' ? '銷項' : '進項' }}</template>
            </el-table-column>
            <el-table-column prop="doc_no" label="單號" width="170" />
            <el-table-column prop="reason" label="未納入原因" min-width="320" />
            <el-table-column label="金額(未稅)" width="110" align="right">
              <template #default="{ row }">{{ money(row.untaxed) }}</template>
            </el-table-column>
          </el-table>
          <p class="muted">
            申報前請先用財政部提供的媒體申報檢核軟體驗證這個檔案;檔案內容的規格說明見
            doc/vat-media-spec.md。
          </p>
        </template>
      </el-card>
    </div>
  </div>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
.vat {
  max-width: 960px;
}
.tabs {
  margin-top: 16px;
}
.media {
  margin-top: 20px;
}
.media-head {
  display: flex;
  gap: 12px;
  align-items: center;
  margin-bottom: 12px;
}
.muted {
  color: var(--el-text-color-secondary);
}
.ex {
  margin: 12px 0;
}
:deep(.k-head td) {
  background: var(--el-fill-color-light);
  font-weight: 600;
}
:deep(.k-total td) {
  font-weight: 600;
}
</style>
