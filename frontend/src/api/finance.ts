import { http, qs, requestPage, type PageMeta } from './http'
import type { DocAction, DocStatus } from './inventory'
import type { Decimal } from './masterdata'

export type LedgerKind = 'receivable' | 'payable'

/** 應收 / 應付帳款明細(往來對象為客戶或供應商) */
export interface LedgerEntry {
  id: number
  customer_id?: number
  customer_code?: string
  customer_name?: string
  supplier_id?: number
  supplier_code?: string
  supplier_name?: string
  source_type: 'goods_receipt' | 'purchase_return' | 'delivery' | 'sales_return'
  source_id: number
  source_no: string
  doc_date: string
  due_date: string
  currency: string
  exchange_rate: Decimal
  amount: Decimal
  base_amount: Decimal
  paid_amount: Decimal
  balance: Decimal
}

export const financeApi = {
  /** meta 另附篩選結果的本位幣合計 */
  ledger: (kind: LedgerKind, q: Record<string, string | number | boolean | null | undefined>) =>
    requestPage<LedgerEntry>(`/finance/${kind}s${qs(q)}`) as Promise<{
      items: LedgerEntry[]
      meta: PageMeta & { base_amount_sum?: Decimal }
    }>,
}

export type SettleSide = 'collection' | 'payment'
export type SettleMethod = 'cash' | 'transfer' | 'check' | 'other'

export const methodLabels: Record<SettleMethod, string> = {
  cash: '現金',
  transfer: '匯款',
  check: '支票',
  other: '其他',
}

export interface SettlementLine {
  id: number
  line_no: number
  target_id: number
  source_type: LedgerEntry['source_type']
  source_no: string
  source_date: string
  due_date: string
  source_amount: Decimal
  source_paid: Decimal
  balance: Decimal
  amount: Decimal
}

export interface Settlement {
  id: number
  side: 'receipt' | 'payment'
  doc_no: string
  doc_date: string
  partner_id: number
  partner_code: string
  partner_name: string
  sales_user_name: string | null
  currency: string
  method: SettleMethod
  reference: string
  amount: Decimal
  status: DocStatus
  note: string
  created_by_name: string | null
  submitted_by_name: string | null
  submitted_at: string | null
  approved_by_name: string | null
  approved_at: string | null
  posted_by_name: string | null
  posted_at: string | null
  lines: SettlementLine[]
  version: number
  updated_at: string
}

export interface SettlementRow {
  id: number
  doc_no: string
  doc_date: string
  partner_code: string
  partner_name: string
  sales_user_name: string | null
  currency: string
  method: SettleMethod
  reference: string
  amount: Decimal
  status: DocStatus
  note: string
  created_by_name: string | null
}

export interface SettlementInput {
  doc_date: string
  partner_id: number
  currency: string
  method: SettleMethod
  reference: string
  note: string
  lines: { target_id: number; amount: Decimal }[]
  version?: number
}

export interface StatementRow {
  date: string
  kind: LedgerEntry['source_type'] | 'receipt' | 'payment'
  doc_no: string
  ref_id: number
  delta: Decimal
  balance: Decimal
}

export interface Statement {
  partner_id: number
  partner_code: string
  partner_name: string
  currency: string
  from: string
  to: string
  opening: Decimal
  closing: Decimal
  rows: StatementRow[]
}

export interface AgingRow {
  partner_id: number
  partner_code: string
  partner_name: string
  not_due: Decimal
  d1_30: Decimal
  d31_60: Decimal
  d61_90: Decimal
  d90_plus: Decimal
  total: Decimal
}

type Q = Record<string, string | number | boolean | null | undefined>

export const settlementApi = {
  list: (side: SettleSide, q: Q) => requestPage<SettlementRow>(`/finance/${side}s${qs(q)}`),
  get: (side: SettleSide, id: number) => http.get<Settlement>(`/finance/${side}s/${id}`),
  create: (side: SettleSide, input: SettlementInput) =>
    http.post<Settlement>(`/finance/${side}s`, input),
  update: (side: SettleSide, id: number, input: SettlementInput) =>
    http.put<Settlement>(`/finance/${side}s/${id}`, input),
  action: (side: SettleSide, id: number, action: DocAction, version: number) =>
    http.post<Settlement>(`/finance/${side}s/${id}/actions/${action}`, { version }),
  statement: (q: Q) => http.get<Statement>(`/finance/statements${qs(q)}`),
  aging: (side: LedgerKind, asOf?: string) =>
    http.get<{ as_of: string; rows: AgingRow[] }>(`/finance/aging${qs({ side, as_of: asOf })}`),
}
