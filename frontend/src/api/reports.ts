import { download, http, qs, requestPage } from './http'
import type { Decimal } from './masterdata'

export type ReportSide = 'sales' | 'purchase'
export type ReportGroup = 'party' | 'item' | 'detail'

/** 排行列(銷售 / 採購共用;毛利欄位只有銷售端有值) */
export interface ReportRow {
  id: number
  code: string
  name: string
  unit_name?: string
  docs?: number
  delivery_amount?: Decimal
  receipt_amount?: Decimal
  return_amount: Decimal
  net_amount: Decimal
  share_pct: Decimal | null
  avg_price?: Decimal | null
  costed_amount?: Decimal
  cost_amount?: Decimal
  margin?: Decimal
  margin_pct?: Decimal | null
}

export interface ReportTotals {
  delivery_amount?: Decimal
  receipt_amount?: Decimal
  return_amount: Decimal
  net_amount: Decimal
  costed_amount?: Decimal
  cost_amount?: Decimal
  margin?: Decimal
  margin_pct?: Decimal | null
  uncosted_amount?: Decimal
}

export interface ReportResult {
  group: string
  from: string
  to: string
  rows: ReportRow[]
  totals: ReportTotals
}

export interface ReportDetailRow {
  doc_id: number
  doc_no: string
  doc_type: string
  date: string
  customer_code?: string
  customer_name?: string
  supplier_code?: string
  supplier_name?: string
  item_code: string
  item_name: string
  unit_name: string
  qty: Decimal
  amount: Decimal
  cost?: Decimal | null
  margin?: Decimal | null
}

type Q = Record<string, string | number | boolean | null | undefined>

const groupParam = (side: ReportSide, g: ReportGroup) =>
  g === 'party' ? (side === 'sales' ? 'customer' : 'supplier') : g

export const reportApi = {
  summary: (side: ReportSide, g: Exclude<ReportGroup, 'detail'>, q: Q) =>
    http.get<ReportResult>(`/${side}/reports${qs({ ...q, group: groupParam(side, g) })}`),
  detail: (side: ReportSide, q: Q) =>
    requestPage<ReportDetailRow>(`/${side}/reports${qs({ ...q, group: 'detail' })}`),
  exportXlsx: (side: ReportSide, g: ReportGroup, q: Q, name: string) =>
    download(`/${side}/reports${qs({ ...q, group: groupParam(side, g), format: 'xlsx' })}`, name),
}
