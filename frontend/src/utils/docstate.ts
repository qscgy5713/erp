import type { DocAction, DocStatus } from '@/api/inventory'

// 與後端 internal/shared/docstate 一致
export const statusLabels: Record<DocStatus, string> = {
  draft: '草稿',
  pending: '待審',
  approved: '已核准',
  posted: '已過帳',
  closed: '已結案',
  voided: '已作廢',
}

export const statusTagType: Record<
  DocStatus,
  'info' | 'warning' | 'primary' | 'success' | 'danger'
> = {
  draft: 'info',
  pending: 'warning',
  approved: 'primary',
  posted: 'success',
  closed: 'success',
  voided: 'danger',
}

type Transitions = Record<DocStatus, Partial<Record<DocAction, DocStatus>>>

/** 會過帳的單據(庫存單據、進貨 / 退出單):不使用結案 */
const postingFlow: Transitions = {
  draft: { submit: 'pending', void: 'voided' },
  pending: { approve: 'approved', reject: 'draft', void: 'voided' },
  approved: { post: 'posted', unapprove: 'draft', void: 'voided' },
  posted: { unpost: 'approved' },
  closed: {},
  voided: {},
}

/** 採購單:不過帳;核准後可結案,結案可重開回已核准 */
const orderFlow: Transitions = {
  ...postingFlow,
  approved: { unapprove: 'draft', void: 'voided', close: 'closed' },
  posted: {},
  closed: { reopen: 'approved' },
}

export type DocFlow = 'posting' | 'order'

export function allowedActions(status: DocStatus, flow: DocFlow = 'posting'): DocAction[] {
  return Object.keys((flow === 'order' ? orderFlow : postingFlow)[status]) as DocAction[]
}

export const actionLabels: Record<DocAction, string> = {
  submit: '送審',
  reject: '退回',
  approve: '核准',
  unapprove: '取消核准',
  post: '過帳',
  unpost: '反過帳',
  void: '作廢',
  close: '結案',
  reopen: '重開',
}

/** 需要二次確認的動作(影響庫存或無法復原) */
export const confirmActions: Partial<Record<DocAction, string>> = {
  post: '過帳後會異動庫存(進貨單另會產生應付帳款),確定過帳?',
  unpost: '反過帳會以反向分錄沖銷庫存(並移除應付帳款),確定?',
  close: '結案後剩餘未交數量不再進貨,確定結案?',
  void: '作廢後無法復原,確定作廢?',
}
