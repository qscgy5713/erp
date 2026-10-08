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

const transitions: Record<DocStatus, Partial<Record<DocAction, DocStatus>>> = {
  draft: { submit: 'pending', void: 'voided' },
  pending: { approve: 'approved', reject: 'draft', void: 'voided' },
  approved: { post: 'posted', unapprove: 'draft', void: 'voided' },
  posted: { unpost: 'approved' },
  closed: {},
  voided: {},
}

export function allowedActions(status: DocStatus): DocAction[] {
  return Object.keys(transitions[status]) as DocAction[]
}

export const actionLabels: Record<DocAction, string> = {
  submit: '送審',
  reject: '退回',
  approve: '核准',
  unapprove: '取消核准',
  post: '過帳',
  unpost: '反過帳',
  void: '作廢',
}

/** 需要二次確認的動作(影響庫存或無法復原) */
export const confirmActions: Partial<Record<DocAction, string>> = {
  post: '過帳後會異動庫存,確定過帳?',
  unpost: '反過帳會以反向分錄沖銷庫存,確定?',
  void: '作廢後無法復原,確定作廢?',
}
