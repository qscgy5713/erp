<script setup lang="ts">
// 採購單、進貨單、進貨退出單共用的編輯頁:route meta.kind 區分採購單(order)與進貨 / 退出(receipt)
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import type { DocAction, ItemOption } from '@/api/inventory'
import { inventoryApi } from '@/api/inventory'
import {
  masterdataApi,
  type Currency,
  type ItemUnit,
  type PaymentTerm,
  type TaxType,
  type Warehouse,
} from '@/api/masterdata'
import {
  purchaseApi,
  type GoodsReceipt,
  type LineInput,
  type OutstandingLine,
  type PurchaseOrder,
  type ReceiptDocType,
  type ReturnableLine,
  type SupplierOption,
} from '@/api/purchase'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { actionLabels, allowedActions, confirmActions } from '@/utils/docstate'
import { formatDateTime } from '@/utils/format'
import DocStatusTag from '@/components/DocStatusTag.vue'
import ItemPicker from '@/components/ItemPicker.vue'
import SupplierPicker from '@/components/SupplierPicker.vue'

const BASE_CURRENCY = 'TWD'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const isOrder = route.meta.kind === 'order'
const perm = isOrder ? 'purchase.order' : 'purchase.receipt'
const listRoute = isOrder ? 'purchase-orders' : 'purchase-receipts'
const docRoute = isOrder ? 'purchase-order' : 'purchase-receipt'

const order = ref<PurchaseOrder | null>(null)
const receipt = ref<GoodsReceipt | null>(null)
const doc = computed(() => order.value ?? receipt.value)
const isNew = computed(() => !doc.value)
const docType = computed<ReceiptDocType>(
  () => receipt.value?.doc_type ?? (route.query.type === 'return' ? 'return' : 'receipt'),
)
const title = computed(() =>
  isOrder ? '採購單' : docType.value === 'return' ? '進貨退出單' : '進貨單',
)
const editable = computed(
  () => auth.can(`${perm}.write`) && (isNew.value || doc.value?.status === 'draft'),
)
const loading = ref(false)
const saving = ref(false)
const dirty = ref(false)

const warehouses = ref<Warehouse[]>([])
const currencies = ref<Currency[]>([])
const taxTypes = ref<TaxType[]>([])
const paymentTerms = ref<PaymentTerm[]>([])

