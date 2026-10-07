<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  masterdataApi,
  type Currency,
  type ExchangeRate,
  type PaymentTerm,
  type TaxType,
} from '@/api/masterdata'
import type { PageMeta } from '@/api/http'
import { useAuthStore } from '@/stores/auth'
import { useFormDialog } from '@/composables/useFormDialog'
import { useApiError } from '@/composables/useApiError'
import { decimalRule, required } from '@/utils/validators'
import ActiveTag from '@/components/ActiveTag.vue'

const auth = useAuthStore()
const canWrite = computed(() => auth.can('masterdata.finance.write'))
const canReadRates = computed(() =>
  auth.can(['masterdata.finance.read', 'masterdata.finance.write']),
)
const { handle } = useApiError()
const tab = ref('currencies')

// ---- 幣別 ----
const currencies = ref<Currency[]>([])
const activeCurrencies = computed(() => currencies.value.filter((c) => c.is_active && !c.is_base))

async function loadCurrencies() {
  try {
    currencies.value = await masterdataApi.currencies()
  } catch (e) {
    handle(e)
  }
}

async function toggleCurrency(c: Currency, v: boolean) {
  try {
    await masterdataApi.setCurrencyActive(c.code, v)
    await loadCurrencies()
  } catch (e) {
    handle(e)
  }
}

// ---- 匯率 ----
const rateQuery = reactive({ currency: '', page: 1, size: 20 })
const rates = ref<ExchangeRate[]>([])
const rateMeta = ref<PageMeta>({ page: 1, size: 20, total: 0 })

async function loadRates() {
  if (!canReadRates.value) return
  try {
    const res = await masterdataApi.exchangeRates(rateQuery)
    rates.value = res.items
    rateMeta.value = res.meta
  } catch (e) {
    handle(e)
  }
}

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

const rateDlg = useFormDialog({
  defaults: () => ({ currency: 'USD', rate_date: today(), rate: '' }),
  fromRow: (r: ExchangeRate) => ({ currency: r.currency, rate_date: r.rate_date, rate: r.rate }),
  create: (f) => masterdataApi.createExchangeRate(f),
  update: (id, f) => masterdataApi.updateExchangeRate(id, { rate: f.rate, version: f.version }),
  onSaved: loadRates,
})

const rateFormRef = rateDlg.formRef

async function deleteRate(r: ExchangeRate) {
  try {
    await ElMessageBox.confirm(`刪除 ${r.currency} ${r.rate_date} 的匯率?`, '刪除匯率', {
      type: 'warning',
      confirmButtonText: '刪除',
      cancelButtonText: '取消',
    })
  } catch {
    return
  }
  try {
    await masterdataApi.deleteExchangeRate(r.id)
    ElMessage.success('已刪除')
    await loadRates()
  } catch (e) {
    handle(e)
  }
}

// ---- 稅別 ----
const taxTypes = ref<TaxType[]>([])
const kindLabels: Record<TaxType['kind'], string> = {
  taxable: '應稅',
  zero: '零稅率',
  exempt: '免稅',
}

async function loadTaxTypes() {
  try {
    taxTypes.value = await masterdataApi.taxTypes()
  } catch (e) {
    handle(e)
  }
}

const taxDlg = useFormDialog({
  defaults: () => ({
    code: '',
    name: '',
    kind: 'taxable' as TaxType['kind'],
    rate: '0.05',
    is_active: true,
  }),
  fromRow: (r: TaxType) => ({
    code: r.code,
    name: r.name,
    kind: r.kind,
    rate: r.rate,
    is_active: r.is_active,
  }),
  create: (f) => masterdataApi.taxType.create(f),
  update: (id, f) => masterdataApi.taxType.update(id, f),
  onSaved: loadTaxTypes,
})

const taxFormRef = taxDlg.formRef

function onKindChange(kind: TaxType['kind']) {
  if (kind !== 'taxable') taxDlg.form.rate = '0'
}

const percent = (rate: string) => `${(Number(rate) * 100).toFixed(2).replace(/\.?0+$/, '')}%`

// ---- 付款條件 ----
const terms = ref<PaymentTerm[]>([])

async function loadTerms() {
  try {
    terms.value = await masterdataApi.paymentTerms()
  } catch (e) {
    handle(e)
  }
}

const termDlg = useFormDialog({
  defaults: () => ({ code: '', name: '', is_month_end: false, net_days: 30, is_active: true }),
  fromRow: (r: PaymentTerm) => ({
    code: r.code,
    name: r.name,
    is_month_end: r.is_month_end,
    net_days: r.net_days,
    is_active: r.is_active,
  }),
  create: (f) => masterdataApi.paymentTerm.create(f),
  update: (id, f) => masterdataApi.paymentTerm.update(id, f),
  onSaved: loadTerms,
})

