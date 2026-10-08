import { http, qs, requestPage } from './http'
import type { Decimal, ItemUnit } from './masterdata'

export type DocStatus = 'draft' | 'pending' | 'approved' | 'posted' | 'closed' | 'voided'
export type DocAction =
  'submit' | 'reject' | 'approve' | 'unapprove' | 'post' | 'unpost' | 'void' | 'close' | 'reopen'
export type StockDocType = 'adjustment' | 'transfer' | 'count'

export interface StockLine {
  id?: number
  line_no?: number
  item_id: number
  item_code?: string
  item_name?: string
  item_spec?: string
  unit_id: number
  unit_name?: string
  base_unit_name?: string
  qty: Decimal | null
  factor?: Decimal
  base_qty?: Decimal | null
  system_qty?: Decimal | null
  diff_qty?: Decimal | null
  note: string
}

export interface StockDocument {
  id: number
  doc_type: StockDocType
  doc_no: string
  doc_date: string
  warehouse_id: number
  warehouse_name: string
  to_warehouse_id: number | null
  to_warehouse_name: string | null
  category_id: number | null
  category_name: string | null
  status: DocStatus
  note: string
  created_by_name: string | null
  submitted_by_name: string | null
  submitted_at: string | null
  approved_by_name: string | null
  approved_at: string | null
  posted_by_name: string | null
  posted_at: string | null
  lines: StockLine[]
  version: number
  updated_at: string
}

export interface StockDocumentRow {
  id: number
  doc_type: StockDocType
  doc_no: string
  doc_date: string
  warehouse_name: string
  to_warehouse_name: string | null
  status: DocStatus
  note: string
  line_count: number
  created_by_name: string | null
  updated_at: string
}

export interface StockDocumentInput {
  doc_type: StockDocType
  doc_date: string
  warehouse_id: number
  to_warehouse_id: number | null
  category_id: number | null
  note: string
  lines: Pick<StockLine, 'item_id' | 'unit_id' | 'qty' | 'note'>[]
  version?: number
}

export interface Balance {
  item_id: number
  item_code: string
  item_name: string
  item_spec: string
  unit_name: string
  warehouse_id: number
  warehouse_code: string
  warehouse_name: string
  qty: Decimal
  item_total: Decimal
  safety_stock: Decimal
  below_safety: boolean
  updated_at: string
}

export interface MovementSummary {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  opening_qty: Decimal
  in_qty: Decimal
  out_qty: Decimal
  closing_qty: Decimal
}

export interface LedgerEntry {
  id: number
  doc_date: string
  warehouse_code: string
  warehouse_name: string
  source_type: string
  source_id: number
  source_no: string
  is_reversal: boolean
  qty: Decimal
  balance_qty: Decimal
}

export interface ItemOption {
  id: number
  code: string
  name: string
  spec: string
  item_type: 'goods' | 'service'
  base_unit_id: number
  base_unit_name: string
  units: ItemUnit[]
}

type Q = Record<string, string | number | boolean | null | undefined>

export const inventoryApi = {
  documents: (q: Q) => requestPage<StockDocumentRow>(`/inventory/documents${qs(q)}`),
  document: (id: number) => http.get<StockDocument>(`/inventory/documents/${id}`),
  create: (input: StockDocumentInput) => http.post<StockDocument>('/inventory/documents', input),
  update: (id: number, input: StockDocumentInput) =>
    http.put<StockDocument>(`/inventory/documents/${id}`, input),
  action: (id: number, action: DocAction, version: number) =>
    http.post<StockDocument>(`/inventory/documents/${id}/actions/${action}`, { version }),

  balances: (q: Q) => requestPage<Balance>(`/inventory/balances${qs(q)}`),
  movementSummary: (q: Q) => requestPage<MovementSummary>(`/inventory/movement-summary${qs(q)}`),
  itemLedger: (itemId: number, q: Q) =>
    http.get<{ opening_qty: Decimal; entries: LedgerEntry[] }>(
      `/inventory/items/${itemId}/ledger${qs(q)}`,
    ),

  itemOptions: (keyword: string, itemType?: string) =>
    http.get<ItemOption[]>(`/masterdata/item-options${qs({ keyword, item_type: itemType })}`),
}
