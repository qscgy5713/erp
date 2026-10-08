// 採購與銷售單據共用編輯頁(TradeEditView)的設定:
// 各流程的 API、權限、單據類型、來源引用欄位,並把各 API 的回應轉成共用的 TradeDoc 形狀。
import type { DocAction, DocStatus } from '@/api/inventory'
import { purchaseApi, type GoodsReceipt, type PurchaseOrder } from '@/api/purchase'
import { salesApi, type Delivery, type SalesOrder } from '@/api/sales'

export type FlowKind = 'purchase-order' | 'receipt' | 'sales-order' | 'delivery'
/** 明細引用來源的欄位名稱 */
export type RefKey = 'po_line_id' | 'receipt_line_id' | 'so_line_id' | 'delivery_line_id'

export interface TradeLine {
  id: number
  line_no: number
  item_id: number
  item_code: string
  item_name: string
  item_type: 'goods' | 'service'
  unit_id: number
  unit_name: string
  base_unit_name: string
  factor: string
  qty: string
  unit_price: string
  amount: string
  note: string
  /** 來源明細 id 與單號(進貨 → 採購單、退出 → 進貨單、出貨 → 訂單、退回 → 出貨單) */
  ref_id: number | null
  ref_no: string | null
  /** 訂單類:已交 / 已出貨量與未交量 */
  done_qty?: string
  remaining_qty?: string
  /** 進貨 / 出貨類:批號管理料品的批號與效期;已過帳的出貨另有實際出庫的批號 */
  item_lot_control?: string
  lot_no?: string
  expiry_date?: string | null
  lots?: { lot_no: string; expiry_date: string | null; qty: string }[]
}

export interface TradeDoc {
  id: number
  doc_type: string
  doc_no: string
  doc_date: string
  status: DocStatus
  version: number
  partner_id: number
  partner_label: string
  sales_user_name: string | null
  warehouse_id: number
  currency: string
  exchange_rate: string
  tax_type_id: number
  payment_term_id: number | null
  note: string
  untaxed_amount: string
  tax_amount: string
  total_amount: string
  base_total: string | null
  /** 採購單:預定交貨日;訂單:預定出貨日 */
  expected_date: string | null
  valid_until: string | null
  customer_po_no: string
  quotation_id: number | null
  quotation_no: string | null
  invoice_no: string
  invoice_date: string | null
  /** 進貨單的供應商發票憑證種類(營業稅媒體申報用),空字串 = 沒有 */
  invoice_kind: string
  created_by_name: string | null
  submitted_by_name: string | null
  submitted_at: string | null
  approved_by_name: string | null
  approved_at: string | null
  closed_by_name: string | null
  closed_at: string | null
  posted_by_name: string | null
  posted_at: string | null
  lines: TradeLine[]
}

/** 「從來源單據帶入」對話框的一列 */
export interface ImportRow {
  ref_id: number
  doc_no: string
  doc_date: string
  warehouse_id: number
  item_id: number
  item_code: string
  item_name: string
  unit_id: number
  unit_name: string
  qty: string
  unit_price: string
  remaining_qty: string
}

export interface Importer {
  button: string
  title: string
  remainLabel: string
  fetch: (partnerId: number, currency: string, keyword: string) => Promise<ImportRow[]>
}

export interface Flow {
  kind: FlowKind
  side: 'purchase' | 'sales'
  /** 進貨 / 出貨類會過帳;採購單、報價單、訂單不過帳 */
  posting: boolean
  /** 權限前綴,例如 purchase.order → purchase.order.write */
  perm: string
  routes: { list: string; doc: string; new: string }
  /** 可用的單據類型,第一個為預設;採購單只有一種 */
  docTypes: string[]
  titles: Record<string, string>
  partner: 'supplier' | 'customer'
  partnerLabel: string
  refKey: Partial<Record<string, RefKey>>
  /** 訂單類明細的已交 / 未交欄位名稱(採購:已交 / 未交;銷售:已出 / 未出) */
  doneLabel: string
  remainLabel: string
  importers: Partial<Record<string, Importer>>
  load: (id: number) => Promise<TradeDoc>
  save: (id: number | null, input: Record<string, unknown>) => Promise<TradeDoc>
  action: (id: number, action: DocAction, version: number) => Promise<TradeDoc>
}

