import { download, http, qs, requestPage, upload } from './http'
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
  /** 報表自我核對的警示(例如現金流量表與現金科目餘額不一致) */
  note?: string
  lines: StmtLine[]
}

export interface VoucherAttachment {
  id: number
  filename: string
  content_type: string
  size_bytes: number
  uploaded_at: string
  uploaded_by_name: string | null
}

export interface Project {
  id: number
  /** 專案不做樂觀鎖,僅為符合 useFormDialog 的型別;後端不回傳也不檢查 */
  version: number
  code: string
  name: string
  is_active: boolean
}

export interface DeptPnlRow {
  department_id: number | null
  code: string
  name: string
  revenue: Decimal
  cost: Decimal
  expense: Decimal
  profit: Decimal
}

export interface DeptPnl {
  from: string
  to: string
  rows: DeptPnlRow[]
  total: DeptPnlRow
}

export interface VatSummary {
  taxable_triplicate: Decimal
  taxable_duplicate: Decimal
  taxable_sales: Decimal
  zero_sales: Decimal
  exempt_sales: Decimal
  total_sales: Decimal
  output_tax: Decimal
  deductible_goods: Decimal
  deductible_expense: Decimal
  input_tax: Decimal
  non_tax_purchase: Decimal
  net_tax: Decimal
}

export interface VatDetail {
  doc_no: string
  doc_type: string
  date: string
  invoice_no: string
  tax_kind: 'taxable' | 'zero' | 'exempt'
  partner: string
  partner_tax_id: string
  untaxed: Decimal
  tax: Decimal
}

export interface VatIssue {
  kind: 'sales_no_invoice' | 'purchase_no_invoice'
  doc_no: string
  date: string
  partner: string
  untaxed: Decimal
  tax: Decimal
}

export interface VatReport {
  year: number
  period: number
  from: string
  to: string
  summary: VatSummary
  issues: VatIssue[]
  sales: VatDetail[]
  purchases: VatDetail[]
  settings: VatSettings
  result: VatResult
  suggested_prior_credit: Decimal
}

export interface VatSettings {
  prior_credit: Decimal
  customs_tax: Decimal
  use_ratio: boolean
}

export interface VatResult {
  deduct_ratio: Decimal
  input_tax: Decimal
  customs_tax: Decimal
  deductible_tax: Decimal
  prior_credit: Decimal
  tax_payable: Decimal
  carry_credit: Decimal
}

export interface MediaExcluded {
  side: 'sales' | 'purchase'
  doc_no: string
  reason: string
  untaxed: Decimal
  tax: Decimal
}

export interface MediaPreview {
  file_name: string
  count: number
  totals: {
    sales_count: number
    sales_amount: Decimal
    sales_tax: Decimal
    purchase_count: number
    purchase_amount: Decimal
    purchase_tax: Decimal
  }
  excluded: MediaExcluded[]
  covered: string
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
  fx_reval: '外幣重估',
  fx_reval_rev: '外幣重估沖回',
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
  project_id: number | null
  project_name?: string | null
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
    project_id: number | null
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

export interface RevalRow {
  currency: string
  rate: Decimal
  ar_foreign: Decimal
  ar_carrying: Decimal
  ar_revalued: Decimal
  ap_foreign: Decimal
  ap_carrying: Decimal
  ap_revalued: Decimal
}

export interface RevalHistory {
  id: number
  period: string
  status: 'active' | 'undone'
  ar_diff: Decimal
  ap_diff: Decimal
  voucher_id: number | null
}

export interface RevalPreview {
  period?: string
  as_of?: string
  rows?: RevalRow[]
  ar_diff?: Decimal
  ap_diff?: Decimal
  net_gain?: Decimal
  history: RevalHistory[]
}

export const glApi = {
  revaluation: (period?: string) => http.get<RevalPreview>(`/gl/revaluation${qs({ period })}`),
  runRevaluation: (period: string) => http.post<RevalHistory>(`/gl/revaluation/${period}`, {}),
  undoRevaluation: (period: string) => http.post<void>(`/gl/revaluation/${period}/undo`, {}),
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
  attachments: (id: number) => http.get<VoucherAttachment[]>(`/gl/vouchers/${id}/attachments`),
  uploadAttachment: (id: number, file: File) =>
    upload<VoucherAttachment>(`/gl/vouchers/${id}/attachments`, file),
  deleteAttachment: (id: number, aid: number) =>
    http.delete<unknown>(`/gl/vouchers/${id}/attachments/${aid}`),
  downloadAttachment: (id: number, a: VoucherAttachment) =>
    download(`/gl/vouchers/${id}/attachments/${a.id}`, a.filename),
  cashFlow: (from: string, to: string, compare: boolean) =>
    http.get<Statement>(`/gl/reports/cash-flow${qs({ from, to, compare })}`),
  projects: () => http.get<Project[]>('/gl/projects'),
  createProject: (input: { code: string; name: string }) =>
    http.post<Project>('/gl/projects', input),
  updateProject: (id: number, input: { code: string; name: string; is_active: boolean }) =>
    http.put<Project>(`/gl/projects/${id}`, input),
  projectPnl: (from: string, to: string) =>
    http.get<DeptPnl>(`/gl/reports/project-pnl${qs({ from, to })}`),
  departmentPnl: (from: string, to: string) =>
    http.get<DeptPnl>(`/gl/reports/department-pnl${qs({ from, to })}`),
  balanceSheet: (as_of: string, compare: boolean) =>
    http.get<Statement>(`/gl/reports/balance-sheet${qs({ as_of, compare })}`),
  exportStatement: (
    kind: 'income-statement' | 'balance-sheet' | 'cash-flow',
    params: Record<string, string | boolean>,
    name: string,
  ) => download(`/gl/reports/${kind}${qs({ ...params, format: 'xlsx' })}`, name),
  vat401: (year: number, period: number) =>
    http.get<VatReport>(`/gl/reports/vat401${qs({ year, period })}`),
  saveVatSettings: (year: number, period: number, input: VatSettings) =>
    http.put<unknown>(`/gl/reports/vat401/settings${qs({ year, period })}`, input),
  vat401Media: (year: number, period: number) =>
    http.get<MediaPreview>(`/gl/reports/vat401/media${qs({ year, period })}`),
  downloadVat401Media: (year: number, period: number, fileName: string) =>
    download(`/gl/reports/vat401/media${qs({ year, period, format: 'txt' })}`, fileName),
  exportVat401: (year: number, period: number) =>
    download(
      `/gl/reports/vat401${qs({ year, period, format: 'xlsx' })}`,
      `營業稅401_${year}年第${period}期.xlsx`,
    ),
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
