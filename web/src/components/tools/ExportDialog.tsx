// Export a table, query result or selection to a file.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, download, saveBlob, watchJob, type FilterSet, type Meta, type ObjRef } from '../../utils/api'
import { enterSubmits } from '../../utils/forms'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { askNotify, desktopNotify } from '../../utils/theme'
import { ACTIONS, ROW, msg } from '../shell/ui'

export type ExportSource =
  | { mode: 'table'; ref: ObjRef; filters: FilterSet; sort: { Column: string; Desc: boolean }[]; columns: string[]; limit: number; offset: number }
  | { mode: 'query'; text: string; columns: string[]; limit: number }

export function ExportDialog({ sid, source, onClose, notify }: { sid: string; source: ExportSource; onClose: () => void; notify: (m: string) => void }) {
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const table = source.mode === 'table'
  const [format, setFormat] = useState('csv')
  const [gzip, setGzip] = useState(false)
  const [structure, setStructure] = useState(false)
  const [drop, setDrop] = useState(false)
  const [page, setPage] = useState(false) // current page only
  const [nullAs, setNullAs] = useState('')
  const [header, setHeader] = useState(true)
  const [lineBreak, setLineBreak] = useState('')
  const ext = format + (gzip && format !== 'xlsx' ? '.gz' : '')
  const [name, setName] = useState('')
  const [busy, setBusy] = useState(false)
  const [rows, setRows] = useState<number | null>(null) // a whole-table export runs as a background job: its progress
  const [err, setErr] = useState('')
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const go = async () => {
    setBusy(true); setErr(''); askNotify()
    try {
      const body = {
        mode: source.mode === 'query' ? 'query' : page ? 'page' : 'table', format, gzip: gzip && format !== 'xlsx',
        structure: structure && format === 'sql' && table, drop: drop && format === 'sql' && table, columns: source.columns, filename: name.trim() || undefined, fileName: name.trim() || undefined,
        header: format === 'csv' ? header : undefined,
        // shortcut: the camelCase twins are the older spelling, drop them once the server only reads the snake_case names
        null_as: format === 'csv' && nullAs !== '' ? nullAs : undefined, nullAs: format === 'csv' && nullAs !== '' ? nullAs : undefined, lineBreak: format === 'csv' && lineBreak !== '' ? lineBreak : undefined,
        ...(source.mode === 'table' ? { ref: source.ref, filters: source.filters, sort: source.sort, limit: source.limit, offset: source.offset } : { text: source.text, limit: source.limit }),
      }
      if (body.mode === 'table') {
        const { job } = await api<{ job: string }>('POST', `/sessions/${sid}/export`, { ...body, job: true })
        const ac = abort.current = new AbortController()
        setRows(0)
        const j = await watchJob(sid, job, (x) => setRows(x.rows), ac.signal)
        if (j.error) throw new Error(j.error)
        const f = await download(`/sessions/${sid}/jobs/${job}/file`)
        saveBlob(f.blob, f.name)
      } else {
        const f = await download(`/sessions/${sid}/export`, body)
        saveBlob(f.blob, f.name)
      }
      notify('Export saved'); desktopNotify('Export finished'); onClose()
    } catch (e) { if (!abort.current?.signal.aborted) { setErr(msg(e)); setBusy(false); setRows(null) } }
  }
  return (
    <Dialog title={`Export ${table ? source.ref.Name : 'result'}`} onClose={() => { abort.current?.abort(); onClose() }}>
      <div className={ROW}><label className="w-28 text-ink-2">Format</label><Select aria-label="Format" value={format} onChange={setFormat} options={[{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }, { value: 'sql', label: 'SQL INSERTs' }, { value: 'xlsx', label: 'Excel (xlsx)' }]} /></div>
      {table && <div className={ROW}><label className="w-28 text-ink-2">Rows</label><Select aria-label="Rows" value={page ? 'page' : 'all'} onChange={(v) => setPage(v === 'page')} options={[{ value: 'all', label: 'Whole table (filters applied)' }, { value: 'page', label: 'Current page only' }]} /></div>}
      {format === 'csv' && meta?.csvWarning && <div className="text-edit text-[12px] mb-1" role="alert">{meta.csvWarning}</div>}
      {format === 'csv' && <>
        <label className={ROW}><input type="checkbox" checked={header} onChange={(e) => setHeader(e.target.checked)} />Header row</label>
        <div className={ROW}><label className="w-28 text-ink-2">NULL as</label><input className="flex-1 min-w-0" aria-label="NULL as" placeholder={meta?.config.csv.NullAs ? `${meta.config.csv.NullAs} (from the settings)` : 'empty (from the settings)'} value={nullAs} onChange={(e) => setNullAs(e.target.value)} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">Line breaks</label><Select aria-label="Line breaks" value={lineBreak} onChange={setLineBreak} options={[{ value: '', label: 'As in the settings' }, { value: '\n', label: 'LF (Unix)' }, { value: '\r\n', label: 'CRLF (Windows)' }]} /></div>
      </>}
      {format !== 'xlsx' && <label className={ROW}><input type="checkbox" checked={gzip} onChange={(e) => setGzip(e.target.checked)} />Compress with gzip</label>}
      {format === 'sql' && table && <>
        <label className={ROW}><input type="checkbox" checked={structure} onChange={(e) => setStructure(e.target.checked)} />Include the table structure (CREATE TABLE)</label>
        <label className={ROW}><input type="checkbox" checked={drop} onChange={(e) => setDrop(e.target.checked)} />Drop the table first</label>
      </>}
      <div className={ROW}><label className="w-28 text-ink-2">File name</label><input className="flex-1 min-w-0" aria-label="File name" placeholder={`${meta?.export?.filename || '{table}'}.${ext}`} value={name} onChange={(e) => setName(e.target.value)} onKeyDown={(e) => enterSubmits(e, () => !busy && go())} /></div>
      <div className="text-ink-2 text-[12px]">Pattern tokens: <code>{'{connection} {database} {table} {tag} {date} {time}'}</code>. Default: <code>{meta?.export?.filename || '{table}'}</code>.</div>
      <div className="text-ink-2 text-[12px]">{source.columns.length ? `${source.columns.length} visible column${source.columns.length === 1 ? '' : 's'}.` : 'All columns.'}</div>
      {rows !== null && <div className="mt-2 text-[12.5px]" role="status">{rows} rows written…</div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={() => { abort.current?.abort(); onClose() }}>Cancel</button><button className="btn-primary" disabled={busy} onClick={go}>{busy ? 'Exporting…' : 'Export'}</button></div>
    </Dialog>
  )
}
