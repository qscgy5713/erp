import { download, http, qs, requestPage } from './http'
import type { Decimal } from './masterdata'

export type AcctType = 'asset' | 'liability' | 'equity' | 'revenue' | 'cost' | 'expense'

export const acctTypeLabels: Record<AcctType, string> = {
  asset: '資產',
  liability: '負債',
  equity: '權益',
  revenue: '收入',
  cost: '成本',
  expense: '費用',
}

export interface Account {
  id: number
  code: string
  name: string
  acct_type: AcctType
  parent_id: number | null
  parent_code: string | null
  is_postable: boolean
  is_active: boolean
  note: string
  has_entries: boolean
  version: number
}

export type AccountInput = Omit<Account, 'id' | 'parent_code' | 'has_entries' | 'version'> & {
  version?: number
}

export interface AccountOption {
  id: number
  code: string
  name: string
  acct_type: AcctType
}

export interface Mapping {
  key: string
  label: string
  account_id: number
  account_code: string
  account_name: string
}

export type VoucherStatus = 'draft' | 'posted' | 'voided'

export interface StmtLine {
  kind: 'header' | 'account' | 'total'
  level: number
  code?: string
  label: string
  amount: Decimal
  prev: Decimal
}

export interface Statement {
  title: string
  from?: string
  to: string
  compare: boolean
  prev_from?: string
  prev_to?: string
  balanced?: boolean
  lines: StmtLine[]
}

export interface YearEnd {
  year: number
  status: 'closed' | 'open' | 'not_ended'
  voucher_no?: string
  profit: Decimal
}

export const sourceLabels: Record<string, string> = {
  manual: '手動',
  goods_receipt: '進貨',
  purchase_return: '進貨退出',
  delivery: '出貨',
  sales_return: '銷貨退回',
  collection: '收款',
  payment: '付款',
  cost_closing: '月結成本',
  year_end: '年度結帳',
  opening_balance: '期初科目餘額',
}

export interface VoucherLine {
  line_no?: number
  account_id: number | null
  account_code?: string
  account_name?: string
  debit: Decimal
  credit: Decimal
  description: string
  customer_id: number | null
  customer_name?: string | null
  supplier_id: number | null
  supplier_name?: string | null
  department_id: number | null
  department_name?: string | null
}

export interface Voucher {
  id: number
  doc_no: string
  voucher_date: string
  source_type: string
  source_id: number | null
  source_no: string
  description: string
  status: VoucherStatus
  reversal_of: number | null
  reversal_of_no: string | null
  reversed_by_no: string | null
  total_amount: Decimal
  created_by_name: string | null
  posted_by_name: string | null
  posted_at: string | null
  lines: VoucherLine[]
  version: number
}

export interface VoucherRow {
  id: number
  doc_no: string
  voucher_date: string
  source_type: string
  source_id: number | null
  source_no: string
  description: string
  status: VoucherStatus
  reversal_of: number | null
  reversed: boolean
  total_amount: Decimal
  created_by_name: string | null
  version: number
}

export interface VoucherInput {
  voucher_date: string
  description: string
  lines: {
    account_id: number
    debit: Decimal
    credit: Decimal
    description: string
    customer_id: number | null
    supplier_id: number | null
    department_id: number | null
  }[]
  version?: number
}

export interface Period {
  period: string
  status: 'open' | 'closed'
  closed_at: string | null
  closed_by_name: string | null
}

export interface TrialRow {
  account_id: number
  code: string
  name: string
  acct_type: AcctType
  opening: Decimal
  period_debit: Decimal
  period_credit: Decimal
  closing: Decimal
}

export interface TrialBalance {
  rows: TrialRow[]
  total_opening: Decimal
  total_debit: Decimal
  total_credit: Decimal
  balanced: boolean
}

export interface LedgerRow {
  voucher_id: number
  doc_no: string
  date: string
  source_type: string
  source_no: string
  description: string
  debit: Decimal
  credit: Decimal
  balance: Decimal
}

export interface GeneralLedger {
  account_code: string
  account_name: string
  opening: Decimal
  total_debit: Decimal
  total_credit: Decimal
  closing: Decimal
  rows: LedgerRow[]
}

export interface JournalRow {
  voucher_id: number
  doc_no: string
  date: string
  source_type: string
  source_no: string
  voucher_description: string
  line_no: number
  account_code: string
  account_name: string
  debit: Decimal
  credit: Decimal
  description: string
}

type Q = Record<string, string | number | boolean | null | undefined>

export const glApi = {
  accounts: (q: Q = {}) => http.get<Account[]>(`/gl/accounts${qs(q)}`),
  accountOptions: (keyword = '') =>
    http.get<AccountOption[]>(`/gl/account-options${qs({ keyword })}`),
  createAccount: (input: AccountInput) => http.post<Account>('/gl/accounts', input),
  updateAccount: (id: number, input: AccountInput) =>
    http.put<Account>(`/gl/accounts/${id}`, input),
  mappings: () => http.get<Mapping[]>('/gl/mappings'),
  setMapping: (key: string, account_id: number) =>
    http.put<unknown>(`/gl/mappings/${key}`, { account_id }),

  vouchers: (q: Q) => requestPage<VoucherRow>(`/gl/vouchers${qs(q)}`),
  voucher: (id: number) => http.get<Voucher>(`/gl/vouchers/${id}`),
  createVoucher: (input: VoucherInput) => http.post<Voucher>('/gl/vouchers', input),
  updateVoucher: (id: number, input: VoucherInput) =>
    http.put<Voucher>(`/gl/vouchers/${id}`, input),
  voucherAction: (
    id: number,
    action: 'post' | 'void' | 'reverse',
    version: number,
    date?: string,
  ) => http.post<Voucher>(`/gl/vouchers/${id}/actions/${action}`, { version, date }),

  incomeStatement: (from: string, to: string, compare: boolean) =>
    http.get<Statement>(`/gl/reports/income-statement${qs({ from, to, compare })}`),
  balanceSheet: (as_of: string, compare: boolean) =>
    http.get<Statement>(`/gl/reports/balance-sheet${qs({ as_of, compare })}`),
  exportStatement: (
    kind: 'income-statement' | 'balance-sheet',
    params: Record<string, string | boolean>,
    name: string,
  ) => download(`/gl/reports/${kind}${qs({ ...params, format: 'xlsx' })}`, name),
  years: () => http.get<YearEnd[]>('/gl/year-end'),
  yearEnd: (year: number, action: 'close' | 'undo') =>
    http.post<unknown>(`/gl/year-end/${year}/${action}`),

  periods: () => http.get<Period[]>('/gl/periods'),
  changePeriod: (period: string, action: 'close' | 'reopen') =>
    http.post<unknown>(`/gl/periods/${period}/${action}`),

  trialBalance: (from: string, to: string) =>
    http.get<TrialBalance>(`/gl/reports/trial-balance${qs({ from, to })}`),
  ledger: (account_id: number, from: string, to: string) =>
    http.get<GeneralLedger>(`/gl/reports/ledger${qs({ account_id, from, to })}`),
  journal: (q: Q) => requestPage<JournalRow>(`/gl/reports/journal${qs(q)}`),
}