const base = (d: PurchaseOrder | GoodsReceipt | SalesOrder | Delivery) => ({
  id: d.id,
  doc_no: d.doc_no,
  doc_date: d.doc_date,
  status: d.status,
  version: d.version,
  warehouse_id: d.warehouse_id,
  currency: d.currency,
  exchange_rate: d.exchange_rate,
  tax_type_id: d.tax_type_id,
  payment_term_id: d.payment_term_id,
  note: d.note,
  untaxed_amount: d.untaxed_amount,
  tax_amount: d.tax_amount,
  total_amount: d.total_amount,
  created_by_name: d.created_by_name,
  submitted_by_name: d.submitted_by_name,
  submitted_at: d.submitted_at,
  approved_by_name: d.approved_by_name,
  approved_at: d.approved_at,
  // 以下由各類型覆寫
  sales_user_name: null,
  base_total: null,
  expected_date: null,
  valid_until: null,
  customer_po_no: '',
  quotation_id: null,
  quotation_no: null,
  invoice_no: '',
  invoice_date: null,
  invoice_kind: '',
  closed_by_name: null,
  closed_at: null,
  posted_by_name: null,
  posted_at: null,
})

const line = (
  l:
    | PurchaseOrder['lines'][number]
    | SalesOrder['lines'][number]
    | GoodsReceipt['lines'][number]
    | Delivery['lines'][number],
) => ({
  id: l.id,
  line_no: l.line_no,
  item_id: l.item_id,
  item_code: l.item_code,
  item_name: l.item_name,
  item_type: l.item_type,
  unit_id: l.unit_id,
  unit_name: l.unit_name,
  base_unit_name: l.base_unit_name,
  factor: l.factor,
  qty: l.qty,
  unit_price: l.unit_price,
  amount: l.amount,
  note: l.note,
})

function fromPurchaseOrder(d: PurchaseOrder): TradeDoc {
  return {
    ...base(d),
    doc_type: 'order',
    partner_id: d.supplier_id,
    partner_label: `${d.supplier_code} ${d.supplier_name}`,
    expected_date: d.expected_date,
    closed_by_name: d.closed_by_name,
    closed_at: d.closed_at,
    lines: d.lines.map((l) => ({
      ...line(l),
      ref_id: null,
      ref_no: null,
      done_qty: l.received_qty,
      remaining_qty: l.remaining_qty,
    })),
  }
}

function fromReceipt(d: GoodsReceipt): TradeDoc {
  return {
    ...base(d),
    doc_type: d.doc_type,
    partner_id: d.supplier_id,
    partner_label: `${d.supplier_code} ${d.supplier_name}`,
    base_total: d.base_total,
    invoice_no: d.invoice_no,
    invoice_date: d.invoice_date,
    invoice_kind: d.invoice_kind,
    posted_by_name: d.posted_by_name,
    posted_at: d.posted_at,
    lines: d.lines.map((l) => ({
      ...line(l),
      ref_id: l.po_line_id ?? l.receipt_line_id,
      ref_no: l.po_no ?? l.source_receipt_no,
      item_lot_control: l.item_lot_control,
      lot_no: l.lot_no,
      expiry_date: l.expiry_date,
      lots: l.lots,
    })),
  }
}

function fromSalesOrder(d: SalesOrder): TradeDoc {
  return {
    ...base(d),
    doc_type: d.doc_type,
    partner_id: d.customer_id,
    partner_label: `${d.customer_code} ${d.customer_name}`,
    sales_user_name: d.sales_user_name,
    expected_date: d.delivery_date,
    valid_until: d.valid_until,
    customer_po_no: d.customer_po_no,
    quotation_id: d.quotation_id,
    quotation_no: d.quotation_no,
    closed_by_name: d.closed_by_name,
    closed_at: d.closed_at,
    lines: d.lines.map((l) => ({
      ...line(l),
      ref_id: null,
      ref_no: null,
      done_qty: l.delivered_qty,
      remaining_qty: l.remaining_qty,
    })),
  }
}

function fromDelivery(d: Delivery): TradeDoc {
  return {
    ...base(d),
    doc_type: d.doc_type,
    partner_id: d.customer_id,
    partner_label: `${d.customer_code} ${d.customer_name}`,
    sales_user_name: d.sales_user_name,
    base_total: d.base_total,
    invoice_no: d.invoice_no,
    invoice_date: d.invoice_date,
    posted_by_name: d.posted_by_name,
    posted_at: d.posted_at,
    lines: d.lines.map((l) => ({
      ...line(l),
      ref_id: l.so_line_id ?? l.delivery_line_id,
      ref_no: l.so_no ?? l.source_delivery_no,
      item_lot_control: l.item_lot_control,
      lot_no: l.lot_no,
      expiry_date: l.expiry_date,
      lots: l.lots,
    })),
  }
}

