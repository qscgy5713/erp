import { http, qs, requestPage } from './http'
import type { DocAction, DocStatus, LotUsed } from './inventory'
import type { Decimal, LotControl } from './masterdata'

/** 替代料:1 單位主料可由 ratio 單位替代料取代 */
export interface BomSubstitute {
  item_id: number
  item_code?: string
  item_name?: string
  unit_name?: string
  ratio: Decimal
}

export interface BomLine {
  substitutes?: BomSubstitute[]
  line_no?: number
  item_id: number
  item_code?: string
  item_name?: string
  unit_name?: string
  qty: Decimal
  /** 損耗率(%) */
  scrap_pct: Decimal
  note: string
}

export interface Bom {
  id: number
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  yield_qty: Decimal
  /** 生效日 YYYY-MM-DD */
  effective_from: string
  is_active: boolean
  note: string
  lines: BomLine[]
  version: number
}

export interface BomRow {
  id: number
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  yield_qty: Decimal
  effective_from: string
  is_active: boolean
  line_count: number
}

export interface BomInput {
  item_id: number
  yield_qty: Decimal
  effective_from: string
  is_active: boolean
  note: string
  lines: {
    item_id: number
    qty: Decimal
    scrap_pct: Decimal
    note: string
    substitutes: { item_id: number; ratio: Decimal }[]
  }[]
  version?: number
}

export interface WorkOrderLine {
  line_no?: number
  item_id: number
  item_code?: string
  item_name?: string
  unit_name?: string
  qty: Decimal
  lot_no: string
  bin_code?: string
  note: string
  item_lot_control?: LotControl
  on_hand?: Decimal
  lots?: LotUsed[]
}

export interface WorkOrder {
  id: number
  doc_no: string
  doc_date: string
  status: DocStatus
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  item_lot_control: LotControl
  plan_qty: Decimal
  warehouse_id: number
  warehouse_name: string
  material_warehouse_id: number
  material_warehouse_name: string
  processing_cost: Decimal
  output_lot_no: string
  output_bin_code: string
  output_expiry_date: string | null
  due_date: string | null
  note: string
  created_by_name: string | null
  submitted_by_name: string | null
  submitted_at: string | null
  approved_by_name: string | null
  approved_at: string | null
  posted_by_name: string | null
  posted_at: string | null
  lines: WorkOrderLine[]
  output_lots: LotUsed[]
  /** 分批完工的累計有效完工量與各次完工回報(D94) */
  completed_qty: Decimal
  completions: Completion[]
  version: number
}

export interface CompletionLine {
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  qty: Decimal
  lot_no: string
  bin_code: string
  lots: LotUsed[]
}

export interface Completion {
  id: number
  doc_no: string
  doc_date: string
  qty: Decimal
  processing_cost: Decimal
  output_lot_no: string
  output_expiry_date: string | null
  output_bin_code: string
  note: string
  status: 'posted' | 'voided'
  created_by_name: string | null
  created_at: string
  voided_by_name: string | null
  voided_at: string | null
  lines: CompletionLine[]
  output_lots: LotUsed[]
}

export interface CompletionPreview {
  qty: Decimal
  remaining: Decimal
  processing_cost: Decimal
  lines: WorkOrderLine[]
}

export interface CompletionInput {
  doc_date: string
  qty: Decimal
  processing_cost: Decimal
  output_lot_no: string
  output_bin_code: string
  output_expiry_date: string | null
  note: string
  lines: { item_id: number; qty: Decimal; lot_no: string; bin_code: string }[]
}

export interface WorkOrderRow {
  id: number
  doc_no: string
  doc_date: string
  status: DocStatus
  item_code: string
  item_name: string
  unit_name: string
  plan_qty: Decimal
  completed_qty: Decimal
  due_date: string | null
  processing_cost: Decimal
  warehouse_name: string
  created_by_name: string | null
}

export interface WorkOrderInput {
  doc_date: string
  item_id: number
  plan_qty: Decimal
  warehouse_id: number
  material_warehouse_id: number
  processing_cost: Decimal
  output_lot_no: string
  output_bin_code: string
  output_expiry_date: string | null
  due_date: string | null
  note: string
  lines: { item_id: number; qty: Decimal; lot_no: string; bin_code: string; note: string }[]
  version?: number
}

