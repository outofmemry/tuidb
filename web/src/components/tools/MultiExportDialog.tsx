// Export with a table checklist across several schemas, each table to its own file (one download per table: a browser cannot write into a folder).
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, download, qs, saveBlob, watchJob, type DbObject, type Meta, type SessionInfo } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { enterSubmits } from '../../utils/forms'

const SYSTEM_SCHEMA = /^(information_schema|pg_.*|mysql|performance_schema|sys|system|INFORMATION_SCHEMA)$/
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const at = (schema: string, name: string) => schema + '\u0000' + name

function SchemaTables({ sid, schema, picked, toggle, all }: { sid: string; schema: string; picked: Set<string>; toggle: (k: string[], on: boolean) => void; all: boolean }) {
  const q = useQuery({ queryKey: ['multi-objs', sid, schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema })), gcTime: 0 })
  const tables = (q.data ?? []).filter((o) => o.Kind === 'table').map((o) => o.Name)
  const keys = tables.map((n) => at(schema, n))
  const every = keys.length > 0 && keys.every((k) => picked.has(k))
  return (
    <div className="pl-5">
      {q.isLoading && <div className="text-ink-2">Loading…</div>}{q.error && <div className="text-del">{q.error.message}</div>}
      {all && keys.length > 1 && <label className="flex items-center gap-1.5 text-ink-2"><input type="checkbox" checked={every} onChange={(e) => toggle(keys, e.target.checked)} />All tables of {schema || 'this database'}</label>}
      {tables.map((n) => <label key={n} className="flex items-center gap-1.5"><input type="checkbox" checked={picked.has(at(schema, n))} onChange={(e) => toggle([at(schema, n)], e.target.checked)} />{n}</label>)}
    </div>
  )
}

export function MultiExportDialog({ sid, info, initial, onClose, notify }: { sid: string; info: SessionInfo; initial: { Schema: string; Name: string }[]; onClose: () => void; notify: (m: string) => void }) {
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const sch = useQuery({ queryKey: ['schemas', sid, info.database], queryFn: () => api<{ schemas: string[] | null }>('GET', `/sessions/${sid}/schemas`), enabled: info.caps.Schemas, gcTime: 0 })
  const schemas = info.caps.Schemas ? (sch.data?.schemas ?? [info.schema]).filter((n) => meta?.config.sidebar.LoadSystemSchemas || n === info.schema || !SYSTEM_SCHEMA.test(n)) : ['']
  const [picked, setPicked] = useState<Set<string>>(new Set(initial.map((o) => at(o.Schema, o.Name))))
  const [open, setOpen] = useState<Set<string>>(new Set(initial.length ? initial.map((o) => o.Schema) : [info.caps.Schemas ? info.schema : '']))
  const [format, setFormat] = useState('csv'), [gzip, setGzip] = useState(false), [structure, setStructure] = useState(false)
  const [header, setHeader] = useState(true), [pattern, setPattern] = useState('')
  const [state, setState] = useState(''), [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const toggle = (keys: string[], on: boolean) => setPicked((p) => { const n = new Set(p); keys.forEach((k) => (on ? n.add(k) : n.delete(k))); return n })
  const list = [...picked].map((k) => { const [Schema, Name] = k.split('\u0000'); return { Schema, Name } })
  const several = new Set(list.map((o) => o.Schema)).size > 1
  const go = async () => {
    setBusy(true); setErr('')
    const ac = abort.current = new AbortController()
    try {
      let n = 0
      for (const ref of list) {
        setState(`${++n}/${list.length}: ${ref.Name}`)
        const { job } = await api<{ job: string }>('POST', `/sessions/${sid}/export`, { mode: 'table', ref: { ...ref, Kind: 'table' }, format, gzip: gzip && format !== 'xlsx', structure: structure && format === 'sql', job: true, header: format === 'csv' ? header : undefined, filename: pattern.trim() || undefined, fileName: pattern.trim() ? pattern.trim() : several ? `${ref.Schema}.${ref.Name}.${format}${gzip ? '.gz' : ''}` : undefined })
        const j = await watchJob(sid, job, () => {}, ac.signal)
        if (j.error) throw new Error(`${ref.Name}: ${j.error}`)
        const f = await download(`/sessions/${sid}/jobs/${job}/file`)
        saveBlob(f.blob, f.name)
      }
      notify(`Exported ${list.length} table${list.length === 1 ? '' : 's'}, one file each`); onClose()
    } catch (e) { if (!ac.signal.aborted) { setErr(msg(e)); setBusy(false) } }
  }
  return (
    <Dialog title="Export tables" onClose={() => { abort.current?.abort(); onClose() }}>
      <div className="max-h-[36vh] overflow-auto border border-rule p-2 text-[12.5px]" role="group" aria-label="Tables to export" onKeyDown={(e) => enterSubmits(e, () => !busy && list.length > 0 && go())}>
        {schemas.map((s) => (
          <div key={s}>
            {info.caps.Schemas && <button className="btn-ghost" aria-expanded={open.has(s)} onClick={() => setOpen((o) => { const n = new Set(o); n.has(s) ? n.delete(s) : n.add(s); return n })}>{open.has(s) ? '▾' : '▸'} {s}</button>}
            {open.has(s) && <SchemaTables sid={sid} schema={s} picked={picked} toggle={toggle} all />}
          </div>
        ))}
      </div>
      <div className="flex items-center gap-2.5 my-2 text-[12.5px]"><label className="w-28 text-ink-2">Format</label><Select aria-label="Format" value={format} onChange={setFormat} options={[{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }, { value: 'sql', label: 'SQL INSERTs' }, { value: 'xlsx', label: 'Excel (xlsx)' }]} />
        {format !== 'xlsx' && <label className="flex items-center gap-1.5"><input type="checkbox" checked={gzip} onChange={(e) => setGzip(e.target.checked)} />gzip</label>}
        {format === 'sql' && <label className="flex items-center gap-1.5"><input type="checkbox" checked={structure} onChange={(e) => setStructure(e.target.checked)} />structure</label>}</div>
      {format === 'csv' && <label className="flex items-center gap-1.5 text-[12.5px] my-1"><input type="checkbox" checked={header} onChange={(e) => setHeader(e.target.checked)} />Header row</label>}
      <div className="flex items-center gap-2.5 my-2 text-[12.5px]"><label className="w-28 text-ink-2">File name</label><input className="flex-1 min-w-0" aria-label="File name" placeholder={`${meta?.export?.filename || '{table}'} (per table)`} value={pattern} onChange={(e) => setPattern(e.target.value)} onKeyDown={(e) => enterSubmits(e, () => !busy && list.length > 0 && go())} /></div>
      <div className="text-ink-2 text-[12px]">Tokens: {'{connection} {database} {table} {tag} {date} {time}'}.</div>
      {format === 'csv' && meta?.csvWarning && <div className="text-edit text-[12px]" role="alert">{meta.csvWarning}</div>}
      <p className="text-ink-2 text-[12px]">{list.length} table{list.length === 1 ? '' : 's'} chosen. Each one is a separate download{several ? ', named schema.table' : ''}; the browser may ask to allow several downloads.</p>
      {state && <div className="text-[12.5px]" role="status">{state}</div>}
      {err && <div className="text-del mt-1">{err}</div>}
      <div className="flex justify-end gap-2 mt-[18px]"><button onClick={() => { abort.current?.abort(); onClose() }}>Cancel</button><button className="btn-primary" disabled={busy || !list.length} onClick={go}>{busy ? 'Exporting…' : 'Export'}</button></div>
    </Dialog>
  )
}
