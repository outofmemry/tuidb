// Milestone 2 dialogs: export, import, search in database, process list, query variables, copy as, and the result chart.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, download, qs, saveBlob, upload, watchJob, type Column, type DbObject, type FilterSet, type Gate, type Job, type Meta, type ObjRef, type SessionInfo } from '../utils/api'
import { COPY_FORMATS, copyText, formatRows, type CopyFormat } from '../utils/copy'
import { filterMenu, whyNot, type MenuItem } from '../utils/menus'
import { enterSubmits, previewLength, sortProcs, type ProcRow } from '../utils/m13'
import { CellEditor } from './CellEditor'
import { Dialog, GateFields, ask, confirm } from './Dialogs'
import { Select } from './Select'
import { askNotify, desktopNotify } from '../utils/theme'

const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
const ROW = 'flex items-center gap-2.5 my-2 text-[12.5px]'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ---- export ---------------------------------------------------------------------------

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
      <div className={ROW}><label className="w-28 text-ink-2">File name</label><input className="flex-1 min-w-0" aria-label="File name" placeholder={`${meta?.export?.filename || '{table}'}.${ext}`} value={name} onChange={(e) => setName(e.target.value)} /></div>
      <div className="text-ink-2 text-[12px]">{source.columns.length ? `${source.columns.length} visible column${source.columns.length === 1 ? '' : 's'}.` : 'All columns.'} The pattern may use {'{connection} {database} {table} {tag} {date} {time}'}.</div>
      {rows !== null && <div className="mt-2 text-[12.5px]" role="status">{rows} rows written…</div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={() => { abort.current?.abort(); onClose() }}>Cancel</button><button className="btn-primary" disabled={busy} onClick={go}>{busy ? 'Exporting…' : 'Export'}</button></div>
    </Dialog>
  )
}

// ---- import ----------------------------------------------------------------------------

const sourceOf = (file: string) => (/\.json$/i.test(file) ? 'json' : /\.sql$/i.test(file) ? 'sql' : 'csv')

