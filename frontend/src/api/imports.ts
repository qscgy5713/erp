import { download, http, upload } from './http'

export interface ImportType {
  key: string
  label: string
  desc: string
  columns: string[]
  needs_date: boolean
  undoable: boolean
}

export interface ImportBatch {
  id: number
  import_type: string
  type_label: string
  filename: string
  row_count: number
  summary: string
  created_by_name: string | null
  created_at: string
  undoable: boolean
  undone_at: string | null
}

export interface RowError {
  row: number
  column: string
  message: string
}

export interface ImportResult {
  type: string
  dry_run: boolean
  ok: boolean
  row_count: number
  summary: string
  error_count: number
  errors: RowError[]
  batch_id: number | null
}

export const importsApi = {
  types: () => http.get<ImportType[]>('/imports/types'),
  batches: () => http.get<ImportBatch[]>('/imports/batches'),
  template: (t: ImportType) => download(`/imports/types/${t.key}/template`, `${t.label}範本.xlsx`),
  run: (key: string, file: File, opts: { dryRun: boolean; date?: string }) =>
    upload<ImportResult>(
      `/imports/types/${key}?dry_run=${opts.dryRun}${opts.date ? `&date=${opts.date}` : ''}`,
      file,
    ),
  undo: (id: number) => http.post<unknown>(`/imports/batches/${id}/undo`),
}
