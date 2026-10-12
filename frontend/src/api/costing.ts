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

export interface StandardCost {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  standard_cost: Decimal | null
  updated_at: string | null
  has_bom: boolean
}

export interface VarianceRow {
  item_id: number
  item_code: string
  item_name: string
  unit_name: string
  produced_qty: Decimal
  actual_amount: Decimal
  actual_unit: Decimal | null
  standard_unit: Decimal | null
  standard_amount: Decimal | null
  /** 實際 − 標準;正數 = 實際高於標準(不利差異) */
  variance: Decimal | null
  variance_pct: Decimal | null
  status: 'ok' | 'uncosted' | 'no_standard'
}

export const costingApi = {
  standardCosts: (q: { keyword?: string; only_bom?: boolean; page: number; size: number }) =>
    requestPage<StandardCost>(`/costing/standard-costs${qs(q)}`),
  setStandardCost: (item_id: number, standard_cost: string | null) =>
    http.put<unknown>(`/costing/standard-costs/${item_id}`, { standard_cost }),
  variance: (period: string) =>
    http.get<{ period: string; rows: VarianceRow[] }>(`/costing/variance${qs({ period })}`),
  closings: () => http.get<Closing[]>('/costing/closings'),
  items: (period: string, q: { keyword?: string; page: number; size: number }) =>
    requestPage<ItemCost>(`/costing/closings/${period}/items${qs(q)}`),
  run: (period: string) => http.post<Closing>(`/costing/closings/${period}/run`),
  cancel: (period: string) => http.post<unknown>(`/costing/closings/${period}/cancel`),
  reconcile: () => http.get<Reconcile>('/costing/reconcile'),
}
