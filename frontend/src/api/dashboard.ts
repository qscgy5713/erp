import { http } from './http'
import type { Decimal } from './masterdata'

export interface Dashboard {
  date: string
  sales?: {
    today: Decimal
    today_count: number
    month: Decimal
    month_count: number
    daily: { date: string; amount: Decimal }[]
    top_customers: { id: number; code: string; name: string; amount: Decimal }[]
    scope_label: string
  }
  pending: { key: string; label: string; count: number; path: string }[]
  low_stock?: {
    count: number
    items: {
      id: number
      code: string
      name: string
      unit_name: string
      safety_stock: Decimal
      total: Decimal
    }[]
  }
  /** 效期警示:有庫存的批號中已過期與即將到期的數量,以及最早到期的幾個 */
  expiry?: {
    expired: number
    expiring: number
    days: number
    items: {
      lot_id: number
      lot_no: string
      expiry_date: string
      item_code: string
      item_name: string
      qty: Decimal
      expired: boolean
    }[]
  }
  /** 呆滯料:有庫存但超過 days 天沒有任何異動 */
  stagnant?: {
    count: number
    days: number
    items: {
      id: number
      code: string
      name: string
      unit_name: string
      total: Decimal
      last_move: string
    }[]
  }
  /** 最近一次月結的經營指標(需月結權限) */
  performance?: {
    period: string
    sales: Decimal
    cogs: Decimal
    margin: Decimal
    margin_pct: Decimal | null
    inventory_value: Decimal
    turnover: Decimal | null
    turnover_days: Decimal | null
  }
  receivable?: { open_amount: Decimal; overdue_amount: Decimal; overdue_count: number }
  payable?: {
    open_amount: Decimal
    overdue_amount: Decimal
    overdue_count: number
    due_soon_amount: Decimal
  }
  costing?: { last_closing: string; current_month: string }
}

export const dashboardApi = {
  get: () => http.get<Dashboard>('/dashboard'),
}
