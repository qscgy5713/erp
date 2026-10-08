<script setup lang="ts">
// 收款單 / 付款單編輯頁(route meta.settle):選擇對象與幣別 → 挑未沖帳款 → 輸入本次沖帳金額。
// 沖帳金額與帳款餘額同號(退回 / 退出的負數可與正數互抵),合計不可為負;過帳才真正沖銷。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { DocAction } from '@/api/inventory'
import { masterdataApi, type Currency } from '@/api/masterdata'
import {
  financeApi,
  methodLabels,
  settlementApi,
  type LedgerEntry,
  type Settlement,
  type SettleMethod,
  type SettleSide,
} from '@/api/finance'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { actionLabels, allowedActions, confirmActions } from '@/utils/docstate'
import { formatDateTime } from '@/utils/format'
import DocStatusTag from '@/components/DocStatusTag.vue'
import PartnerPicker, { type PartnerOption } from '@/components/PartnerPicker.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const side = route.meta.settle as SettleSide
const isCollection = side === 'collection'
const perm = `finance.${side}`
const title = isCollection ? '收款單' : '付款單'
const partnerLabel = isCollection ? '客戶' : '供應商'
const listRoute = `${side}s`
const docRoute = `${side}`

const doc = ref<Settlement | null>(null)
const isNew = computed(() => !doc.value)
const editable = computed(
  () => auth.can([`${perm}.write`]) && (isNew.value || doc.value?.status === 'draft'),
)
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)
const currencies = ref<Currency[]>([])

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

interface Row {
  target_id: number
  source_type: LedgerEntry['source_type']
  source_no: string
  source_date: string
  due_date: string
  balance: string
  amount: string
}

const form = reactive({
  doc_date: today(),
  partner_id: null as number | null,
  partner_label: '',
  currency: 'TWD',
  method: 'transfer' as SettleMethod,
  reference: '',
  note: '',
  lines: [] as Row[],
})

const decimals = computed(
  () => currencies.value.find((c) => c.code === form.currency)?.decimals ?? 0,
)

const sourceLabels: Record<LedgerEntry['source_type'], string> = {
  goods_receipt: '進貨',
  purchase_return: '退出',
  delivery: '出貨',
  sales_return: '退回',
}

function applyDoc(d: Settlement) {
  doc.value = d
  Object.assign(form, {
    doc_date: d.doc_date,
    partner_id: d.partner_id,
    partner_label: `${d.partner_code} ${d.partner_name}`,
    currency: d.currency,
    method: d.method,
    reference: d.reference,
    note: d.note,
    lines: d.lines.map((l) => ({
      target_id: l.target_id,
      source_type: l.source_type,
      source_no: l.source_no,
      source_date: l.source_date,
      due_date: l.due_date,
      // 過帳後帳款的已沖金額已含本單,餘額顯示過帳前的值
      balance: d.status === 'posted' ? String(Number(l.balance) + Number(l.amount)) : l.balance,
      amount: l.amount,
    })),
  })
  reset()
  setTimeout(() => (dirty.value = false))
}

watch(form, () => (dirty.value = true), { deep: true })

