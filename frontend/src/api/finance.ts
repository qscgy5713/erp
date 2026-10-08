import { qs, requestPage, type PageMeta } from './http'
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
