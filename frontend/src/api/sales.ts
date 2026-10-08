import { http, qs, requestPage } from './http'
import type { DocAction, DocStatus, LotUsed } from './inventory'
import type { Decimal, LotControl } from './masterdata'

export type SalesOrderType = 'quotation' | 'order'
export type DeliveryDocType = 'delivery' | 'return'
export type ShipState = 'none' | 'partial' | 'full'

interface SalesHeader {
  id: number
  doc_no: string
  doc_date: string
  customer_id: number
  customer_code: string
  customer_name: string
  sales_user_id: number | null
  sales_user_name: string | null
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

interface SalesLine {
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

export interface SalesOrderLine extends SalesLine {
  delivered_qty: Decimal
  remaining_qty: Decimal
}

export interface SalesOrder extends SalesHeader {
  doc_type: SalesOrderType
  quotation_id: number | null
  quotation_no: string | null
  valid_until: string | null
  delivery_date: string | null
  customer_po_no: string
  closed_by_name: string | null
  closed_at: string | null
  lines: SalesOrderLine[]
}

export interface DeliveryLine extends SalesLine {
  item_lot_control: LotControl
  lot_no: string
  expiry_date: string | null
  bin_code: string
  lots?: LotUsed[]
  base_amount: Decimal
  so_line_id: number | null
  so_no: string | null
  delivery_line_id: number | null
  source_delivery_no: string | null
}

export interface Delivery extends SalesHeader {
  doc_type: DeliveryDocType
  invoice_no: string
  invoice_date: string | null
  base_untaxed: Decimal
  base_tax: Decimal
  base_total: Decimal
  posted_by_name: string | null
  posted_at: string | null
  lines: DeliveryLine[]
}

export interface SalesOrderRow {
  id: number
  doc_type: SalesOrderType
  doc_no: string
  doc_date: string
  valid_until: string | null
  delivery_date: string | null
  customer_po_no: string
  customer_code: string
  customer_name: string
  sales_user_name: string | null
  currency: string
  total_amount: Decimal
  status: DocStatus
  ship_state: ShipState
  converted: boolean
  note: string
  created_by_name: string | null
}

export interface DeliveryRow {
  id: number
  doc_type: DeliveryDocType
  doc_no: string
  doc_date: string
  customer_code: string
  customer_name: string
  sales_user_name: string | null
  warehouse_name: string
  currency: string
  total_amount: Decimal
  invoice_no: string
  status: DocStatus
  note: string
  created_by_name: string | null
}

export interface UnshippedLine {
  so_line_id: number
  order_id: number
  doc_no: string
  doc_date: string
  delivery_date: string | null
  customer_id: number
  customer_code: string
  customer_name: string
  customer_po_no: string
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
  delivered_qty: Decimal
  remaining_qty: Decimal
}

export interface ReturnableDeliveryLine {
  delivery_line_id: number
  delivery_id: number
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

export interface Availability {
  item_id: number
  on_hand: Decimal
  reserved: Decimal
  available: Decimal
}

export interface CustomerOption {
  id: number
  code: string
  name: string
  short_name: string
  currency: string
  tax_type_id: number | null
  payment_term_id: number | null
  sales_user_id: number | null
  sales_user_name: string | null
}

type Q = Record<string, string | number | boolean | null | undefined>

export const salesApi = {
  orders: (q: Q) => requestPage<SalesOrderRow>(`/sales/orders${qs(q)}`),
  order: (id: number) => http.get<SalesOrder>(`/sales/orders/${id}`),
  createOrder: (input: unknown) => http.post<SalesOrder>('/sales/orders', input),
  updateOrder: (id: number, input: unknown) => http.put<SalesOrder>(`/sales/orders/${id}`, input),
  orderAction: (id: number, action: DocAction, version: number) =>
    http.post<SalesOrder>(`/sales/orders/${id}/actions/${action}`, { version }),
  unshipped: (q: Q) => requestPage<UnshippedLine>(`/sales/unshipped-lines${qs(q)}`),
  availability: (warehouseId: number, itemIds: number[], excludeOrderId?: number) =>
    http.get<Availability[]>(
      `/sales/availability${qs({
        warehouse_id: warehouseId,
        item_ids: itemIds.join(','),
        exclude_order_id: excludeOrderId,
      })}`,
    ),

  deliveries: (q: Q) => requestPage<DeliveryRow>(`/sales/deliveries${qs(q)}`),
  delivery: (id: number) => http.get<Delivery>(`/sales/deliveries/${id}`),
  createDelivery: (input: unknown) => http.post<Delivery>('/sales/deliveries', input),
  updateDelivery: (id: number, input: unknown) =>
    http.put<Delivery>(`/sales/deliveries/${id}`, input),
  deliveryAction: (id: number, action: DocAction, version: number) =>
    http.post<Delivery>(`/sales/deliveries/${id}/actions/${action}`, { version }),
  setInvoice: (
    id: number,
    input: { invoice_no: string; invoice_date: string | null; version: number },
  ) => http.put<Delivery>(`/sales/deliveries/${id}/invoice`, input),
  returnable: (q: { customer_id: number; currency: string; keyword?: string }) =>
    http.get<ReturnableDeliveryLine[]>(`/sales/returnable-lines${qs(q)}`),

  customerOptions: (keyword: string) =>
    http.get<CustomerOption[]>(`/masterdata/customer-options${qs({ keyword })}`),
}