export function ImportDialog({ sid, table: ref, onClose, onDone, notify }: { sid: string; table: ObjRef; onClose: () => void; onDone: () => void; notify: (m: string) => void }) {
  const base = `/sessions/${sid}`
  const [file, setFile] = useState<File | null>(null)
  const [fileName, setFileName] = useState('')
  const [source, setSource] = useState('csv')
  const [header, setHeader] = useState(true)
  const [delimiter, setDelimiter] = useState('')
  const [create, setCreate] = useState(false)
  const [prev, setPrev] = useState<{ columns: string[]; rows: string[][] } | null>(null)
  const [mapping, setMapping] = useState<Record<string, string>>({})
  const [err, setErr] = useState('')
  const [gate, setGate] = useState<Gate | null>(null)
  const [password, setPassword] = useState('')
  const [job, setJob] = useState<Job | null>(null)
  const startingRef = useRef(false)
  const [starting, setStarting] = useState(false) // true from the click until the POST answers, so a double click cannot start two imports
  const cols = useQuery({ queryKey: ['cols', sid, ref], queryFn: () => api<Column[]>('GET', `${base}/object/columns` + qs({ schema: ref.Schema, name: ref.Name, kind: ref.Kind })), gcTime: 0 })
  const tcols = (cols.data ?? []).map((c) => c.Name)

  useEffect(() => {
    if (!file) return
    const t = setTimeout(async () => {
      try {
        const p = await api<{ columns: string[]; rows: string[][] }>('POST', `${base}/import/preview`, upload({ source, header, delimiter }, source === 'csv' ? file.slice(0, 1 << 20) : file))
        setPrev(p); setErr('')
        setMapping(Object.fromEntries(p.columns.map((c) => [c, create || tcols.includes(c) ? c : ''])))
      } catch (e) { setPrev(null); setErr(msg(e)) }
    }, 250)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file, source, header, delimiter, create, cols.data])

  const pick = async (f: File | undefined) => {
    if (!f) return
    setFileName(f.name); setSource(sourceOf(f.name)); setFile(f)
  }
  const start = async (confirmed: boolean) => {
    if (startingRef.current) return
    startingRef.current = true; setStarting(true)
    setErr(''); askNotify()
    try {
      const r = await api<{ job: string }>('POST', `${base}/import`, upload({ source, header, delimiter, createTable: create, ref, mapping: source === 'sql' ? undefined : mapping, confirmed, password }, file!))
      setGate(null)
      setJob({ done: false, error: '', rows: 0, batches: 0, total: 0, checked: 0, hits: [], lines: [] }) // the job record exists from here, so `running` stays true past the POST
      follow(r.job)
    } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.body.gate) { setGate(e.body.gate); if (confirmed) setErr(e.message) } else setErr(msg(e))
    } finally { startingRef.current = false; setStarting(false) }
  }
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const follow = async (id: string) => {
    const ac = abort.current = new AbortController()
    try {
      const j = await watchJob(sid, id, setJob, ac.signal)
      desktopNotify(j.error ? `Import failed: ${j.error}` : `Imported ${j.rows} rows`)
      if (!j.error) { notify(`Imported ${j.rows} rows`); onDone() }
    } catch (e) { if (!ac.signal.aborted) setErr(msg(e)) }
  }
  const running = (!!job && !job.done) || starting

  return (
    <Dialog title={`Import into ${ref.Name}`} onClose={() => !running && onClose()}>
      <div className={ROW}><input type="file" aria-label="File" accept=".csv,.tsv,.json,.sql,text/*" onChange={(e) => pick(e.target.files?.[0])} /></div>
      {file && <>
        <div className={ROW}>
          <Select aria-label="Source" value={source} onChange={setSource} options={[{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }, { value: 'sql', label: 'SQL dump' }]} />
          {source === 'csv' && <>
            <label className="flex items-center gap-1.5"><input type="checkbox" checked={header} onChange={(e) => setHeader(e.target.checked)} />Header row</label>
            <label className="flex items-center gap-1.5">Delimiter <input className="w-12" aria-label="Delimiter" placeholder="," value={delimiter} maxLength={1} onChange={(e) => setDelimiter(e.target.value)} /></label>
          </>}
          {source !== 'sql' && <label className="flex items-center gap-1.5"><input type="checkbox" checked={create} onChange={(e) => setCreate(e.target.checked)} />Create the table</label>}
        </div>
        {source === 'sql' && <div className="text-ink-2 text-[12px]">The file is run as it is. {fileName}</div>}
        {prev && <>
          <div className="text-ink-2 text-[12px] mb-1">Preview of {prev.rows.length} row{prev.rows.length === 1 ? '' : 's'} from {fileName}</div>
          <div className="overflow-auto max-h-[220px] border border-rule">
            <table className="text-[12px] font-mono border-collapse">
              {source !== 'sql' && <thead><tr>{prev.columns.map((c) => (
                <th key={c} className="text-left p-1 border-b border-rule-2 font-medium align-top">
                  <div>{c}</div>
                  <Select aria-label={`Map ${c}`} className="mt-1 w-full" value={mapping[c] ?? ''} onChange={(v) => setMapping({ ...mapping, [c]: v })} options={[{ value: '', label: 'skip' }, ...(create ? [c] : tcols).map((n) => ({ value: n, label: n }))]} />
                </th>))}</tr></thead>}
              <tbody>{prev.rows.map((r, i) => <tr key={i}>{r.map((v, j) => <td key={j} className="p-1 border-b border-rule whitespace-nowrap max-w-[24ch] overflow-hidden text-ellipsis">{v}</td>)}</tr>)}</tbody>
            </table>
          </div>
        </>}
      </>}
      {gate && <GateFields gate={gate} label="Safe mode" password={password} setPassword={setPassword} />}
      {job && <div className="mt-3 text-[12.5px]" role="status">{job.done ? (job.error ? '' : 'Done. ') : 'Importing. '}{job.rows} rows in {job.batches} batches.</div>}
      {job?.error && <div className="text-del mt-1 whitespace-pre-wrap">Stopped: {job.error}</div>}
      {err && <div className="text-del mt-2 whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}>
        <button disabled={running} onClick={onClose}>{job?.done ? 'Close' : 'Cancel'}</button>
        {!job?.done && <button className="btn-primary" disabled={running || !file || !prev || (gate?.password && !password) || undefined} onClick={() => start(!!gate)}>Import</button>}
      </div>
    </Dialog>
  )
}