export const flows: Record<FlowKind, Flow> = {
  'purchase-order': {
    kind: 'purchase-order',
    side: 'purchase',
    posting: false,
    perm: 'purchase.order',
    routes: { list: 'purchase-orders', doc: 'purchase-order', new: 'purchase-order-new' },
    docTypes: ['order'],
    titles: { order: '採購單' },
    partner: 'supplier',
    partnerLabel: '供應商',
    refKey: {},
    doneLabel: '已交',
    remainLabel: '未交',
    importers: {},
    load: async (id) => fromPurchaseOrder(await purchaseApi.order(id)),
    save: async (id, input) =>
      fromPurchaseOrder(
        await (id ? purchaseApi.updateOrder(id, input) : purchaseApi.createOrder(input)),
      ),
    action: async (id, a, v) => fromPurchaseOrder(await purchaseApi.orderAction(id, a, v)),
  },
  receipt: {
    kind: 'receipt',
    side: 'purchase',
    posting: true,
    perm: 'purchase.receipt',
    routes: { list: 'purchase-receipts', doc: 'purchase-receipt', new: 'purchase-receipt-new' },
    docTypes: ['receipt', 'return'],
    titles: { receipt: '進貨單', return: '進貨退出單' },
    partner: 'supplier',
    partnerLabel: '供應商',
    refKey: { receipt: 'po_line_id', return: 'receipt_line_id' },
    doneLabel: '',
    remainLabel: '',
    importers: {
      receipt: {
        button: '從採購單帶入',
        title: '從採購單帶入(未交明細)',
        remainLabel: '未交',
        fetch: async (supplier_id, currency, keyword) =>
          (await purchaseApi.outstanding({ supplier_id, currency, keyword, size: 100 })).items.map(
            (r) => ({ ...r, ref_id: r.po_line_id }),
          ),
      },
      return: {
        button: '從進貨單帶入',
        title: '從進貨單帶入(可退明細)',
        remainLabel: '可退',
        fetch: async (supplier_id, currency, keyword) =>
          (await purchaseApi.returnable({ supplier_id, currency, keyword })).map((r) => ({
            ...r,
            ref_id: r.receipt_line_id,
          })),
      },
    },
    load: async (id) => fromReceipt(await purchaseApi.receipt(id)),
    save: async (id, input) =>
      fromReceipt(
        await (id ? purchaseApi.updateReceipt(id, input) : purchaseApi.createReceipt(input)),
      ),
    action: async (id, a, v) => fromReceipt(await purchaseApi.receiptAction(id, a, v)),
  },
  'sales-order': {
    kind: 'sales-order',
    side: 'sales',
    posting: false,
    perm: 'sales.order',
    routes: { list: 'sales-orders', doc: 'sales-order', new: 'sales-order-new' },
    docTypes: ['order', 'quotation'],
    titles: { quotation: '報價單', order: '訂單' },
    partner: 'customer',
    partnerLabel: '客戶',
    refKey: {},
    doneLabel: '已出',
    remainLabel: '未出',
    importers: {},
    load: async (id) => fromSalesOrder(await salesApi.order(id)),
    save: async (id, input) =>
      fromSalesOrder(await (id ? salesApi.updateOrder(id, input) : salesApi.createOrder(input))),
    action: async (id, a, v) => fromSalesOrder(await salesApi.orderAction(id, a, v)),
  },
  delivery: {
    kind: 'delivery',
    side: 'sales',
    posting: true,
    perm: 'sales.delivery',
    routes: { list: 'sales-deliveries', doc: 'sales-delivery', new: 'sales-delivery-new' },
    docTypes: ['delivery', 'return'],
    titles: { delivery: '出貨單', return: '銷貨退回單' },
    partner: 'customer',
    partnerLabel: '客戶',
    refKey: { delivery: 'so_line_id', return: 'delivery_line_id' },
    doneLabel: '',
    remainLabel: '',
    importers: {
      delivery: {
        button: '從訂單帶入',
        title: '從訂單帶入(未出貨明細)',
        remainLabel: '未出',
        fetch: async (customer_id, currency, keyword) =>
          (await salesApi.unshipped({ customer_id, currency, keyword, size: 100 })).items.map(
            (r) => ({ ...r, ref_id: r.so_line_id }),
          ),
      },
      return: {
        button: '從出貨單帶入',
        title: '從出貨單帶入(可退明細)',
        remainLabel: '可退',
        fetch: async (customer_id, currency, keyword) =>
          (await salesApi.returnable({ customer_id, currency, keyword })).map((r) => ({
            ...r,
            ref_id: r.delivery_line_id,
          })),
      },
    },
    load: async (id) => fromDelivery(await salesApi.delivery(id)),
    save: async (id, input) =>
      fromDelivery(
        await (id ? salesApi.updateDelivery(id, input) : salesApi.createDelivery(input)),
      ),
    action: async (id, a, v) => fromDelivery(await salesApi.deliveryAction(id, a, v)),
  },
}
