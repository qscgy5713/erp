<script setup lang="ts">
// 採購 / 銷售單據共用編輯頁:route meta.kind 決定流程(採購單、進貨 / 退出、報價 / 訂單、出貨 / 退回),
// 差異集中在 flows.ts。
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { inventoryApi, type DocAction, type ItemOption } from '@/api/inventory'
import {
  masterdataApi,
  type Currency,
  type ItemUnit,
  type PaymentTerm,
  type TaxType,
  type Warehouse,
} from '@/api/masterdata'
import { salesApi, type Availability } from '@/api/sales'
import { useAuthStore } from '@/stores/auth'
import { useApiError } from '@/composables/useApiError'
import { actionLabels, allowedActions, confirmActions } from '@/utils/docstate'
import { formatDateTime } from '@/utils/format'
import DocStatusTag from '@/components/DocStatusTag.vue'
import ItemPicker from '@/components/ItemPicker.vue'
import PartnerPicker, { type PartnerOption } from '@/components/PartnerPicker.vue'
import ApprovalProgress from '@/components/ApprovalProgress.vue'
import LotCell from '@/components/LotCell.vue'
import BinCell from '@/components/BinCell.vue'
import { useBinStore } from '@/composables/useBinStore'
import type { ApprovalProgress as ApprovalProgressData } from '@/api/approval'
import { flows, type FlowKind, type ImportRow, type TradeDoc } from './flows'

const BASE_CURRENCY = 'TWD'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const { fieldErrors, handle, reset } = useApiError()

const flow = flows[route.meta.kind as FlowKind]

const doc = ref<TradeDoc | null>(null)
const isNew = computed(() => !doc.value)
const docType = computed<string>(() => {
  if (doc.value) return doc.value.doc_type
  const t = String(route.query.type ?? '')
  return flow.docTypes.includes(t) ? t : flow.docTypes[0]!
})
const title = computed(() => flow.titles[docType.value] ?? '')
const refKey = computed(() => flow.refKey[docType.value])
const importer = computed(() => flow.importers[docType.value])
const isQuotation = computed(() => docType.value === 'quotation')
const editable = computed(
  () => auth.can([`${flow.perm}.write`]) && (isNew.value || doc.value?.status === 'draft'),
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
  unit_name?: string
  amount?: string
  /** 前端用:此料品可選的單位 */
  unitOptions: { id: number; name: string; factor: string }[]
  ref_id?: number | null
  ref_no?: string | null
  /** 帶入來源時的剩餘量(提示用,以後端為準) */
  available?: string
  done_qty?: string
  remaining_qty?: string
  /** 批號管理的料品:批號與效期(進貨 / 出貨類);已過帳的單據另有實際出庫的批號 */
  item_lot_control?: string
  lot_no?: string
  bin_code?: string
  expiry_date?: string | null
  lots?: { lot_no: string; expiry_date: string | null; qty: string }[]
}

