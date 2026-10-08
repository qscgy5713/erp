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