export interface ExplodeLine {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  qty: Decimal
  /** 可改用的替代料,qty 已依替代比例換算 */
  substitutes?: ExplodeLine[]
}

type Q = Record<string, string | number | boolean | null | undefined>

export interface MrpRow {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  level: number
  sales_demand: Decimal
  wo_demand: Decimal
  plan_demand: Decimal
  safety_stock: Decimal
  on_hand: Decimal
  incoming_po: Decimal
  incoming_wo: Decimal
  net: Decimal
  action: 'make' | 'buy' | 'none'
  suggested_qty: Decimal
  lead_time_days: number
  min_order_qty: Decimal
  order_multiple: Decimal
  /** 最早需要補貨的週期開始日 */
  first_need: string | null
  /** 最晚下單日(最早的一筆) */
  release_by: string | null
  /** 前置時間來不及:即使今天下單也趕不上 */
  late: boolean
  buckets: MrpBucket[]
  substitutes: {
    item_id: number
    item_code: string
    item_name: string
    ratio: Decimal
    free_qty: Decimal
    covers: Decimal
  }[]
}

export interface MrpBucket {
  start: string
  gross: Decimal
  receipts: Decimal
  planned: Decimal
  projected: Decimal
}

export interface MrpParam {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  lead_time_days: number
  min_order_qty: Decimal
  order_multiple: Decimal
  has_bom: boolean
}

export interface MrpParamInput {
  lead_time_days: number
  min_order_qty: string
  order_multiple: string
}

// ---- 委外加工(D100) ----

export type ScStatus = 'open' | 'closed' | 'voided'
export type ScKind = 'issue' | 'receive' | 'return'

export interface ScLine {
  line_no?: number
  item_id: number | null
  item_code?: string
  item_name?: string
  unit_name?: string
  item_lot_control?: LotControl
  /** 預計用量 */
  qty: Decimal
  issued_qty?: Decimal
  consumed_qty?: Decimal
  /** 目前還在加工廠 */
  held_qty?: Decimal
  note: string
}

export interface ScTxnLine {
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  qty: Decimal
  lot_no: string
  bin_code: string
  lots: LotUsed[]
}

export interface ScTxn {
  id: number
  kind: ScKind
  doc_no: string
  doc_date: string
  qty: Decimal
  processing_fee: Decimal
  output_lot_no: string
  output_expiry_date: string | null
  output_bin_code: string
  note: string
  status: 'posted' | 'voided'
  created_by_name: string | null
  created_at: string
  voided_by_name: string | null
  voided_at: string | null
  lines: ScTxnLine[]
  output_lots: LotUsed[]
}

export interface SubcontractOrder {
  id: number
  doc_no: string
  doc_date: string
  status: ScStatus
  supplier_id: number
  supplier_code: string
  supplier_name: string
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  item_lot_control: LotControl
  plan_qty: Decimal
  received_qty: Decimal
  unit_fee: Decimal
  issue_warehouse_id: number
  issue_warehouse_name: string
  receive_warehouse_id: number
  receive_warehouse_name: string
  sc_warehouse_id: number
  sc_warehouse_name: string
  due_date: string | null
  note: string
  created_by_name: string | null
  lines: ScLine[]
  txns: ScTxn[]
  can_close: boolean
  version: number
}

export interface SubcontractRow {
  id: number
  doc_no: string
  doc_date: string
  status: ScStatus
  supplier_code: string
  supplier_name: string
  item_code: string
  item_name: string
  unit_name: string
  plan_qty: Decimal
  received_qty: Decimal
  unit_fee: Decimal
  due_date: string | null
}

export interface SubcontractInput {
  doc_date: string
  supplier_id: number
  item_id: number
  plan_qty: Decimal
  unit_fee: Decimal
  issue_warehouse_id: number
  receive_warehouse_id: number
  due_date: string | null
  note: string
  lines: { item_id: number; qty: Decimal; note: string }[]
  version?: number
}

export interface ScMaterialInput {
  doc_date: string
  note: string
  lines: { item_id: number; qty: Decimal; lot_no: string; bin_code: string }[]
}

