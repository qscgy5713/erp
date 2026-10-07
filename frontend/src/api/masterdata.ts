import { http, qs, requestPage } from './http'

// 金額、數量、匯率一律以字串傳遞,避免 JavaScript 浮點誤差
export type Decimal = string

export interface Currency {
  code: string
  name: string
  symbol: string
  decimals: number
  is_active: boolean
  is_base: boolean
}

export interface ExchangeRate {
  id: number
  currency: string
  rate_date: string
  rate: Decimal
  version: number
  updated_at: string
}

export interface TaxType {
  id: number
  code: string
  name: string
  kind: 'taxable' | 'zero' | 'exempt'
  rate: Decimal
  is_active: boolean
  version: number
}

export interface PaymentTerm {
  id: number
  code: string
  name: string
  is_month_end: boolean
  net_days: number
  is_active: boolean
  version: number
}

export interface Unit {
  id: number
  code: string
  name: string
  is_active: boolean
  version: number
}

export interface ItemCategory {
  id: number
  parent_id: number | null
  code: string
  name: string
  sort_order: number
  is_active: boolean
  item_count: number
  version: number
}

export interface Warehouse {
  id: number
  code: string
  name: string
  address: string
  allow_negative: boolean
  is_active: boolean
  version: number
}

export interface ItemUnit {
  unit_id: number
  unit_code?: string
  unit_name?: string
  factor: Decimal
  barcode: string | null
}

export interface Item {
  id: number
  code: string
  name: string
  spec: string
  category_id: number | null
  category_name?: string | null
  item_type: 'goods' | 'service'
  base_unit_id: number
  base_unit_name?: string
  barcode: string | null
  tax_type_id: number | null
  default_warehouse_id: number | null
  safety_stock: Decimal
  list_price: Decimal
  note: string
  is_active: boolean
  units: ItemUnit[]
  version: number
  updated_at: string
}

export interface Contact {
  name: string
  title: string
  phone: string
  email: string
}

export interface Address {
  label: string
  zip: string
  address: string
  is_default: boolean
}

interface PartnerBase {
  id: number
  code: string
  name: string
  short_name: string
  tax_id: string | null
  phone: string
  email: string
  contacts: Contact[]
  addresses: Address[]
  currency: string
  tax_type_id: number | null
  payment_term_id: number | null
  note: string
  is_active: boolean
  version: number
  updated_at: string
}

export interface Customer extends PartnerBase {
  invoice_title: string
  credit_limit: Decimal
  sales_user_id: number | null
  sales_user_name?: string | null
}

export interface Supplier extends PartnerBase {
  bank_name: string
  bank_account: string
}

export interface UserOption {
  id: number
  username: string
  name: string
  department_id: number | null
}

export interface ListQuery {
  keyword?: string
  is_active?: boolean | null
  page: number
  size: number
}

type Input<T> = Omit<T, 'id' | 'updated_at' | 'version'> & { version?: number }
export type CategoryInput = Omit<Input<ItemCategory>, 'item_count'>

const crud = <T extends { id: number }, I = Input<T>>(base: string) => ({
  create: (input: I) => http.post<T>(base, input),
  update: (id: number, input: I) => http.put<T>(`${base}/${id}`, input),
})

export const masterdataApi = {
  currencies: () => http.get<Currency[]>('/masterdata/currencies'),
  setCurrencyActive: (code: string, is_active: boolean) =>
    http.put<Currency>(`/masterdata/currencies/${code}`, { is_active }),

  exchangeRates: (q: {
    currency?: string
    from?: string
    to?: string
    page: number
    size: number
  }) => requestPage<ExchangeRate>(`/masterdata/exchange-rates${qs(q)}`),
  createExchangeRate: (input: { currency: string; rate_date: string; rate: Decimal }) =>
    http.post<ExchangeRate>('/masterdata/exchange-rates', input),
  updateExchangeRate: (id: number, input: { rate: Decimal; version: number }) =>
    http.put<ExchangeRate>(`/masterdata/exchange-rates/${id}`, input),
  deleteExchangeRate: (id: number) => http.delete(`/masterdata/exchange-rates/${id}`),
  lookupRate: (currency: string, date: string) =>
    http.get<{ currency: string; date: string; rate: Decimal }>(
      `/masterdata/exchange-rates/lookup${qs({ currency, date })}`,
    ),

  taxTypes: () => http.get<TaxType[]>('/masterdata/tax-types'),
  taxType: crud<TaxType>('/masterdata/tax-types'),
  paymentTerms: () => http.get<PaymentTerm[]>('/masterdata/payment-terms'),
  paymentTerm: crud<PaymentTerm>('/masterdata/payment-terms'),
  units: () => http.get<Unit[]>('/masterdata/units'),
  unit: crud<Unit>('/masterdata/units'),
  categories: () => http.get<ItemCategory[]>('/masterdata/item-categories'),
  category: crud<ItemCategory, CategoryInput>('/masterdata/item-categories'),
  warehouses: () => http.get<Warehouse[]>('/masterdata/warehouses'),
  warehouse: crud<Warehouse>('/masterdata/warehouses'),

  items: (q: ListQuery & { category_id?: number | null; item_type?: string }) =>
    requestPage<Item>(`/masterdata/items${qs({ ...q })}`),
  getItem: (id: number) => http.get<Item>(`/masterdata/items/${id}`),
  item: crud<Item>('/masterdata/items'),

  customers: (q: ListQuery) => requestPage<Customer>(`/masterdata/customers${qs({ ...q })}`),
  getCustomer: (id: number) => http.get<Customer>(`/masterdata/customers/${id}`),
  customer: crud<Customer>('/masterdata/customers'),

  suppliers: (q: ListQuery) => requestPage<Supplier>(`/masterdata/suppliers${qs({ ...q })}`),
  getSupplier: (id: number) => http.get<Supplier>(`/masterdata/suppliers/${id}`),
  supplier: crud<Supplier>('/masterdata/suppliers'),

  userOptions: () => http.get<UserOption[]>('/system/user-options'),
}
