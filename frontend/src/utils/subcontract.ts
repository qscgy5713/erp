import type { ScKind, ScStatus } from '@/api/production'

export const scStatusLabels: Record<ScStatus, string> = {
  draft: '草稿(已退回)',
  pending: '待核准',
  open: '進行中',
  closed: '已結案',
  voided: '已作廢',
}

export const scStatusTypes: Record<ScStatus, 'primary' | 'success' | 'info' | 'warning'> = {
  draft: 'info',
  pending: 'warning',
  open: 'primary',
  closed: 'success',
  voided: 'info',
}

export const scKindLabels: Record<ScKind, string> = {
  issue: '發料',
  receive: '收回',
  return: '退料',
}
