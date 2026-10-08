import { http, qs, requestPage } from './http'
import type { DocAction, DocStatus, LotUsed } from './inventory'
import type { Decimal, LotControl } from './masterdata'

export type ReceiptDocType = 'receipt' | 'return'
export type ReceiptState = 'none' | 'partial' | 'full'

/** 採購單與進貨 / 退出單共用的單頭 */
interface PurchaseHeader {
  id: number
  doc_no: string
  doc_date: string
  supplier_id: number
  supplier_code: string
  supplier_name: string
  warehouse_id: number
  warehouse_name: string
  currency: string
  exchange_rate: Decimal
  tax_type_id: number
  tax_type_name: string
  tax_rate: Decimal
  payment_term_id: number | null
  payment_term_name: string | null
  untaxed_amount: Decimal
  tax_amount: Decimal
  total_amount: Decimal
  status: DocStatus
  note: string
  created_by_name: string | null
  submitted_by_name: string | null
  submitted_at: string | null
  approved_by_name: string | null
  approved_at: string | null
  version: number
  updated_at: string
}

export interface PurchaseLine {
  id: number
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  item_spec: string
  item_type: 'goods' | 'service'
  unit_id: number
  unit_name: string
  base_unit_name: string
  qty: Decimal
  factor: Decimal
  base_qty: Decimal
  unit_price: Decimal
  amount: Decimal
  note: string
}

export interface OrderLine extends PurchaseLine {
  received_qty: Decimal
  remaining_qty: Decimal
}

export interface PurchaseOrder extends PurchaseHeader {
  expected_date: string | null
  closed_by_name: string | null
  closed_at: string | null
  lines: OrderLine[]
}

export interface ReceiptLine extends PurchaseLine {
  item_lot_control: LotControl
  lot_no: string
  expiry_date: string | null
  lots?: LotUsed[]
  base_amount: Decimal
  po_line_id: number | null
  po_no: string | null
  receipt_line_id: number | null
  source_receipt_no: string | null
}

export interface GoodsReceipt extends PurchaseHeader {
  doc_type: ReceiptDocType
  invoice_no: string
  invoice_date: string | null
  invoice_kind: string
  base_untaxed: Decimal
  base_tax: Decimal
  base_total: Decimal
  posted_by_name: string | null
  posted_at: string | null
  lines: ReceiptLine[]
}

export interface OrderRow {
  id: number
  doc_no: string
  doc_date: string
  expected_date: string | null
  supplier_code: string
  supplier_name: string
  currency: string
  total_amount: Decimal
  status: DocStatus
  receipt_state: ReceiptState
  note: string
  created_by_name: string | null
}

export interface ReceiptRow {
  id: number
  doc_type: ReceiptDocType
  doc_no: string
  doc_date: string
  supplier_code: string
  supplier_name: string
  warehouse_name: string
  currency: string
  total_amount: Decimal
  invoice_no: string
  status: DocStatus
  note: string
  created_by_name: string | null
}

export interface OutstandingLine {
  po_line_id: number
  order_id: number
  doc_no: string
  doc_date: string
  expected_date: string | null
  supplier_id: number
  supplier_code: string
  supplier_name: string
  currency: string
  warehouse_id: number
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  item_spec: string
  unit_id: number
  unit_name: string
  qty: Decimal
  unit_price: Decimal
  received_qty: Decimal
  remaining_qty: Decimal
}

export interface ReturnableLine {
  receipt_line_id: number
  receipt_id: number
  doc_no: string
  doc_date: string
  warehouse_id: number
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  item_spec: string
  unit_id: number
  unit_name: string
  qty: Decimal
  unit_price: Decimal
  returned_qty: Decimal
  remaining_qty: Decimal
}

export interface SupplierOption {
  id: number
  code: string
  name: string
  short_name: string
  currency: string
  tax_type_id: number | null
  payment_term_id: number | null
}

type Q = Record<string, string | number | boolean | null | undefined>

export const purchaseApi = {
  orders: (q: Q) => requestPage<OrderRow>(`/purchase/orders${qs(q)}`),
  order: (id: number) => http.get<PurchaseOrder>(`/purchase/orders/${id}`),
  createOrder: (input: unknown) => http.post<PurchaseOrder>('/purchase/orders', input),
  updateOrder: (id: number, input: unknown) =>
    http.put<PurchaseOrder>(`/purchase/orders/${id}`, input),
  orderAction: (id: number, action: DocAction, version: number) =>
    http.post<PurchaseOrder>(`/purchase/orders/${id}/actions/${action}`, { version }),
  outstanding: (q: Q) => requestPage<OutstandingLine>(`/purchase/outstanding-lines${qs(q)}`),

  receipts: (q: Q) => requestPage<ReceiptRow>(`/purchase/receipts${qs(q)}`),
  receipt: (id: number) => http.get<GoodsReceipt>(`/purchase/receipts/${id}`),
  createReceipt: (input: unknown) => http.post<GoodsReceipt>('/purchase/receipts', input),
  updateReceipt: (id: number, input: unknown) =>
    http.put<GoodsReceipt>(`/purchase/receipts/${id}`, input),
  receiptAction: (id: number, action: DocAction, version: number) =>
    http.post<GoodsReceipt>(`/purchase/receipts/${id}/actions/${action}`, { version }),
  returnable: (q: { supplier_id: number; currency: string; keyword?: string }) =>
    http.get<ReturnableLine[]>(`/purchase/returnable-lines${qs(q)}`),

  supplierOptions: (keyword: string) =>
    http.get<SupplierOption[]>(`/masterdata/supplier-options${qs({ keyword })}`),
}