export interface ScReceiveInput {
  doc_date: string
  qty: Decimal
  processing_fee: Decimal
  output_lot_no: string
  output_bin_code: string
  output_expiry_date: string | null
  note: string
  lines: { item_id: number; qty: Decimal; lot_no: string }[]
}

export interface ScReceivePreview {
  qty: Decimal
  remaining: Decimal
  processing_fee: Decimal
  lines: {
    item_id: number
    item_code: string
    item_name: string
    unit_name: string
    qty: Decimal
    held_qty: Decimal
  }[]
}

export const productionApi = {
  mrp: (shortageOnly = true, weeks = 8) =>
    http.get<MrpRow[]>(`/production/mrp${qs({ shortage_only: shortageOnly, weeks })}`),
  mrpParams: (q: Q) => requestPage<MrpParam>(`/production/mrp-params${qs(q)}`),
  setMrpParams: (item_id: number, input: MrpParamInput) =>
    http.put<unknown>(`/production/mrp-params/${item_id}`, input),
  boms: (q: Q) => requestPage<BomRow>(`/production/boms${qs(q)}`),
  bom: (id: number) => http.get<Bom>(`/production/boms/${id}`),
  createBom: (input: BomInput) => http.post<Bom>('/production/boms', input),
  updateBom: (id: number, input: BomInput) => http.put<Bom>(`/production/boms/${id}`, input),
  deleteBom: (id: number) => http.delete(`/production/boms/${id}`),

  orders: (q: Q) => requestPage<WorkOrderRow>(`/production/work-orders${qs(q)}`),
  order: (id: number) => http.get<WorkOrder>(`/production/work-orders/${id}`),
  createOrder: (input: WorkOrderInput) => http.post<WorkOrder>('/production/work-orders', input),
  updateOrder: (id: number, input: WorkOrderInput) =>
    http.put<WorkOrder>(`/production/work-orders/${id}`, input),
  action: (id: number, action: DocAction, version: number) =>
    http.post<WorkOrder>(`/production/work-orders/${id}/actions/${action}`, { version }),
  completionPreview: (id: number, qty?: string) =>
    http.get<CompletionPreview>(`/production/work-orders/${id}/completion-preview${qs({ qty })}`),
  complete: (id: number, input: CompletionInput) =>
    http.post<WorkOrder>(`/production/work-orders/${id}/completions`, input),
  voidCompletion: (id: number, completionId: number) =>
    http.post<WorkOrder>(`/production/work-orders/${id}/completions/${completionId}/void`),
  closeOrder: (id: number, version: number) =>
    http.post<WorkOrder>(`/production/work-orders/${id}/close`, { version }),
  explode: (item_id: number, qty: string, date?: string) =>
    http.get<ExplodeLine[]>(`/production/work-orders/explode${qs({ item_id, qty, date })}`),
  subcontracts: (q: Q) => requestPage<SubcontractRow>(`/production/subcontracts${qs(q)}`),
  subcontract: (id: number) => http.get<SubcontractOrder>(`/production/subcontracts/${id}`),
  createSubcontract: (input: SubcontractInput) =>
    http.post<SubcontractOrder>('/production/subcontracts', input),
  updateSubcontract: (id: number, input: SubcontractInput) =>
    http.put<SubcontractOrder>(`/production/subcontracts/${id}`, input),
  scMaterial: (id: number, kind: 'issue' | 'return', input: ScMaterialInput) =>
    http.post<SubcontractOrder>(`/production/subcontracts/${id}/${kind}`, input),
  scReceivePreview: (id: number, qty?: string) =>
    http.get<ScReceivePreview>(`/production/subcontracts/${id}/receive-preview${qs({ qty })}`),
  scReceive: (id: number, input: ScReceiveInput) =>
    http.post<SubcontractOrder>(`/production/subcontracts/${id}/receive`, input),
  scVoidTxn: (id: number, txnId: number) =>
    http.post<SubcontractOrder>(`/production/subcontracts/${id}/txns/${txnId}/void`),
  scClose: (id: number, version: number) =>
    http.post<SubcontractOrder>(`/production/subcontracts/${id}/close`, { version }),
  scVoid: (id: number, version: number) =>
    http.post<SubcontractOrder>(`/production/subcontracts/${id}/void`, { version }),
}
