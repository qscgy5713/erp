import { http, qs, requestPage } from './http'
import type { DocAction, DocStatus, LotUsed } from './inventory'
import type { Decimal, LotControl } from './masterdata'

export interface BomLine {
  line_no?: number
  item_id: number
  item_code?: string
  item_name?: string
  unit_name?: string
  qty: Decimal
  note: string
}

export interface Bom {
  id: number
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  yield_qty: Decimal
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
  is_active: boolean
  line_count: number
}

export interface BomInput {
  item_id: number
  yield_qty: Decimal
  is_active: boolean
  note: string
  lines: { item_id: number; qty: Decimal; note: string }[]
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
  version: number
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
}

type Q = Record<string, string | number | boolean | null | undefined>

export const productionApi = {
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
  explode: (item_id: number, qty: string) =>
    http.get<ExplodeLine[]>(`/production/work-orders/explode${qs({ item_id, qty })}`),
}