const today = () => {
  const d = new Date()
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

interface LineRow {
  id?: number
  item_id: number | null
  unit_id: number | null
  qty: string
  unit_price: string
  note: string
  item_code?: string
  item_name?: string
  item_type?: string
  base_unit_name?: string
  /** 前端用:此料品可選的單位 */
  unitOptions: { id: number; name: string; factor: string }[]
  po_line_id?: number | null
  po_no?: string | null
  receipt_line_id?: number | null
  source_receipt_no?: string | null
  /** 引用來源時的剩餘量(帶入時的提示,以後端為準) */
  available?: string
  received_qty?: string
  remaining_qty?: string
}

const form = reactive({
  doc_date: today(),
  supplier_id: null as number | null,
  supplier_label: '',
  warehouse_id: null as number | null,
  expected_date: null as string | null,
  currency: BASE_CURRENCY,
  exchange_rate: '1',
  tax_type_id: null as number | null,
  payment_term_id: null as number | null,
  invoice_no: '',
  note: '',
  lines: [] as LineRow[],
})

const decimals = computed(
  () => currencies.value.find((c) => c.code === form.currency)?.decimals ?? 0,
)
const isForeign = computed(() => form.currency !== BASE_CURRENCY)

function unitOptionsOf(item: ItemOption): LineRow['unitOptions'] {
  return [
    { id: item.base_unit_id, name: item.base_unit_name, factor: '1' },
    ...item.units.map((u: ItemUnit) => ({
      id: u.unit_id,
      name: u.unit_name ?? '',
      factor: u.factor,
    })),
  ]
}

function applyDoc(d: PurchaseOrder | GoodsReceipt) {
  if (isOrder) order.value = d as PurchaseOrder
  else receipt.value = d as GoodsReceipt
  Object.assign(form, {
    doc_date: d.doc_date,
    supplier_id: d.supplier_id,
    supplier_label: `${d.supplier_code} ${d.supplier_name}`,
    warehouse_id: d.warehouse_id,
    expected_date: isOrder ? (d as PurchaseOrder).expected_date : null,
    currency: d.currency,
    exchange_rate: d.exchange_rate,
    tax_type_id: d.tax_type_id,
    payment_term_id: d.payment_term_id,
    invoice_no: isOrder ? '' : (d as GoodsReceipt).invoice_no,
    note: d.note,
    lines: d.lines.map((l) => ({
      ...l,
      unitOptions: [{ id: l.unit_id, name: l.unit_name, factor: l.factor }],
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
    applyDoc(isOrder ? await purchaseApi.order(id) : await purchaseApi.receipt(id))
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

// ---- 單頭連動 ----

/** 選供應商時帶入預設幣別、稅別、付款條件 */
function onPickSupplier(s: SupplierOption | null) {
  if (!s) return
  form.supplier_label = `${s.code} ${s.name}`
  if (s.tax_type_id) form.tax_type_id = s.tax_type_id
  form.payment_term_id = s.payment_term_id
  if (s.currency && s.currency !== form.currency) {
    form.currency = s.currency
    lookupRate()
  }
}

const rateHint = ref('')

/** 外幣:依日期帶入匯率(可手動修改,以單據上的匯率入帳)。
 *  只在使用者改幣別 / 日期時呼叫,不用 watch,避免載入既有單據時覆蓋已存的匯率 */
async function lookupRate() {
  rateHint.value = ''
  if (!isForeign.value) {
    form.exchange_rate = '1'
    return
  }
  try {
    form.exchange_rate = (await masterdataApi.lookupRate(form.currency, form.doc_date)).rate
  } catch {
    form.exchange_rate = ''
    rateHint.value = `查無 ${form.currency} 在 ${form.doc_date} 以前的匯率,請手動輸入`
  }
}

// ---- 明細 ----

async function loadUnits(row: LineRow) {
  if (row.unitOptions.length > 1 || !row.item_code) return
  try {
    const found = (await inventoryApi.itemOptions(row.item_code)).find((o) => o.id === row.item_id)
    if (found) row.unitOptions = unitOptionsOf(found)
  } catch (e) {
    handle(e)
  }
}

function onPickItem(row: LineRow, item: ItemOption | null) {
  if (!item) return
  row.item_code = item.code
  row.item_name = item.name
  row.item_type = item.item_type
  row.base_unit_name = item.base_unit_name
  row.unitOptions = unitOptionsOf(item)
  row.unit_id = item.base_unit_id
}

function addLine() {
  form.lines.push({
    item_id: null,
    unit_id: null,
    qty: '',
    unit_price: '',
    note: '',
    unitOptions: [],
  })
}

/** 有引用來源的明細:料品與單位跟著來源,不可修改 */
const linked = (row: LineRow) => !!(row.po_line_id || row.receipt_line_id)

function round(n: number, places: number): number {
  const f = 10 ** places
  return (Math.round(Math.abs(n) * f + Number.EPSILON) / f) * Math.sign(n)
}

/** 金額預覽(以後端為準) */
function amountOf(row: LineRow): number | null {
  const q = Number(row.qty)
  const p = Number(row.unit_price)
  if (row.qty === '' || row.unit_price === '' || Number.isNaN(q) || Number.isNaN(p)) return null
  return round(q * p, decimals.value)
}

const preview = computed(() => {
  const rate = Number(taxTypes.value.find((t) => t.id === form.tax_type_id)?.rate ?? 0)
  const untaxed = round(
    form.lines.reduce((s, l) => s + (amountOf(l) ?? 0), 0),
    decimals.value,
  )
  const tax = round(untaxed * rate, decimals.value)
  return { untaxed, tax, total: round(untaxed + tax, decimals.value) }
})

const fmt = (v: number | string | null | undefined, places = decimals.value) =>
  v === null || v === undefined || v === ''
    ? ''
    : Number(v).toLocaleString('zh-TW', {
        minimumFractionDigits: places,
        maximumFractionDigits: Math.max(places, 6),
      })

// ---- 從來源單據帶入 ----

const importVisible = ref(false)
const importKeyword = ref('')
const importLoading = ref(false)
const importRows = ref<(OutstandingLine | ReturnableLine)[]>([])
const importSelected = ref<(OutstandingLine | ReturnableLine)[]>([])

async function searchImport() {
  if (!form.supplier_id) return
  importLoading.value = true
  try {
    importRows.value =
      docType.value === 'receipt'
        ? (
            await purchaseApi.outstanding({
              supplier_id: form.supplier_id,
              currency: form.currency,
              keyword: importKeyword.value.trim(),
              size: 100,
            })
          ).items
        : await purchaseApi.returnable({
            supplier_id: form.supplier_id,
            currency: form.currency,
            keyword: importKeyword.value.trim(),
          })
  } catch (e) {
    handle(e)
  } finally {
    importLoading.value = false
  }
}

function openImport() {
  if (!form.supplier_id) {
    fieldErrors.value = { supplier_id: '請先選擇供應商' }
    return
  }
  importKeyword.value = ''
  importSelected.value = []
  importVisible.value = true
  searchImport()
}

function refOf(r: OutstandingLine | ReturnableLine): number {
  return 'po_line_id' in r ? r.po_line_id : r.receipt_line_id
}

function applyImport(rows: (OutstandingLine | ReturnableLine)[]) {
  const existing = new Set(form.lines.map((l) => l.po_line_id ?? l.receipt_line_id))
  // 先移除空白列,再接上帶入的明細
  form.lines = form.lines.filter((l) => l.item_id !== null)
  for (const r of rows) {
    if (existing.has(refOf(r))) continue
    const isPo = 'po_line_id' in r
    form.lines.push({
      item_id: r.item_id,
      unit_id: r.unit_id,
      qty: r.remaining_qty,
      unit_price: r.unit_price,
      note: '',
      item_code: r.item_code,
      item_name: r.item_name,
      unitOptions: [{ id: r.unit_id, name: r.unit_name, factor: '' }],
      po_line_id: isPo ? r.po_line_id : null,
      po_no: isPo ? r.doc_no : null,
      receipt_line_id: isPo ? null : r.receipt_line_id,
      source_receipt_no: isPo ? null : r.doc_no,
      available: r.remaining_qty,
    })
  }
  if (!form.warehouse_id && rows[0]) form.warehouse_id = rows[0].warehouse_id
  importVisible.value = false
}

/** 採購單「轉進貨」:帶入該單的未交明細 */
async function prefillFromOrder(orderId: number) {
  try {
    const po = await purchaseApi.order(orderId)
    Object.assign(form, {
      supplier_id: po.supplier_id,
      supplier_label: `${po.supplier_code} ${po.supplier_name}`,
      warehouse_id: po.warehouse_id,
      currency: po.currency,
      tax_type_id: po.tax_type_id,
      payment_term_id: po.payment_term_id,
    })
    if (isForeign.value) await lookupRate()
    applyImport(
      po.lines
        .filter((l) => Number(l.remaining_qty) > 0)
        .map((l) => ({
          po_line_id: l.id,
          order_id: po.id,
          doc_no: po.doc_no,
          doc_date: po.doc_date,
          expected_date: po.expected_date,
          supplier_id: po.supplier_id,
          supplier_code: po.supplier_code,
          supplier_name: po.supplier_name,
          currency: po.currency,
          warehouse_id: po.warehouse_id,
          line_no: l.line_no,
          item_id: l.item_id,
          item_code: l.item_code,
          item_name: l.item_name,
          item_spec: l.item_spec,
          unit_id: l.unit_id,
          unit_name: l.unit_name,
          qty: l.qty,
          unit_price: l.unit_price,
          received_qty: l.received_qty,
          remaining_qty: l.remaining_qty,
        })),
    )
  } catch (e) {
    handle(e)
  }
}

// ---- 儲存 ----

// 後端錯誤以「送出的明細序號」標示;空白列送出前會被略過,需對應回畫面上的列
const sentIndex = ref<number[]>([])

function lineError(uiIndex: number): string | undefined {
  const i = sentIndex.value.indexOf(uiIndex)
  return i < 0 ? undefined : fieldErrors.value[`lines.${i}`]
}

function payload() {
  sentIndex.value = form.lines.flatMap((l, i) => (l.item_id !== null ? [i] : []))
  const lines: LineInput[] = form.lines
    .filter((l): l is LineRow & { item_id: number } => l.item_id !== null)
    .map((l) => ({
      item_id: l.item_id,
      unit_id: l.unit_id ?? 0,
      qty: l.qty === '' ? '0' : l.qty,
      unit_price: l.unit_price === '' ? '0' : l.unit_price,
      po_line_id: l.po_line_id ?? null,
      receipt_line_id: l.receipt_line_id ?? null,
      note: l.note,
    }))
  return {
    doc_date: form.doc_date,
    supplier_id: form.supplier_id!,
    warehouse_id: form.warehouse_id!,
    currency: form.currency,
    exchange_rate: isForeign.value && form.exchange_rate !== '' ? form.exchange_rate : null,
    tax_type_id: form.tax_type_id!,
    payment_term_id: form.payment_term_id,
    note: form.note,
    lines,
  }
}

async function save(): Promise<boolean> {
  const missing: Record<string, string> = {}
  if (!form.supplier_id) missing.supplier_id = '請選擇供應商'
  if (!form.warehouse_id) missing.warehouse_id = '請選擇倉庫'
  if (!form.tax_type_id) missing.tax_type_id = '請選擇稅別'
  if (Object.keys(missing).length) {
    fieldErrors.value = missing
    return false
  }
  saving.value = true
  try {
    const base = payload()
    const version = doc.value?.version
    let saved: PurchaseOrder | GoodsReceipt
    if (isOrder) {
      const input = { ...base, expected_date: form.expected_date || null, version }
      saved = order.value
        ? await purchaseApi.updateOrder(order.value.id, input)
        : await purchaseApi.createOrder(input)
    } else {
      const input = { ...base, doc_type: docType.value, invoice_no: form.invoice_no, version }
      saved = receipt.value
        ? await purchaseApi.updateReceipt(receipt.value.id, input)
        : await purchaseApi.createReceipt(input)
    }
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
      return auth.can(`${perm}.write`)
    case 'approve':
    case 'reject':
    case 'unapprove':
    case 'close':
    case 'reopen':
      return auth.can(`${perm}.approve`)
    case 'post':
    case 'unpost':
      return auth.can('purchase.receipt.post')
    case 'void':
      return doc.value.status === 'draft' ? auth.can(`${perm}.write`) : auth.can(`${perm}.approve`)
  }
}

const actions = computed(() =>
  doc.value ? allowedActions(doc.value.status, isOrder ? 'order' : 'posting').filter(canDo) : [],
)
const acting = ref<DocAction | null>(null)

async function runAction(action: DocAction) {
  if (!doc.value) return
  if (confirmActions[action]) {
    try {
      await ElMessageBox.confirm(confirmActions[action]!, actionLabels[action], {
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
    const id = doc.value.id
    const v = doc.value.version
    applyDoc(
      isOrder
        ? await purchaseApi.orderAction(id, action, v)
        : await purchaseApi.receiptAction(id, action, v),
    )
    ElMessage.success(`已${actionLabels[action]}`)
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

const canConvert = computed(
  () =>
    isOrder &&
    order.value?.status === 'approved' &&
    order.value.lines.some((l) => Number(l.remaining_qty) > 0) &&
    auth.can('purchase.receipt.write'),
)

onMounted(async () => {
  loading.value = true
  try {
    ;[warehouses.value, currencies.value, taxTypes.value, paymentTerms.value] = await Promise.all([
      masterdataApi.warehouses(),
      masterdataApi.currencies(),
      masterdataApi.taxTypes(),
      masterdataApi.paymentTerms(),
    ])
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
  await load()
  if (isNew.value) {
    form.tax_type_id = taxTypes.value.find((t) => t.is_active && t.code === 'TX5')?.id ?? null
    const fromPo = Number(route.query.from_po)
    if (!isOrder && fromPo) await prefillFromOrder(fromPo)
    if (form.lines.length === 0) addLine()
  }
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
        <el-button
          v-if="canConvert"
          type="primary"
          plain
          @click="router.push({ name: 'purchase-receipt-new', query: { from_po: order!.id } })"
        >
          轉進貨單
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
                @change="lookupRate"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="供應商" :error="fieldErrors.supplier_id">
              <SupplierPicker
                v-model="form.supplier_id"
                :label="form.supplier_label"
                :disabled="!editable"
                @select="onPickSupplier"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item
              :label="isOrder ? '預定入庫倉' : docType === 'return' ? '出庫倉' : '入庫倉'"
              :error="fieldErrors.warehouse_id"
            >
              <el-select v-model="form.warehouse_id" style="width: 100%">
                <el-option
                  v-for="w in warehouses.filter((x) => x.is_active || x.id === form.warehouse_id)"
                  :key="w.id"
                  :label="`${w.code} ${w.name}`"
                  :value="w.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col v-if="isOrder" :xs="24" :sm="12" :md="6">
            <el-form-item label="預定交貨日" :error="fieldErrors.expected_date">
              <el-date-picker
                v-model="form.expected_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col v-else :xs="24" :sm="12" :md="6">
            <el-form-item label="發票號碼" :error="fieldErrors.invoice_no">
              <el-input v-model="form.invoice_no" maxlength="20" />
            </el-form-item>
          </el-col>
        </el-row>
        <el-row :gutter="16">
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="幣別" :error="fieldErrors.currency">
              <el-select v-model="form.currency" style="width: 100%" @change="lookupRate">
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
            <el-form-item label="匯率" :error="fieldErrors.exchange_rate || rateHint">
              <el-input v-model="form.exchange_rate" :disabled="!isForeign" />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="稅別" :error="fieldErrors.tax_type_id">
              <el-select v-model="form.tax_type_id" style="width: 100%">
                <el-option
                  v-for="t in taxTypes.filter((x) => x.is_active || x.id === form.tax_type_id)"
                  :key="t.id"
                  :label="t.name"
                  :value="t.id"
                />
              </el-select>
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="付款條件" :error="fieldErrors.payment_term_id">
              <el-select v-model="form.payment_term_id" clearable style="width: 100%">
                <el-option
                  v-for="t in paymentTerms.filter(
                    (x) => x.is_active || x.id === form.payment_term_id,
                  )"
                  :key="t.id"
                  :label="t.name"
                  :value="t.id"
                />
              </el-select>
            </el-form-item>
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
            明細({{ form.lines.filter((l) => l.item_id).length }} 筆)
            <span v-if="fieldErrors.lines" class="err">{{ fieldErrors.lines }}</span>
          </span>
          <el-button v-if="editable && !isOrder" size="small" @click="openImport">
            {{ docType === 'receipt' ? '從採購單帶入' : '從進貨單帶入' }}
          </el-button>
        </div>
      </template>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column v-if="!isOrder" label="來源" width="140">
          <template #default="{ row }">{{ row.po_no ?? row.source_receipt_no ?? '' }}</template>
        </el-table-column>
        <el-table-column label="料品" min-width="240">
          <template #default="{ row, $index }">
            <ItemPicker
              v-if="editable && !linked(row)"
              v-model="row.item_id"
              :label="row.item_code ? `${row.item_code} ${row.item_name}` : undefined"
              @select="(it) => onPickItem(row, it)"
            />
            <span v-else>{{ row.item_code }} {{ row.item_name }}</span>
            <el-tag v-if="row.item_type === 'service'" size="small" class="ml">服務</el-tag>
            <div v-if="lineError($index)" class="err">{{ lineError($index) }}</div>
          </template>
        </el-table-column>
        <el-table-column label="單位" width="110">
          <template #default="{ row }">
            <el-select
              v-if="editable && !linked(row)"
              v-model="row.unit_id"
              size="small"
              :disabled="!row.item_id"
              @visible-change="(v: boolean) => v && loadUnits(row)"
            >
              <el-option v-for="u in row.unitOptions" :key="u.id" :label="u.name" :value="u.id" />
            </el-select>
            <span v-else>{{ row.unitOptions[0]?.name ?? row.unit_name }}</span>
          </template>
        </el-table-column>
        <el-table-column label="數量" width="130">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.qty" size="small" />
            <span v-else>{{ fmt(row.qty, 0) }}</span>
            <div v-if="editable && row.available" class="hint">
              剩餘 {{ fmt(row.available, 0) }}
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="`單價(${form.currency},未稅)`" width="150">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.unit_price" size="small" />
            <span v-else>{{ fmt(row.unit_price, 0) }}</span>
          </template>
        </el-table-column>
        <el-table-column label="金額" width="120" align="right">
          <template #default="{ row }">
            {{ fmt(editable ? amountOf(row) : row.amount) }}
          </template>
        </el-table-column>
        <template v-if="isOrder && doc && doc.status !== 'draft'">
          <el-table-column label="已交" width="90" align="right">
            <template #default="{ row }">{{ fmt(row.received_qty, 0) }}</template>
          </el-table-column>
          <el-table-column label="未交" width="90" align="right">
            <template #default="{ row }">{{ fmt(row.remaining_qty, 0) }}</template>
          </el-table-column>
        </template>
        <el-table-column label="備註" min-width="140">
          <template #default="{ row }">
            <el-input v-if="editable" v-model="row.note" size="small" maxlength="255" />
            <span v-else>{{ row.note }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="editable" width="60">
          <template #default="{ $index }">
            <el-button link type="danger" @click="form.lines.splice($index, 1)">刪除</el-button>
          </template>
        </el-table-column>
      </el-table>
      <div class="footer">
        <el-button v-if="editable" @click="addLine">新增明細</el-button>
        <span class="spacer" />
        <div class="totals">
          <span>未稅 {{ fmt(editable ? preview.untaxed : doc?.untaxed_amount) }}</span>
          <span>稅額 {{ fmt(editable ? preview.tax : doc?.tax_amount) }}</span>
          <strong>
            合計 {{ form.currency }} {{ fmt(editable ? preview.total : doc?.total_amount) }}
          </strong>
          <span v-if="receipt && isForeign" class="base">
            (本位幣 TWD {{ fmt(receipt.base_total, 0) }})
          </span>
        </div>
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
      <el-descriptions-item v-if="order" label="結案">
        {{ order.closed_by_name }} {{ formatDateTime(order.closed_at) }}
      </el-descriptions-item>
      <el-descriptions-item v-if="receipt" label="過帳">
        {{ receipt.posted_by_name }} {{ formatDateTime(receipt.posted_at) }}
      </el-descriptions-item>
    </el-descriptions>

    <el-dialog
      v-model="importVisible"
      :title="docType === 'receipt' ? '從採購單帶入(未交明細)' : '從進貨單帶入(可退明細)'"
      width="860px"
      :close-on-click-modal="false"
    >
      <div class="page-toolbar">
        <el-input
          v-model="importKeyword"
          placeholder="單號 / 料號 / 品名"
          clearable
          style="width: 220px"
          @keyup.enter="searchImport"
          @clear="searchImport"
        />
        <el-button @click="searchImport">查詢</el-button>
        <span class="hint">只列出 {{ form.currency }} 且供應商相同的單據</span>
      </div>
      <el-table
        v-loading="importLoading"
        :data="importRows"
        border
        size="small"
        max-height="420"
        @selection-change="(rows: (OutstandingLine | ReturnableLine)[]) => (importSelected = rows)"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column prop="doc_no" label="單號" width="150" />
        <el-table-column prop="doc_date" label="日期" width="100" />
        <el-table-column label="料品" min-width="200">
          <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
        </el-table-column>
        <el-table-column prop="unit_name" label="單位" width="70" />
        <el-table-column label="數量" width="90" align="right">
          <template #default="{ row }">{{ fmt(row.qty, 0) }}</template>
        </el-table-column>
        <el-table-column :label="docType === 'receipt' ? '未交' : '可退'" width="90" align="right">
          <template #default="{ row }">{{ fmt(row.remaining_qty, 0) }}</template>
        </el-table-column>
        <el-table-column label="單價" width="100" align="right">
          <template #default="{ row }">{{ fmt(row.unit_price, 0) }}</template>
        </el-table-column>
      </el-table>
      <template #footer>
        <el-button @click="importVisible = false">取消</el-button>
        <el-button
          type="primary"
          :disabled="importSelected.length === 0"
          @click="applyImport(importSelected)"
        >
          帶入 {{ importSelected.length }} 筆
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
.ml {
  margin-left: 6px;
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
.totals {
  display: flex;
  gap: 16px;
  align-items: baseline;
  flex-wrap: wrap;
}
.base {
  color: var(--el-text-color-secondary);
}
</style>