// ---- search in database ------------------------------------------------------------------

const OPS = ['Contains', 'Not contains', '=', 'Has prefix', 'Has suffix']

export function SearchDialog({ sid, info, onClose, onOpen }: { sid: string; info: SessionInfo; onClose: () => void; onOpen: (ref: ObjRef, f: FilterSet) => void }) {
  const base = `/sessions/${sid}`
  const [value, setValue] = useState('')
  const [op, setOp] = useState('Contains')
  const [some, setSome] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const [job, setJob] = useState<Job | null>(null)
  const [id, setId] = useState('')
  const [err, setErr] = useState('')
  const objs = useQuery({ queryKey: ['objects', sid, info.database, info.schema], queryFn: () => api<DbObject[]>('GET', `${base}/objects` + qs({ schema: info.schema })), gcTime: 0 })
  const tables = (objs.data ?? []).filter((o) => o.Kind === 'table').map((o) => o.Name)
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const run = async () => {
    setErr(''); setJob(null)
    try {
      const r = await api<{ job: string; total: number }>('POST', `${base}/search`, { Value: value, Op: op, Schema: info.schema, Tables: some ? picked : [] })
      setId(r.job); setJob({ done: false, error: '', rows: 0, batches: 0, total: r.total, checked: 0, hits: [], lines: [] })
      const ac = abort.current = new AbortController()
      watchJob(sid, r.job, setJob, ac.signal).catch((e) => { if (!ac.signal.aborted) setErr(msg(e)) })
    } catch (e) { setErr(msg(e)) }
  }
  const stop = () => { api('DELETE', `${base}/jobs/${id}`).catch(() => {}) }
  const running = !!job && !job.done
  return (
    <Dialog title="Search in database" onClose={() => { stop(); onClose() }}>
      <div className={ROW}>
        <input autoFocus className="flex-1 min-w-0" aria-label="Value" placeholder="Value" value={value} onChange={(e) => setValue(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && value && !running) run() }} />
        <Select aria-label="Operator" value={op} onChange={setOp} options={OPS} />
      </div>
      <div className={ROW}>
        <label className="flex items-center gap-1.5"><input type="radio" checked={!some} onChange={() => setSome(false)} />All tables</label>
        <label className="flex items-center gap-1.5"><input type="radio" checked={some} onChange={() => setSome(true)} />Selected tables</label>
      </div>
      {some && <div className="border border-rule max-h-[140px] overflow-auto p-1.5 text-[12.5px]">
        {tables.map((t) => <label key={t} className="flex items-center gap-1.5"><input type="checkbox" checked={picked.includes(t)} onChange={(e) => setPicked(e.target.checked ? [...picked, t] : picked.filter((x) => x !== t))} />{t}</label>)}
      </div>}
      {job && <div className="mt-3 text-[12.5px]" role="status">{job.checked}/{job.total} tables{running ? '…' : job.hits.length ? '' : '. No matches.'}</div>}
      {job && job.hits.length > 0 && <div className="mt-1 border border-rule max-h-[240px] overflow-auto">
        {job.hits.map((h) => (
          <button key={h.ref.Schema + '.' + h.ref.Name} className="flex w-full justify-between gap-3 text-left border-transparent border-b-rule" disabled={!!h.error} title={h.error} onClick={() => { onOpen(h.ref, h.filters); onClose() }}>
            <span className="truncate">{h.ref.Name}</span><span className={h.error ? 'text-del' : 'text-ink-2'}>{h.error ? 'failed' : `${h.count} row${h.count === 1 ? '' : 's'}`}</span>
          </button>))}
      </div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}>
        <button onClick={() => { stop(); onClose() }}>Close</button>
        {running ? <button onClick={stop}>Stop</button> : <button className="btn-primary" disabled={!value || (some && !picked.length)} onClick={run}>Search</button>}
      </div>
    </Dialog>
  )
}