const termFormRef = termDlg.formRef

const termDesc = (t: { is_month_end: boolean; net_days: number }) =>
  t.is_month_end
    ? t.net_days
      ? `單據日當月底後 ${t.net_days} 天`
      : '單據日當月底'
    : t.net_days
      ? `單據日後 ${t.net_days} 天`
      : '單據日當天'

onMounted(() => {
  loadCurrencies()
  loadRates()
  loadTaxTypes()
  loadTerms()
})
</script>

<template>
  <el-tabs v-model="tab">
    <el-tab-pane label="幣別" name="currencies">
      <el-table :data="currencies" border>
        <el-table-column prop="code" label="代碼" width="100" />
        <el-table-column prop="name" label="名稱" />
        <el-table-column prop="symbol" label="符號" width="100" />
        <el-table-column prop="decimals" label="小數位" width="90" align="right" />
        <el-table-column label="啟用" width="140">
          <template #default="{ row }">
            <el-tag v-if="row.is_base" type="primary">本位幣</el-tag>
            <el-switch
              v-else
              :model-value="row.is_active"
              :disabled="!canWrite"
              @change="(v: string | number | boolean) => toggleCurrency(row, !!v)"
            />
          </template>
        </el-table-column>
      </el-table>
    </el-tab-pane>

    <el-tab-pane v-if="canReadRates" label="匯率" name="rates">
      <div class="page-toolbar">
        <el-select
          v-model="rateQuery.currency"
          clearable
          placeholder="幣別"
          style="width: 140px"
          @change="((rateQuery.page = 1), loadRates())"
        >
          <el-option
            v-for="c in activeCurrencies"
            :key="c.code"
            :label="`${c.code} ${c.name}`"
            :value="c.code"
          />
        </el-select>
        <span class="spacer" />
        <el-button v-if="canWrite" type="primary" @click="rateDlg.openCreate()">新增匯率</el-button>
      </div>
      <el-alert
        type="info"
        :closable="false"
        show-icon
        title="匯率為 1 單位外幣換算新台幣的金額;開單時取單據日當天或之前最近一筆。"
        class="mb"
      />
      <el-table :data="rates" border>
        <el-table-column prop="rate_date" label="日期" width="140" />
        <el-table-column prop="currency" label="幣別" width="100" />
        <el-table-column prop="rate" label="匯率" align="right" />
        <el-table-column v-if="canWrite" label="操作" width="140">
          <template #default="{ row }">
            <el-button link type="primary" @click="rateDlg.openEdit(row)">編輯</el-button>
            <el-button link type="danger" @click="deleteRate(row)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="pager">
        <el-pagination
          v-model:current-page="rateQuery.page"
          v-model:page-size="rateQuery.size"
          :total="rateMeta.total"
          layout="total, prev, pager, next"
          @current-change="loadRates"
        />
      </div>
    </el-tab-pane>

    <el-tab-pane label="稅別" name="tax">
      <div class="page-toolbar">
        <span class="spacer" />
        <el-button v-if="canWrite" type="primary" @click="taxDlg.openCreate()">新增稅別</el-button>
      </div>
      <el-table :data="taxTypes" border>
        <el-table-column prop="code" label="代碼" width="120" />
        <el-table-column prop="name" label="名稱" />
        <el-table-column label="類別" width="110">
          <template #default="{ row }">{{ kindLabels[row.kind as TaxType['kind']] }}</template>
        </el-table-column>
        <el-table-column label="稅率" width="100" align="right">
          <template #default="{ row }">{{ percent(row.rate) }}</template>
        </el-table-column>
        <el-table-column label="狀態" width="100">
          <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
        </el-table-column>
        <el-table-column v-if="canWrite" label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="primary" @click="taxDlg.openEdit(row)">編輯</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-tab-pane>

    <el-tab-pane label="付款條件" name="terms">
      <div class="page-toolbar">
        <span class="spacer" />
        <el-button v-if="canWrite" type="primary" @click="termDlg.openCreate()"
          >新增付款條件</el-button
        >
      </div>
      <el-table :data="terms" border>
        <el-table-column prop="code" label="代碼" width="120" />
        <el-table-column prop="name" label="名稱" />
        <el-table-column label="到期日" min-width="200">
          <template #default="{ row }">{{ termDesc(row) }}</template>
        </el-table-column>
        <el-table-column label="狀態" width="100">
          <template #default="{ row }"><ActiveTag :active="row.is_active" /></template>
        </el-table-column>
        <el-table-column v-if="canWrite" label="操作" width="90">
          <template #default="{ row }">
            <el-button link type="primary" @click="termDlg.openEdit(row)">編輯</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-tab-pane>
  </el-tabs>

  <!-- 匯率 -->
  <el-dialog
    v-model="rateDlg.visible.value"
    :title="rateDlg.editing.value ? '編輯匯率' : '新增匯率'"
    width="420px"
    :close-on-click-modal="false"
  >
    <el-form ref="rateFormRef" :model="rateDlg.form" label-width="70px">
      <el-form-item label="幣別" :error="rateDlg.fieldErrors.value.currency">
        <el-select
          v-model="rateDlg.form.currency"
          :disabled="!!rateDlg.editing.value"
          style="width: 100%"
        >
          <el-option
            v-for="c in activeCurrencies"
            :key="c.code"
            :label="`${c.code} ${c.name}`"
            :value="c.code"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="日期" :error="rateDlg.fieldErrors.value.rate_date">
        <el-date-picker
          v-model="rateDlg.form.rate_date"
          value-format="YYYY-MM-DD"
          :disabled="!!rateDlg.editing.value"
          :clearable="false"
          style="width: 100%"
        />
      </el-form-item>
      <el-form-item
        label="匯率"
        prop="rate"
        :rules="[required, decimalRule(6, { positive: true })]"
        :error="rateDlg.fieldErrors.value.rate"
      >
        <el-input v-model="rateDlg.form.rate" placeholder="例:32.45" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="rateDlg.visible.value = false">取消</el-button>
      <el-button type="primary" :loading="rateDlg.saving.value" @click="rateDlg.save"
        >儲存</el-button
      >
    </template>
  </el-dialog>

  <!-- 稅別 -->
  <el-dialog
    v-model="taxDlg.visible.value"
    :title="taxDlg.editing.value ? '編輯稅別' : '新增稅別'"
    width="440px"
    :close-on-click-modal="false"
  >
    <el-form ref="taxFormRef" :model="taxDlg.form" label-width="70px">
      <el-form-item
        label="代碼"
        prop="code"
        :rules="required"
        :error="taxDlg.fieldErrors.value.code"
      >
        <el-input v-model="taxDlg.form.code" maxlength="10" />
      </el-form-item>
      <el-form-item
        label="名稱"
        prop="name"
        :rules="required"
        :error="taxDlg.fieldErrors.value.name"
      >
        <el-input v-model="taxDlg.form.name" maxlength="50" />
      </el-form-item>
      <el-form-item label="類別">
        <el-radio-group
          v-model="taxDlg.form.kind"
          @change="(v: string | number | boolean | undefined) => onKindChange(v as TaxType['kind'])"
        >
          <el-radio v-for="(label, k) in kindLabels" :key="k" :value="k">{{ label }}</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item
        label="稅率"
        prop="rate"
        :rules="decimalRule(4)"
        :error="taxDlg.fieldErrors.value.rate"
      >
        <el-input v-model="taxDlg.form.rate" :disabled="taxDlg.form.kind !== 'taxable'">
          <template #append>5% 請填 0.05</template>
        </el-input>
      </el-form-item>
      <el-form-item v-if="taxDlg.editing.value" label="啟用"
        ><el-switch v-model="taxDlg.form.is_active"
      /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="taxDlg.visible.value = false">取消</el-button>
      <el-button type="primary" :loading="taxDlg.saving.value" @click="taxDlg.save">儲存</el-button>
    </template>
  </el-dialog>

  <!-- 付款條件 -->
  <el-dialog
    v-model="termDlg.visible.value"
    :title="termDlg.editing.value ? '編輯付款條件' : '新增付款條件'"
    width="440px"
    :close-on-click-modal="false"
  >
    <el-form ref="termFormRef" :model="termDlg.form" label-width="70px">
      <el-form-item
        label="代碼"
        prop="code"
        :rules="required"
        :error="termDlg.fieldErrors.value.code"
      >
        <el-input v-model="termDlg.form.code" maxlength="20" />
      </el-form-item>
      <el-form-item
        label="名稱"
        prop="name"
        :rules="required"
        :error="termDlg.fieldErrors.value.name"
      >
        <el-input v-model="termDlg.form.name" maxlength="50" />
      </el-form-item>
      <el-form-item label="月結"><el-switch v-model="termDlg.form.is_month_end" /></el-form-item>
      <el-form-item label="天數" :error="termDlg.fieldErrors.value.net_days">
        <el-input-number v-model="termDlg.form.net_days" :min="0" :max="365" />
      </el-form-item>
      <el-form-item label="到期日">{{ termDesc(termDlg.form) }}</el-form-item>
      <el-form-item v-if="termDlg.editing.value" label="啟用"
        ><el-switch v-model="termDlg.form.is_active"
      /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="termDlg.visible.value = false">取消</el-button>
      <el-button type="primary" :loading="termDlg.saving.value" @click="termDlg.save"
        >儲存</el-button
      >
    </template>
  </el-dialog>
</template>

<style scoped>
.mb {
  margin-bottom: 12px;
}
</style>
