import { qs, requestPage, type PageMeta } from './http'
import type { Decimal } from './masterdata'

export interface Payable {
  id: number
  supplier_id: number
  supplier_code: string
  supplier_name: string
  source_type: 'goods_receipt' | 'purchase_return'
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
  payables: (q: Record<string, string | number | boolean | null | undefined>) =>
    requestPage<Payable>(`/finance/payables${qs(q)}`) as Promise<{
      items: Payable[]
      meta: PageMeta & { base_amount_sum?: Decimal }
    }>,
}