// ---- process list ------------------------------------------------------------------------

type Proc = { id: string; user: string; db: string; state: string; elapsedMs: number; query: string }

export function ProcessDialog({ sid, info, onClose, notify }: { sid: string; info: SessionInfo; onClose: () => void; notify: (m: string) => void }) {
  const q = useQuery({ queryKey: ['procs', sid], queryFn: () => api<{ supported: boolean; processes: Proc[] }>('GET', `/sessions/${sid}/processes`), gcTime: 0 })
  const [by, setBy] = useState('elapsed') // sort column; elapsed puts the slowest first
  const [look, setLook] = useState<string | null>(null) // quick look at a query
  const [kill, setKill] = useState<{ id: string; gate: Gate } | null>(null)
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const send = async (id: string, confirmed: boolean) => {
    setErr('')
    try { await api('POST', `/sessions/${sid}/processes/kill`, { ID: id, Confirmed: confirmed, Password: password }); setKill(null); setPassword(''); notify(`Killed process ${id}`); q.refetch() } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.body.gate) { setKill({ id, gate: e.body.gate }); if (confirmed) setErr(e.message) } else setErr(msg(e))
    }
  }
  const procs = [...(q.data?.processes ?? [])].sort((a, b) => (by === 'elapsed' ? b.elapsedMs - a.elapsedMs : String(a[by as 'id']).localeCompare(String(b[by as 'id']), undefined, { numeric: true })))
  const table = () => [['id', 'user', 'db', 'state', 'elapsed_ms', 'query'], ...procs.map((p) => [p.id, p.user, p.db, p.state, String(p.elapsedMs), p.query])]
  const copyRows = () => copyText(table().map((r) => r.join('\t')).join('\n')).then(() => notify(`Copied ${procs.length} processes`), (e) => notify(msg(e)))
  const exportPage = () => saveBlob(new Blob([table().map((r) => r.map((v) => (/[",\n]/.test(v) ? `"${v.replace(/"/g, '""')}"` : v)).join(',')).join('\n') + '\n'], { type: 'text/csv;charset=utf-8' }), 'processes.csv')
  const ask = async (id: string) => { if (await confirm(`Kill process ${id}?`)) send(id, true) }
  return (
    <Dialog title="Process list" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Loading…</div>}
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !q.data.supported && <div className="text-ink-2">The process list is not supported by {info.driver}.</div>}
      {q.data?.supported && (q.data.processes.length === 0 ? <div className="text-ink-2">No other processes.</div> : (
        <div className="overflow-auto max-h-[50vh] border border-rule">
          <table className="text-[12px] font-mono border-collapse w-full">
            <thead><tr>{([['Id', 'id'], ['User', 'user'], ['Database', 'db'], ['State', 'state'], ['Elapsed', 'elapsed'], ['Query', ''], ['', '']] as const).map(([h, k], i) => <th key={i} className="text-left p-1 border-b border-rule-2 font-medium text-ink-2" aria-sort={k && k === by ? 'ascending' : undefined}>{k ? <button className="btn-ghost p-0 font-medium" title={`Sort by ${h}`} onClick={() => setBy(k)}>{h}{k === by ? ' ▾' : ''}</button> : h}</th>)}</tr></thead>
            <tbody>{procs.map((p) => (
              <tr key={p.id}>
                <td className="p-1 border-b border-rule">{p.id}</td><td className="p-1 border-b border-rule">{p.user}</td><td className="p-1 border-b border-rule">{p.db}</td><td className="p-1 border-b border-rule">{p.state}</td>
                <td className="p-1 border-b border-rule whitespace-nowrap">{(p.elapsedMs / 1000).toFixed(1)}s</td>
                <td className="p-1 border-b border-rule max-w-[36ch] truncate cursor-pointer" title="Click for a closer look" onClick={() => setLook(p.query)}>{p.query}</td>
                <td className="p-1 border-b border-rule"><button className="btn-ghost btn-danger" onClick={() => ask(p.id)}>Kill</button></td>
              </tr>))}</tbody>
          </table>
        </div>))}
      {kill && <><GateFields gate={kill.gate} label="Safe mode" password={password} setPassword={setPassword} /><div className={ACTIONS}><button onClick={() => setKill(null)}>Cancel</button><button className="btn-primary" disabled={kill.gate.password && !password} onClick={() => send(kill.id, true)}>Kill {kill.id}</button></div></>}
      {err && <div className="text-del mt-2">{err}</div>}
      {look !== null && <CellEditor col="Query" readOnly value={look} onDone={() => setLook(null)} />}
      <div className={ACTIONS}><button onClick={() => q.refetch()}>Refresh</button>{procs.length > 0 && <><button onClick={copyRows}>Copy rows</button><button onClick={exportPage}>Export page</button></>}<button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}

// ---- query variables ---------------------------------------------------------------------

export const PARAM_FORMATS: [string, string][] = [['colon', ':name'], ['dollar', '$name'], ['at', '@name'], ['brace', '${name}'], ['mustache', '{{name}}'], ['question', '? (by position)']]

// format picker: overrides [editor] param_format for this run; changing it asks the server which parameters the text has in that spelling.
export function VariablesDialog({ names: first, init, format: first_fmt, onFormat, onRun, onClose }: { names: string[]; init: Record<string, string>; format: string; onFormat: (f: string) => Promise<string[]>; onRun: (v: Record<string, string>, format: string) => void; onClose: () => void }) {
  const [names, setNames] = useState(first), [format, setFormat] = useState(first_fmt), [err, setErr] = useState('')
  const [vals, setVals] = useState<Record<string, string>>(Object.fromEntries(first.map((n) => [n, init[n] ?? ''])))
  const pick = async (f: string) => {
    setFormat(f); setErr('')
    try { const n = await onFormat(f); setNames(n); setVals((v) => Object.fromEntries(n.map((x) => [x, v[x] ?? init[x] ?? '']))) } catch (e) { setErr(msg(e)) }
  }
  return (
    <Dialog title="Query parameters" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); onRun(vals, format) }}>
        <div className={ROW}><label className="w-32 text-ink-2">Format</label><Select aria-label="Parameter format" value={format} onChange={pick} options={PARAM_FORMATS.map(([value, label]) => ({ value, label }))} /></div>
        {names.length === 0 && <div className="text-ink-2 text-[12.5px]">The text has no parameters in this format.</div>}
        {names.map((n, i) => (
          <div key={n} className={ROW}><label className="w-32 text-ink-2 truncate" title={n}>{n}</label><input autoFocus={i === 0} className="flex-1 min-w-0" aria-label={n} value={vals[n] ?? ''} onChange={(e) => setVals({ ...vals, [n]: e.target.value })} /></div>
        ))}
        <div className="text-ink-2 text-[12px]">Numbers and NULL are used as they are; anything else is quoted.</div>
        {err && <div className="text-del mt-1">{err}</div>}
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button type="submit" className="btn-primary" disabled={names.length === 0}>Run</button></div>
      </form>
    </Dialog>
  )
}

// ---- copy as -----------------------------------------------------------------------------

export function CopyAsDialog({ onPick, onClose }: { onPick: (f: CopyFormat) => void; onClose: () => void }) {
  return (
    <Dialog title="Copy as" onClose={onClose}>
      <div className="flex flex-col gap-0.5">
        {COPY_FORMATS.map((f, i) => <button key={f} autoFocus={i === 0} className="text-left border-transparent hover:border-ink-3" onClick={() => onPick(f)}>{f}</button>)}
      </div>
    </Dialog>
  )
}

// ---- chart -------------------------------------------------------------------------------

export type Series = { kind: string; labels: string[]; values: number[]; series?: { name: string; values: number[] }[] }

export function ChartView({ stmt, columns, cells, notify }: { stmt: string; columns: string[]; cells: (string | null)[][]; notify?: (m: string) => void }) {
  const [kind, setKind] = useState('')
  const q = useQuery({ queryKey: ['chart', stmt, columns, cells, kind], queryFn: () => api<Series>('POST', '/chart', { Stmt: stmt, Kind: kind, Columns: columns, Cells: cells }), gcTime: 0 })
  const s = q.data
  const kinds = ['bar', 'line', 'pie']
  const addToBoard = async () => {
    const name = await ask('Metrics board name'); if (!name) return
    try { await api('POST', '/boards', { Name: name, Query: stmt, Kind: kind || s?.kind }); notify?.(`Added to board ${name}`) } catch (e) { notify?.(msg(e)) }
  }
  const box = useRef<HTMLDivElement>(null)
  // PNG: the SVG is drawn into a canvas with the CSS variables resolved (an image cannot see the page's :root); a pie is a list of bars, so it is drawn directly.
  const savePng = async () => {
    if (!s) return
    const cs = getComputedStyle(document.documentElement), c = document.createElement('canvas'), x = c.getContext('2d')!
    const css = (n: string) => cs.getPropertyValue(n).trim() || '#888'
    try {
      const svg = box.current?.querySelector('svg')
      if (svg) {
        const img = new Image(), url = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(svg).replace(/var\((--[\w-]+)\)/g, (_, n) => css(n))], { type: 'image/svg+xml' }))
        await new Promise((ok, no) => { img.onload = ok; img.onerror = no; img.src = url })
        c.width = W * 2; c.height = H * 2; x.fillStyle = css('--canvas'); x.fillRect(0, 0, c.width, c.height); x.drawImage(img, 0, 0, c.width, c.height)
        URL.revokeObjectURL(url)
      } else {
        const max = Math.max(...s.values, 1), total = s.values.reduce((a, b) => a + Math.max(b, 0), 0) || 1
        c.width = 1280; c.height = 40 + s.values.length * 36; x.fillStyle = css('--canvas'); x.fillRect(0, 0, c.width, c.height); x.font = '22px monospace'
        s.values.forEach((v, i) => { const y = 36 + i * 36; x.fillStyle = css('--ink-2'); x.fillText(cut(s.labels[i], 22), 20, y); x.fillStyle = css('--accent'); x.fillRect(340, y - 20, (Math.max(v, 0) / max) * 600, 22); x.fillStyle = css('--ink'); x.fillText(`${fmt(v)} (${((Math.max(v, 0) / total) * 100).toFixed(1)}%)`, 960, y) })
      }
      c.toBlob((b) => { if (b) saveBlob(b, 'chart.png'); else notify?.('Could not render the chart') })
    } catch (e) { notify?.(msg(e)) }
  }
  return (
    <div className="flex-1 min-h-0 overflow-auto p-3" ref={box}>
      <div className="flex gap-1.5 mb-2" role="group" aria-label="Chart type">{kinds.map((k) => <button key={k} className={(kind || s?.kind) === k ? 'btn-on' : ''} onClick={() => setKind(k)}>{k}</button>)}
        {s && s.values.length > 0 && <><button className="ml-auto" onClick={savePng}>Save as PNG</button><button onClick={addToBoard}>Add to Metrics Board</button></>}</div>
      {q.error && <div className="text-del text-[12.5px]">{q.error.message}</div>}
      {s && s.values.length === 0 && <div className="text-ink-2 text-[12.5px]">Nothing to draw: the result has no numeric column.</div>}
      {s && s.values.length > 0 && <Plot s={s} />}
    </div>
  )
}

export function Plot({ s }: { s: Series }) { return s.kind === 'pie' ? <Bars s={s} /> : s.kind === 'line' ? <Line s={s} /> : <Columns s={s} /> }

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2))
const cut = (l: string, n = 12) => (l.length > n ? l.slice(0, n - 1) + '…' : l)
const W = 640, H = 260, PAD = 34
// several y columns: the first keeps the accent, the others step around the hue wheel
const hueOf = (i: number) => (i === 0 ? 'var(--accent)' : `hsl(${(i * 67 + 200) % 360} 55% 58%)`)
const many = (s: Series) => (s.series && s.series.length > 1 ? s.series : [{ name: '', values: s.values }])

function Legend({ list }: { list: { name: string }[] }) {
  if (list.length < 2) return null
  return <div className="flex flex-wrap gap-x-4 gap-y-1 mb-1.5 text-[12px] font-mono text-ink-2" aria-label="Legend">{list.map((x, i) => <span key={i} className="flex items-center gap-1.5"><span className="inline-block w-3 h-3 rounded-[1px]" style={{ background: hueOf(i) }} />{x.name}</span>)}</div>
}

function Columns({ s }: { s: Series }) {
  const list = many(s), n = s.labels.length || s.values.length
  const max = Math.max(...list.flatMap((x) => x.values), 0) || 1, gw = (W - PAD) / n, bw = Math.max((gw - 4) / list.length, 1)
  return (
    <div><Legend list={list} />
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full max-w-[760px]" role="img" aria-label="Bar chart">
      {s.labels.map((label, i) => (
        <g key={i}>
          {list.map((ser, k) => {
            const v = ser.values[i] ?? 0, h = (Math.max(v, 0) / max) * (H - PAD - 14), x = PAD + i * gw + 2 + k * bw
            return <g key={k}><title>{`${label}${ser.name ? ' / ' + ser.name : ''}: ${fmt(v)}`}</title>
              <rect x={x} y={H - PAD - h} width={bw} height={h} fill={hueOf(k)} />
              {list.length === 1 && <text x={x + bw / 2} y={H - PAD - h - 3} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{fmt(v)}</text>}</g>
          })}
          <text x={PAD + i * gw + gw / 2} y={H - 14} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{cut(label, Math.max(3, Math.floor(gw / 6)))}</text>
        </g>
      ))}
      <line x1={PAD} x2={W} y1={H - PAD} y2={H - PAD} stroke="var(--rule-2)" />
    </svg></div>
  )
}

function Line({ s }: { s: Series }) {
  const list = many(s), all = list.flatMap((x) => x.values), n = s.labels.length || s.values.length
  const max = Math.max(...all, 0) || 1, min = Math.min(...all, 0), span = max - min || 1
  const step = n > 1 ? (W - PAD - 10) / (n - 1) : 0
  const pt = (v: number, i: number) => [PAD + i * step, H - PAD - ((v - min) / span) * (H - PAD - 14)]
  return (
    <div><Legend list={list} />
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full max-w-[760px]" role="img" aria-label="Line chart">
      <line x1={PAD} x2={W} y1={H - PAD} y2={H - PAD} stroke="var(--rule-2)" />
      {list.map((ser, k) => <g key={k}>
        <polyline fill="none" stroke={hueOf(k)} strokeWidth="2" points={ser.values.map((v, i) => pt(v, i).join(',')).join(' ')} />
        {ser.values.map((v, i) => { const [x, y] = pt(v, i); return <g key={i}><title>{`${s.labels[i]}${ser.name ? ' / ' + ser.name : ''}: ${fmt(v)}`}</title><circle cx={x} cy={y} r="3" fill={hueOf(k)} /></g> })}</g>)}
      {s.labels.map((l, i) => (n <= 12 || i % Math.ceil(n / 12) === 0) && <text key={i} x={pt(0, i)[0]} y={H - 14} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{cut(l, 8)}</text>)}
      <text x="2" y="12" fontSize="9" fill="var(--ink-2)">{fmt(max)}</text><text x="2" y={H - PAD} fontSize="9" fill="var(--ink-2)">{fmt(min)}</text>
    </svg></div>
  )
}

// a pie is a sorted bar list: the server sorts it, the share is the label
function Bars({ s }: { s: Series }) {
  const total = s.values.reduce((a, b) => a + Math.max(b, 0), 0) || 1, max = Math.max(...s.values) || 1
  return (
    <div className="font-mono text-[12px] max-w-[760px]">
      {s.values.map((v, i) => (
        <div key={i} className="flex items-center gap-2 py-0.5">
          <span className="w-40 truncate text-ink-2" title={s.labels[i]}>{s.labels[i]}</span>
          <span className="flex-1 h-3 bg-raised"><span className="block h-full bg-accent" style={{ width: `${(Math.max(v, 0) / max) * 100}%` }} /></span>
          <span className="w-28 text-right">{fmt(v)} ({((Math.max(v, 0) / total) * 100).toFixed(1)}%)</span>
        </div>
      ))}
    </div>
  )
}