async function load() {
  const id = Number(route.params.id)
  if (!id) return
  loading.value = true
  try {
    applyDoc(await settlementApi.get(side, id))
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

function onPickPartner(p: PartnerOption | null) {
  if (!p) return
  form.partner_label = `${p.code} ${p.name}`
  // 換對象後原本選的帳款不再適用
  form.lines = []
  if (p.currency) form.currency = p.currency
}

function onCurrencyChange() {
  form.lines = [] // 只能沖同幣別的帳款
}

const total = computed(() =>
  form.lines.reduce((s, l) => s + (Number.isNaN(Number(l.amount)) ? 0 : Number(l.amount)), 0),
)

const fmt = (v: number | string | null | undefined, places = decimals.value) =>
  v === null || v === undefined || v === ''
    ? ''
    : Number(v).toLocaleString('zh-TW', {
        minimumFractionDigits: places,
        maximumFractionDigits: Math.max(places, 4),
      })

// ---- 挑選未沖帳款 ----

const pickVisible = ref(false)
const pickLoading = ref(false)
const pickRows = ref<LedgerEntry[]>([])
const pickSelected = ref<LedgerEntry[]>([])

async function openPick() {
  if (!form.partner_id) {
    fieldErrors.value = { partner_id: `請先選擇${partnerLabel}` }
    return
  }
  pickSelected.value = []
  pickVisible.value = true
  pickLoading.value = true
  try {
    const res = await financeApi.ledger(isCollection ? 'receivable' : 'payable', {
      [isCollection ? 'customer_id' : 'supplier_id']: form.partner_id,
      open_only: true,
      size: 100,
    })
    // 只列同幣別、且不在本單已選的帳款
    const chosen = new Set(form.lines.map((l) => l.target_id))
    pickRows.value = res.items.filter((r) => r.currency === form.currency && !chosen.has(r.id))
  } catch (e) {
    handle(e)
  } finally {
    pickLoading.value = false
  }
}

function applyPick() {
  for (const r of pickSelected.value) {
    form.lines.push({
      target_id: r.id,
      source_type: r.source_type,
      source_no: r.source_no,
      source_date: r.doc_date,
      due_date: r.due_date,
      balance: r.balance,
      amount: r.balance, // 預設沖全額
    })
  }
  pickVisible.value = false
}

function lineError(i: number): string | undefined {
  return fieldErrors.value[`lines.${i}`]
}

async function save(): Promise<boolean> {
  const missing: Record<string, string> = {}
  if (!form.partner_id) missing.partner_id = `請選擇${partnerLabel}`
  if (Object.keys(missing).length) {
    fieldErrors.value = missing
    return false
  }
  saving.value = true
  try {
    const input = {
      doc_date: form.doc_date,
      partner_id: form.partner_id!,
      currency: form.currency,
      method: form.method,
      reference: form.reference,
      note: form.note,
      lines: form.lines.map((l) => ({ target_id: l.target_id, amount: l.amount })),
      version: doc.value?.version,
    }
    const saved = doc.value
      ? await settlementApi.update(side, doc.value.id, input)
      : await settlementApi.create(side, input)
    applyDoc(saved)
    ElMessage.success('已儲存')
    if (route.name !== docRoute) router.replace({ name: docRoute, params: { id: saved.id } })
    return true
  } catch (e) {
    handle(e)
    return false
  } finally {
    saving.value = false
  }
}

// ---- 狀態動作 ----

function canDo(action: DocAction): boolean {
  if (!doc.value) return false
  switch (action) {
    case 'submit':
      return auth.can([`${perm}.write`])
    case 'approve':
    case 'reject':
    case 'unapprove':
      return auth.can([`${perm}.approve`])
    case 'post':
    case 'unpost':
      return auth.can([`${perm}.post`])
    case 'void':
      return doc.value.status === 'draft'
        ? auth.can([`${perm}.write`])
        : auth.can([`${perm}.approve`])
    default:
      return false // 收付款單不使用結案
  }
}

const actions = computed(() =>
  doc.value ? allowedActions(doc.value.status, 'posting').filter(canDo) : [],
)
const acting = ref<DocAction | null>(null)

const settleConfirm: Partial<Record<DocAction, string>> = {
  post: `過帳會沖銷所選的${isCollection ? '應收' : '應付'}帳款,確定過帳?`,
  unpost: `反過帳會恢復所選${isCollection ? '應收' : '應付'}帳款的未沖餘額,確定?`,
}

async function runAction(action: DocAction) {
  if (!doc.value) return
  const msg = settleConfirm[action] ?? confirmActions[action]
  if (msg) {
    try {
      await ElMessageBox.confirm(msg, actionLabels[action], {
        type: 'warning',
        confirmButtonText: actionLabels[action],
        cancelButtonText: '取消',
      })
    } catch {
      return
    }
  }
  if (dirty.value && editable.value && !(await save())) return
  acting.value = action
  try {
    applyDoc(await settlementApi.action(side, doc.value.id, action, doc.value.version))
    ElMessage.success(`已${actionLabels[action]}`)
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

onMounted(async () => {
  loading.value = true
  try {
    currencies.value = await masterdataApi.currencies()
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
  await load()
  setTimeout(() => (dirty.value = false))
})
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: listRoute })">← 返回列表</el-button>
        <h2>{{ title }} {{ doc?.doc_no ?? '(新單據)' }}</h2>
        <DocStatusTag v-if="doc" :status="doc.status" />
        <el-tag v-if="dirty && editable && doc" type="warning" effect="plain">未儲存</el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="saving" @click="save">儲存</el-button>
        <el-button
          v-for="a in actions"
          :key="a"
          :type="
            a === 'post' || a === 'approve'
              ? 'success'
              : a === 'void' || a === 'unpost'
                ? 'danger'
                : 'default'
          "
          :loading="acting === a"
          @click="runAction(a)"
        >
          {{ actionLabels[a] }}
        </el-button>
      </div>
    </div>

    <el-card shadow="never" class="mb">
      <el-form :model="form" label-width="90px" :disabled="!editable">
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="日期" :error="fieldErrors.doc_date">
              <el-date-picker
                v-model="form.doc_date"
                value-format="YYYY-MM-DD"
                :clearable="false"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item :label="partnerLabel" :error="fieldErrors.partner_id">
              <PartnerPicker
                v-model="form.partner_id"
                :kind="isCollection ? 'customer' : 'supplier'"
                :label="form.partner_label"
                :disabled="!editable"
                @select="onPickPartner"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="幣別" :error="fieldErrors.currency">
              <el-select v-model="form.currency" style="width: 100%" @change="onCurrencyChange">
                <el-option
                  v-for="c in currencies.filter((x) => x.is_active || x.code === form.currency)"
                  :key="c.code"
                  :label="`${c.code} ${c.name}`"
                  :value="c.code"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item
              :label="isCollection ? '收款方式' : '付款方式'"
              :error="fieldErrors.method"
            >
              <el-select v-model="form.method" style="width: 100%">
                <el-option v-for="(label, v) in methodLabels" :key="v" :label="label" :value="v" />
              </el-select>
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="12">
            <el-form-item label="備查" :error="fieldErrors.reference">
              <el-input v-model="form.reference" maxlength="50" placeholder="匯款帳號末碼 / 票號" />
            </el-form-item>
          </el-col>
          <el-col v-if="doc?.sales_user_name" :xs="24" :sm="12" :md="6">
            <el-form-item label="負責業務">{{ doc.sales_user_name }}</el-form-item>
          </el-col>
        </el-row>
        <el-form-item label="備註">
          <el-input v-model="form.note" type="textarea" :rows="1" maxlength="2000" />
        </el-form-item>
      </el-form>
    </el-card>

    <el-card shadow="never">
      <template #header>
        <div class="card-header">
          <span>
            沖帳明細({{ form.lines.length }} 筆)
            <span v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</span>
          </span>
          <el-button v-if="editable" size="small" @click="openPick">
            選擇未沖{{ isCollection ? '應收' : '應付' }}
          </el-button>
        </div>
      </template>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column label="來源單據" min-width="190">
          <template #default="{ row, $index }">
            {{ sourceLabels[row.source_type as LedgerEntry['source_type']] }} {{ row.source_no }}
            <div v-if="lineError($index)" class="err">{{ lineError($index) }}</div>
          </template>
        </el-table-column>
        <el-table-column prop="source_date" label="單據日期" width="110" />
        <el-table-column prop="due_date" label="到期日" width="110" />
        <el-table-column label="未沖餘額" width="130" align="right">
          <template #default="{ row }">{{ fmt(row.balance) }}</template>
        </el-table-column>
        <el-table-column label="本次沖帳" width="160">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.amount" size="small" />
            <span v-else>{{ fmt(row.amount) }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="editable" width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="form.lines.splice($index, 1)">移除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="footer">
        <span class="hint">負數的帳款(退回 / 退出)可與正數互抵;合計不可為負。</span>
        <span class="spacer" />
        <strong>
          {{ isCollection ? '收款' : '付款' }}合計 {{ form.currency }}
          {{ fmt(editable ? total : doc?.amount) }}
        </strong>
      </div>
    </el-card>

    <el-descriptions v-if="doc" :column="4" border size="small" class="mt">
      <el-descriptions-item label="建立">{{ doc.created_by_name }}</el-descriptions-item>
      <el-descriptions-item label="送審">
        {{ doc.submitted_by_name }} {{ formatDateTime(doc.submitted_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="核准">
        {{ doc.approved_by_name }} {{ formatDateTime(doc.approved_at) }}
      </el-descriptions-item>
      <el-descriptions-item label="過帳">
        {{ doc.posted_by_name }} {{ formatDateTime(doc.posted_at) }}
      </el-descriptions-item>
    </el-descriptions>

    <el-dialog
      v-model="pickVisible"
      :title="`選擇未沖${isCollection ? '應收' : '應付'}(${form.currency})`"
      width="760px"
      :close-on-click-modal="false"
    >
      <el-table
        v-loading="pickLoading"
        :data="pickRows"
        border
        size="small"
        max-height="420"
        @selection-change="(rows: LedgerEntry[]) => (pickSelected = rows)"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column label="來源" min-width="170">
          <template #default="{ row }">
            {{ sourceLabels[row.source_type as LedgerEntry['source_type']] }} {{ row.source_no }}
          </template>
        </el-table-column>
        <el-table-column prop="doc_date" label="單據日期" width="110" />
        <el-table-column prop="due_date" label="到期日" width="110" />
        <el-table-column label="未沖餘額" width="130" align="right">
          <template #default="{ row }">{{ fmt(row.balance) }}</template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="pickVisible = false">取消</el-button>
        <el-button type="primary" :disabled="pickSelected.length === 0" @click="applyPick">
          加入 {{ pickSelected.length }} 筆
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped>
.doc-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-bottom: 12px;
}
.title {
  display: flex;
  align-items: center;
  gap: 8px;
}
.title h2 {
  margin: 0;
  font-size: 18px;
}
.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.mb {
  margin-bottom: 12px;
}
.mt {
  margin-top: 12px;
}
.err {
  color: var(--el-color-danger);
  font-size: 12px;
}
.hint {
  color: var(--el-text-color-secondary);
  font-size: 12px;
}
.footer {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}
.spacer {
  flex: 1;
}
</style>
