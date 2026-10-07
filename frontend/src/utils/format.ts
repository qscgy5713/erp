const dtf = new Intl.DateTimeFormat('zh-TW', {
  timeZone: 'Asia/Taipei',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hour12: false,
})

/** 後端時間一律 UTC,顯示時轉台灣時間 */
export function formatDateTime(iso: string | null | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return ''
  return dtf.format(d)
}

export const dataScopeLabels: Record<string, string> = {
  all: '全部資料',
  department: '本部門',
  self: '僅本人',
}