const form = reactive({
  doc_date: today(),
  partner_id: null as number | null,
  partner_label: '',
  sales_user_name: null as string | null,
  warehouse_id: null as number | null,
  expected_date: null as string | null,
  valid_until: null as string | null,
  customer_po_no: '',
  quotation_id: null as number | null,
  quotation_no: null as string | null,
  currency: BASE_CURRENCY,
  exchange_rate: '1',
  tax_type_id: null as number | null,
  payment_term_id: null as number | null,
  invoice_no: '',
  invoice_date: null as string | null,
  invoice_kind: '',
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

function applyDoc(d: TradeDoc) {
  doc.value = d
  Object.assign(form, {
    doc_date: d.doc_date,
    partner_id: d.partner_id,
    partner_label: d.partner_label,
    sales_user_name: d.sales_user_name,
    warehouse_id: d.warehouse_id,
    expected_date: d.expected_date,
    valid_until: d.valid_until,
    customer_po_no: d.customer_po_no,
    quotation_id: d.quotation_id,
    quotation_no: d.quotation_no,
    currency: d.currency,
    exchange_rate: d.exchange_rate,
    tax_type_id: d.tax_type_id,
    payment_term_id: d.payment_term_id,
    invoice_no: d.invoice_no,
    invoice_date: d.invoice_date,
    invoice_kind: d.invoice_kind,
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
    applyDoc(await flow.load(id))
  } catch (e) {
    handle(e)
  } finally {
    loading.value = false
  }
}

// ---- 單頭連動 ----

/** 選往來對象時帶入預設幣別、稅別、付款條件(客戶另帶出負責業務) */
function onPickPartner(p: PartnerOption | null) {
  if (!p) return
  form.partner_label = `${p.code} ${p.name}`
  form.sales_user_name = p.sales_user_name ?? null
  if (p.tax_type_id) form.tax_type_id = p.tax_type_id
  form.payment_term_id = p.payment_term_id
  if (p.currency && p.currency !== form.currency) {
    form.currency = p.currency
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
  row.item_lot_control = item.lot_control
  row.lot_no = ''
  row.expiry_date = null
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
const linked = (row: LineRow) => !!row.ref_id

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

// ---- 可用量(銷售):訂單 / 報價看「現有 − 其他訂單保留」,出貨單看現有量 ----

const stockMode = computed<'available' | 'on_hand' | null>(() => {
  if (flow.side !== 'sales' || docType.value === 'return') return null
  if (doc.value && !['draft', 'pending', 'approved'].includes(doc.value.status)) return null
  return flow.kind === 'delivery' ? 'on_hand' : 'available'
})
const stock = ref<Record<number, Availability>>({})
const stockKey = computed(() =>
  stockMode.value && form.warehouse_id
    ? `${form.warehouse_id}:${[...new Set(form.lines.map((l) => l.item_id).filter(Boolean))].join(',')}`
    : '',
)

watch(stockKey, async (key) => {
  const ids = [...new Set(form.lines.flatMap((l) => (l.item_id ? [l.item_id] : [])))]
  if (!key || ids.length === 0 || !form.warehouse_id) {
    stock.value = {}
    return
  }
  try {
    const rows = await salesApi.availability(
      form.warehouse_id,
      ids,
      flow.kind === 'sales-order' && doc.value?.doc_type === 'order' ? doc.value.id : undefined,
    )
    if (key === stockKey.value) stock.value = Object.fromEntries(rows.map((r) => [r.item_id, r]))
  } catch (e) {
    handle(e)
  }
})

function stockOf(row: LineRow): string | undefined {
  const s = row.item_id ? stock.value[row.item_id] : undefined
  return s && (stockMode.value === 'on_hand' ? s.on_hand : s.available)
}

/** 數量換算成基本單位後是否超過可用量 / 現有量 */
function short(row: LineRow): boolean {
  const s = stockOf(row)
  if (s === undefined || row.qty === '') return false
  const factor = row.unitOptions.find((u) => u.id === row.unit_id)?.factor || '1'
  return Number(row.qty) * Number(factor) > Number(s)
}

// ---- 從來源單據帶入 ----

const importVisible = ref(false)
const importKeyword = ref('')
const importLoading = ref(false)
const importRows = ref<ImportRow[]>([])
const importSelected = ref<ImportRow[]>([])

async function searchImport() {
  if (!form.partner_id || !importer.value) return
  importLoading.value = true
  try {
    importRows.value = await importer.value.fetch(
      form.partner_id,
      form.currency,
      importKeyword.value.trim(),
    )
  } catch (e) {
    handle(e)
  } finally {
    importLoading.value = false
  }
}

function openImport() {
  if (!form.partner_id) {
    fieldErrors.value = { partner_id: `請先選擇${flow.partnerLabel}` }
    return
  }
  importKeyword.value = ''
  importSelected.value = []
  importVisible.value = true
  searchImport()
}

function applyImport(rows: ImportRow[]) {
  const existing = new Set(form.lines.map((l) => l.ref_id))
  // 先移除空白列,再接上帶入的明細
  form.lines = form.lines.filter((l) => l.item_id !== null)
  for (const r of rows) {
    if (existing.has(r.ref_id)) continue
    form.lines.push({
      item_id: r.item_id,
      unit_id: r.unit_id,
      qty: r.remaining_qty,
      unit_price: r.unit_price,
      note: '',
      item_code: r.item_code,
      item_name: r.item_name,
      unitOptions: [{ id: r.unit_id, name: r.unit_name, factor: '' }],
      ref_id: r.ref_id,
      ref_no: r.doc_no,
      available: r.remaining_qty,
    })
  }
  if (!form.warehouse_id && rows[0]) form.warehouse_id = rows[0].warehouse_id
  importVisible.value = false
}

// ---- 轉單:採購單 → 進貨單、報價單 → 訂單、訂單 → 出貨單 ----

const conversion = computed(() => {
  const d = doc.value
  if (!d || d.status !== 'approved') return null
  const hasRemaining = d.lines.some((l) => Number(l.remaining_qty) > 0)
  if (flow.kind === 'purchase-order' && hasRemaining && auth.can(['purchase.receipt.write']))
    return { label: '轉進貨單', to: { name: 'purchase-receipt-new', query: {} } }
  if (flow.kind === 'sales-order' && d.doc_type === 'quotation' && auth.can(['sales.order.write']))
    return { label: '轉訂單', to: { name: 'sales-order-new', query: { type: 'order' } } }
  if (
    flow.kind === 'sales-order' &&
    d.doc_type === 'order' &&
    hasRemaining &&
    auth.can(['sales.delivery.write'])
  )
    return { label: '轉出貨單', to: { name: 'sales-delivery-new', query: {} } }
  return null
})

function convert() {
  const c = conversion.value
  if (!c || !doc.value) return
  router.push({
    name: c.to.name,
    query: { ...c.to.query, from_kind: flow.kind, from_id: doc.value.id },
  })
}

/** 依轉單來源預填:報價單整張複製(記錄來源報價單);訂單類帶入未交明細並引用來源 */
async function prefill(fromKind: FlowKind, fromId: number) {
  try {
    const src = await flows[fromKind].load(fromId)
    Object.assign(form, {
      partner_id: src.partner_id,
      partner_label: src.partner_label,
      sales_user_name: src.sales_user_name,
      warehouse_id: src.warehouse_id,
      currency: src.currency,
      tax_type_id: src.tax_type_id,
      payment_term_id: src.payment_term_id,
      customer_po_no: src.customer_po_no,
    })
    if (isForeign.value) await lookupRate()
    if (src.doc_type === 'quotation') {
      form.quotation_id = src.id
      form.quotation_no = src.doc_no
      form.lines = src.lines.map((l) => ({
        item_id: l.item_id,
        unit_id: l.unit_id,
        qty: l.qty,
        unit_price: l.unit_price,
        note: l.note,
        item_code: l.item_code,
        item_name: l.item_name,
        item_type: l.item_type,
        base_unit_name: l.base_unit_name,
        unitOptions: [{ id: l.unit_id, name: l.unit_name, factor: l.factor }],
      }))
      return
    }
    applyImport(
      src.lines
        .filter((l) => Number(l.remaining_qty) > 0)
        .map((l) => ({
          ref_id: l.id,
          doc_no: src.doc_no,
          doc_date: src.doc_date,
          warehouse_id: src.warehouse_id,
          item_id: l.item_id,
          item_code: l.item_code,
          item_name: l.item_name,
          unit_id: l.unit_id,
          unit_name: l.unit_name,
          qty: l.qty,
          unit_price: l.unit_price,
          remaining_qty: l.remaining_qty ?? l.qty,
        })),
    )
  } catch (e) {
    handle(e)
  }
}

// ---- 儲存 ----

// ---- 批號與效期(進貨 / 出貨類) ----

/** 入庫類(進貨、銷貨退回)要輸入批號與效期;出庫類(出貨、進貨退出)可留空,系統先到期先出 */
// ---- 儲位:倉庫有啟用儲位時顯示,入庫(進貨、銷貨退回)必填,出庫可留空自動分配 ----
const binStore = useBinStore()
const binTick = ref(0)
void binStore.ensureWarehouses().then(() => binTick.value++)
const binColumn = computed(
  () =>
    (flow.kind === 'receipt' || flow.kind === 'delivery') &&
    binTick.value >= 0 &&
    !!form.warehouse_id &&
    binStore.usesBins(form.warehouse_id),
)

const lotInbound = computed(
  () =>
    (flow.kind === 'receipt' && docType.value === 'receipt') ||
    (flow.kind === 'delivery' && docType.value === 'return'),
)
const lotControlled = (row: LineRow) => !!row.item_lot_control && row.item_lot_control !== 'none'
const lotColumn = computed(
  () => (flow.kind === 'receipt' || flow.kind === 'delivery') && form.lines.some(lotControlled),
)

// 匯入來源明細、載入既有單據時,料品的批號管理方式不在明細資料裡,依料號補查一次
const lotControlLoading = new Set<number>()
async function fillLotControl() {
  if (flow.kind !== 'receipt' && flow.kind !== 'delivery') return
  for (const row of form.lines) {
    if (!row.item_id || !row.item_code || row.item_lot_control !== undefined) continue
    if (lotControlLoading.has(row.item_id)) continue
    lotControlLoading.add(row.item_id)
    try {
      const found = (await inventoryApi.itemOptions(row.item_code)).find(
        (o) => o.id === row.item_id,
      )
      for (const r of form.lines) {
        if (r.item_id === row.item_id) r.item_lot_control = found?.lot_control ?? 'none'
      }
    } catch {
      row.item_lot_control = 'none'
    } finally {
      lotControlLoading.delete(row.item_id)
    }
  }
}
watch(() => form.lines.map((l) => `${l.item_id}:${l.item_lot_control}`).join(','), fillLotControl)

// 後端錯誤以「送出的明細序號」標示;空白列送出前會被略過,需對應回畫面上的列
const sentIndex = ref<number[]>([])

function lineError(uiIndex: number): string | undefined {
  const i = sentIndex.value.indexOf(uiIndex)
  return i < 0 ? undefined : fieldErrors.value[`lines.${i}`]
}

function payload(): Record<string, unknown> {
  sentIndex.value = form.lines.flatMap((l, i) => (l.item_id !== null ? [i] : []))
  const lines = form.lines
    .filter((l): l is LineRow & { item_id: number } => l.item_id !== null)
    .map((l) => ({
      item_id: l.item_id,
      unit_id: l.unit_id ?? 0,
      qty: l.qty === '' ? '0' : l.qty,
      unit_price: l.unit_price === '' ? '0' : l.unit_price,
      note: l.note,
      ...(refKey.value ? { [refKey.value]: l.ref_id ?? null } : {}),
      ...(flow.kind === 'receipt' || flow.kind === 'delivery'
        ? {
            bin_code: binColumn.value ? (l.bin_code ?? '') : '',
            lot_no: lotControlled(l) ? (l.lot_no ?? '') : '',
            expiry_date: lotControlled(l) ? l.expiry_date || null : null,
          }
        : {}),
    }))
  const p: Record<string, unknown> = {
    doc_date: form.doc_date,
    [flow.partner === 'supplier' ? 'supplier_id' : 'customer_id']: form.partner_id,
    warehouse_id: form.warehouse_id,
    currency: form.currency,
    exchange_rate: isForeign.value && form.exchange_rate !== '' ? form.exchange_rate : null,
    tax_type_id: form.tax_type_id,
    payment_term_id: form.payment_term_id,
    note: form.note,
    lines,
    version: doc.value?.version,
  }
  if (flow.kind !== 'purchase-order') p.doc_type = docType.value
  switch (flow.kind) {
    case 'purchase-order':
      p.expected_date = form.expected_date || null
      break
    case 'receipt':
      p.invoice_no = form.invoice_no
      p.invoice_date = form.invoice_date || null
      p.invoice_kind = form.invoice_kind
      break
    case 'sales-order':
      p.customer_po_no = form.customer_po_no
      if (isQuotation.value) p.valid_until = form.valid_until || null
      else {
        p.delivery_date = form.expected_date || null
        p.quotation_id = form.quotation_id
      }
      break
  }
  return p
}

async function save(): Promise<boolean> {
  const missing: Record<string, string> = {}
  if (!form.partner_id) missing.partner_id = `請選擇${flow.partnerLabel}`
  if (!form.warehouse_id) missing.warehouse_id = '請選擇倉庫'
  if (!form.tax_type_id) missing.tax_type_id = '請選擇稅別'
  if (Object.keys(missing).length) {
    fieldErrors.value = missing
    return false
  }
  saving.value = true
  try {
    const saved = await flow.save(doc.value?.id ?? null, payload())
    applyDoc(saved)
    ElMessage.success('已儲存')
    if (route.name !== flow.routes.doc) {
      router.replace({ name: flow.routes.doc, params: { id: saved.id } })
    }
    return true
  } catch (e) {
    handle(e)
    return false
  } finally {
    saving.value = false
  }
}

/** 後端以 supplier_id / customer_id 回報錯誤,畫面上統一顯示在往來對象欄位 */
const partnerError = computed(
  () =>
    fieldErrors.value.partner_id ?? fieldErrors.value.supplier_id ?? fieldErrors.value.customer_id,
)

// ---- 多層簽核 ----
const approvalDocType = {
  'purchase-order': 'purchase_order',
  receipt: 'goods_receipt',
  'sales-order': 'sales_order',
  delivery: 'delivery',
}[flow.kind]
const approvalInfo = ref<ApprovalProgressData | null>(null)
const progressRef = ref<InstanceType<typeof ApprovalProgress> | null>(null)
/** 套用多層簽核的待審單據:只有輪到的人才顯示「核准」 */
const approveBlocked = computed(
  () => doc.value?.status === 'pending' && !!approvalInfo.value && !approvalInfo.value.can_approve,
)

// ---- 狀態動作 ----

function canDo(action: DocAction): boolean {
  if (!doc.value) return false
  switch (action) {
    case 'submit':
      return auth.can([`${flow.perm}.write`])
    case 'approve':
      return auth.can([`${flow.perm}.approve`]) && !approveBlocked.value
    case 'reject':
    case 'unapprove':
    case 'close':
    case 'reopen':
      return auth.can([`${flow.perm}.approve`])
    case 'post':
    case 'unpost':
      return auth.can([`${flow.perm}.post`])
    case 'void':
      return doc.value.status === 'draft'
        ? auth.can([`${flow.perm}.write`])
        : auth.can([`${flow.perm}.approve`])
  }
}

const actions = computed(() =>
  doc.value
    ? allowedActions(doc.value.status, flow.posting ? 'posting' : 'order').filter(canDo)
    : [],
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
    const next = await flow.action(doc.value.id, action, doc.value.version)
    applyDoc(next)
    ElMessage.success(
      action === 'approve' && next.status === 'pending'
        ? '已完成本層核准,等待下一層核准'
        : `已${actionLabels[action]}`,
    )
    progressRef.value?.reload()
  } catch (e) {
    handle(e)
  } finally {
    acting.value = null
  }
}

// ---- 發票登錄(出貨單:任何未作廢狀態皆可) ----

const canInvoice = computed(
  () =>
    flow.kind === 'delivery' &&
    docType.value === 'delivery' &&
    !!doc.value &&
    doc.value.status !== 'voided' &&
    auth.can(['sales.delivery.write']),
)
const invoiceVisible = ref(false)
const invoiceSaving = ref(false)
const invoiceForm = reactive({ invoice_no: '', invoice_date: null as string | null })

function openInvoice() {
  invoiceForm.invoice_no = doc.value?.invoice_no ?? ''
  invoiceForm.invoice_date = doc.value?.invoice_date ?? doc.value?.doc_date ?? null
  reset()
  invoiceVisible.value = true
}

async function saveInvoice() {
  if (!doc.value) return
  invoiceSaving.value = true
  try {
    const d = await salesApi.setInvoice(doc.value.id, {
      invoice_no: invoiceForm.invoice_no.trim().toUpperCase(),
      invoice_date: invoiceForm.invoice_no.trim() ? invoiceForm.invoice_date : null,
      version: doc.value.version,
    })
    applyDoc(await flow.load(d.id))
    invoiceVisible.value = false
    ElMessage.success('已登錄發票')
  } catch (e) {
    handle(e)
  } finally {
    invoiceSaving.value = false
  }
}

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
    const fromKind = route.query.from_kind as FlowKind | undefined
    const fromId = Number(route.query.from_id)
    if (fromKind && flows[fromKind] && fromId) await prefill(fromKind, fromId)
    if (form.lines.length === 0) addLine()
  }
  setTimeout(() => (dirty.value = false))
})
</script>

<template>
  <div v-loading="loading">
    <div class="doc-header">
      <div class="title">
        <el-button link @click="router.push({ name: flow.routes.list })">← 返回列表</el-button>
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
        <el-button v-if="conversion" type="primary" plain @click="convert">
          {{ conversion.label }}
        </el-button>
        <el-button v-if="canInvoice" @click="openInvoice">登錄發票</el-button>
      </div>
    </div>

    <ApprovalProgress
      ref="progressRef"
      :doc-type="approvalDocType"
      :doc-id="doc?.id"
      :status="doc?.status"
      :version="doc?.version"
      @loaded="approvalInfo = $event"
    />

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
            <el-form-item :label="flow.partnerLabel" :error="partnerError">
              <PartnerPicker
                v-model="form.partner_id"
                :kind="flow.partner"
                :label="form.partner_label"
                :disabled="!editable"
                @select="onPickPartner"
              />
            </el-form-item>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item
              :label="
                flow.side === 'purchase'
                  ? docType === 'return'
                    ? '出庫倉'
                    : '入庫倉'
                  : docType === 'return'
                    ? '入庫倉'
                    : '出貨倉'
              "
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
          <el-col v-if="flow.kind === 'purchase-order'" :xs="24" :sm="12" :md="6">
            <el-form-item label="預定交貨日" :error="fieldErrors.expected_date">
              <el-date-picker
                v-model="form.expected_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <template v-else-if="flow.kind === 'receipt'">
            <el-col :xs="24" :sm="12" :md="6">
              <el-form-item label="憑證種類" :error="fieldErrors.invoice_kind">
                <el-select v-model="form.invoice_kind" style="width: 100%">
                  <el-option label="無 / 其他" value="" />
                  <el-option label="三聯式 / 電子計算機發票" value="triplicate" />
                  <el-option label="二聯式收銀機發票" value="register2" />
                  <el-option label="三聯式收銀機 / 電子發票" value="register3" />
                </el-select>
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="6">
              <el-form-item label="發票號碼" :error="fieldErrors.invoice_no">
                <el-input
                  v-model="form.invoice_no"
                  maxlength="20"
                  :placeholder="form.invoice_kind ? 'AB12345678' : ''"
                />
              </el-form-item>
            </el-col>
            <el-col :xs="24" :sm="12" :md="6">
              <el-form-item label="發票日期" :error="fieldErrors.invoice_date">
                <el-date-picker
                  v-model="form.invoice_date"
                  value-format="YYYY-MM-DD"
                  :disabled="!form.invoice_kind && !form.invoice_no"
                  style="width: 100%"
                />
              </el-form-item>
            </el-col>
          </template>
          <el-col v-else-if="isQuotation" :xs="24" :sm="12" :md="6">
            <el-form-item label="有效期限" :error="fieldErrors.valid_until">
              <el-date-picker
                v-model="form.valid_until"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col v-else-if="flow.kind === 'sales-order'" :xs="24" :sm="12" :md="6">
            <el-form-item label="預定出貨日" :error="fieldErrors.delivery_date">
              <el-date-picker
                v-model="form.expected_date"
                value-format="YYYY-MM-DD"
                style="width: 100%"
              />
            </el-form-item>
          </el-col>
          <el-col v-else :xs="24" :sm="12" :md="6">
            <el-form-item label="發票">
              <span v-if="doc?.invoice_no">{{ doc.invoice_no }}({{ doc.invoice_date }})</span>
              <span v-else class="hint">尚未登錄</span>
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
            <el-form-item
              :label="flow.side === 'purchase' ? '付款條件' : '收款條件'"
              :error="fieldErrors.payment_term_id"
            >
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
        <el-row v-if="flow.side === 'sales'" :gutter="16">
          <el-col :xs="24" :sm="12" :md="6">
            <el-form-item label="負責業務">
              <span>{{ form.sales_user_name ?? '(未指定)' }}</span>
            </el-form-item>
          </el-col>
          <el-col v-if="flow.kind === 'sales-order'" :xs="24" :sm="12" :md="6">
            <el-form-item label="客戶單號" :error="fieldErrors.customer_po_no">
              <el-input v-model="form.customer_po_no" maxlength="50" />
            </el-form-item>
          </el-col>
          <el-col v-if="form.quotation_no" :xs="24" :sm="12" :md="6">
            <el-form-item label="來源報價" :error="fieldErrors.quotation_id">
              <!-- 表單停用時 el-button 也會被停用,改用連結 -->
              <RouterLink :to="{ name: 'sales-order', params: { id: form.quotation_id } }">
                {{ form.quotation_no }}
              </RouterLink>
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
          <el-button v-if="editable && importer" size="small" @click="openImport">
            {{ importer.button }}
          </el-button>
        </div>
      </template>
      <el-table :data="form.lines" border size="small">
        <el-table-column type="index" label="#" width="50" />
        <el-table-column v-if="refKey" label="來源" width="140">
          <template #default="{ row }">{{ row.ref_no ?? '' }}</template>
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
        <el-table-column v-if="binColumn" label="儲位" width="150">
          <template #default="{ row }">
            <BinCell
              v-model:code="row.bin_code"
              :warehouse-id="form.warehouse_id"
              :mode="lotInbound ? 'in' : 'out'"
              :editable="editable"
            />
          </template>
        </el-table-column>
        <el-table-column v-if="lotColumn" label="批號 / 效期" width="250">
          <template #default="{ row }">
            <LotCell
              v-model:lot-no="row.lot_no"
              v-model:expiry-date="row.expiry_date"
              :item-id="row.item_id"
              :control="row.item_lot_control"
              :mode="lotInbound ? 'in' : 'out'"
              :warehouse-id="form.warehouse_id"
              :editable="editable"
              :lots="row.lots"
            />
          </template>
        </el-table-column>
        <el-table-column
          v-if="stockMode"
          :label="stockMode === 'on_hand' ? '現有量' : '可用量'"
          width="100"
          align="right"
        >
          <template #default="{ row }">
            <span :class="{ neg: short(row) }">{{ fmt(stockOf(row), 0) }}</span>
            <span v-if="stockOf(row) !== undefined" class="hint"> {{ row.base_unit_name }}</span>
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
        <template v-if="flow.doneLabel && docType !== 'quotation' && doc && doc.status !== 'draft'">
          <el-table-column :label="flow.doneLabel" width="90" align="right">
            <template #default="{ row }">{{ fmt(row.done_qty, 0) }}</template>
          </el-table-column>
          <el-table-column :label="flow.remainLabel" width="90" align="right">
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
          <span v-if="doc?.base_total && isForeign" class="base">
            (本位幣 TWD {{ fmt(doc.base_total, 0) }})
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
      <el-descriptions-item v-if="flow.posting" label="過帳">
        {{ doc.posted_by_name }} {{ formatDateTime(doc.posted_at) }}
      </el-descriptions-item>
      <el-descriptions-item v-else label="結案">
        {{ doc.closed_by_name }} {{ formatDateTime(doc.closed_at) }}
      </el-descriptions-item>
    </el-descriptions>

    <el-dialog
      v-if="importer"
      v-model="importVisible"
      :title="importer.title"
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
        <span class="hint">只列出 {{ form.currency }} 且{{ flow.partnerLabel }}相同的單據</span>
      </div>
      <el-table
        v-loading="importLoading"
        :data="importRows"
        border
        size="small"
        max-height="420"
        @selection-change="(rows: ImportRow[]) => (importSelected = rows)"
      >
        <el-table-column type="selection" width="40" />
        <el-table-column prop="doc_no" label="單號" width="150" />
        <el-table-column prop="doc_date" label="日期" width="100" />
        <el-table-column label="料品" min-width="180">
          <template #default="{ row }">{{ row.item_code }} {{ row.item_name }}</template>
        </el-table-column>
        <el-table-column prop="unit_name" label="單位" width="70" />
        <el-table-column label="數量" width="80" align="right">
          <template #default="{ row }">{{ fmt(row.qty, 0) }}</template>
        </el-table-column>
        <el-table-column :label="importer.remainLabel" width="80" align="right">
          <template #default="{ row }">{{ fmt(row.remaining_qty, 0) }}</template>
        </el-table-column>
        <el-table-column label="單價" width="90" align="right">
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

    <el-dialog
      v-model="invoiceVisible"
      title="登錄發票"
      width="420px"
      :close-on-click-modal="false"
    >
      <el-form label-width="90px">
        <el-form-item label="發票號碼" :error="fieldErrors.invoice_no">
          <el-input v-model="invoiceForm.invoice_no" maxlength="10" placeholder="AB12345678" />
        </el-form-item>
        <el-form-item label="發票日期" :error="fieldErrors.invoice_date">
          <el-date-picker
            v-model="invoiceForm.invoice_date"
            value-format="YYYY-MM-DD"
            style="width: 100%"
          />
        </el-form-item>
        <div class="hint">清空發票號碼即取消登錄。</div>
      </el-form>
      <template #footer>
        <el-button @click="invoiceVisible = false">取消</el-button>
        <el-button type="primary" :loading="invoiceSaving" @click="saveInvoice">儲存</el-button>
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
.neg {
  color: var(--el-color-danger);
  font-weight: 600;
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
