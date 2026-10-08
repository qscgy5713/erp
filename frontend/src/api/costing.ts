import { http, qs, requestPage } from './http'
import type { Decimal } from './masterdata'

export interface Closing {
  period: string
  status: 'costed' | 'pending'
  item_count: number
  cogs_amount: Decimal
  adjust_amount: Decimal
  inventory_value: Decimal
  closed_by_name: string | null
  closed_at: string | null
}

export interface ItemCost {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  opening_qty: Decimal
  opening_value: Decimal
  purchase_qty: Decimal
  purchase_value: Decimal
  sales_qty: Decimal
  adjust_qty: Decimal
  /** 工單領料(負數)與金額:扣庫存,但不是銷貨成本 */
  consume_qty: Decimal
  consume_value: Decimal
  avg_cost: Decimal
  cogs_amount: Decimal
  adjust_amount: Decimal
  closing_qty: Decimal
  closing_value: Decimal
}

export interface Check {
  key: string
  label: string
  status: 'ok' | 'error' | 'warn'
  subledger: Decimal | null
  gl: Decimal | null
  diff: Decimal | null
  message: string
}

export interface Reconcile {
  checked_at: string
  ok: boolean
  checks: Check[]
}

export const costingApi = {
  closings: () => http.get<Closing[]>('/costing/closings'),
  items: (period: string, q: { keyword?: string; page: number; size: number }) =>
    requestPage<ItemCost>(`/costing/closings/${period}/items${qs(q)}`),
  run: (period: string) => http.post<Closing>(`/costing/closings/${period}/run`),
  cancel: (period: string) => http.post<unknown>(`/costing/closings/${period}/cancel`),
  reconcile: () => http.get<Reconcile>('/costing/reconcile'),
}
