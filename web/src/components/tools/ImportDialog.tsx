// Import a CSV, JSON or SQL file into a table.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, qs, upload, watchJob, type Column, type Gate, type Job, type ObjRef } from '../../utils/api'
import { previewLength } from '../../utils/forms'
import { Dialog, GateFields } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { askNotify, desktopNotify } from '../../utils/theme'
import { ACTIONS, ROW, msg } from '../shell/ui'

const ENCODINGS = [{ value: 'utf-8', label: 'UTF-8' }, { value: 'latin1', label: 'Latin-1' }, { value: 'utf-16le', label: 'UTF-16 LE' }, { value: 'utf-16be', label: 'UTF-16 BE' }, { value: 'windows-1252', label: 'Windows-1252' }]

// autoMap picks a table column for every file column. name = same name ignoring case; order = same position;
// both = by name, then the file columns left over take the table columns left over, in order.
export function autoMap(file: string[], table: string[], match: string, create: boolean): Record<string, string> {
  if (create) return Object.fromEntries(file.map((c) => [c, c]))
  const out: Record<string, string> = {}, used = new Set<string>()
  if (match !== 'order') for (const c of file) { const t = table.find((n) => n.toLowerCase() === c.toLowerCase() && !used.has(n)); if (t) { out[c] = t; used.add(t) } }
  if (match !== 'name') { const free = table.filter((n) => !used.has(n)); file.forEach((c, i) => { if (out[c] === undefined) { const t = match === 'order' ? table[i] : free.shift(); if (t) out[c] = t } }) }
  return Object.fromEntries(file.map((c) => [c, out[c] ?? '']))
}

const sourceOf = (file: string) => (/\.json$/i.test(file) ? 'json' : /\.sql$/i.test(file) ? 'sql' : 'csv')

// `file` starts the dialog with a file dropped on the table (the drop zones live in the grid and the sidebar).
export function ImportDialog({ sid, table: ref, file: dropped, onClose, onDone, notify }: { sid: string; table: ObjRef; file?: File; onClose: () => void; onDone: () => void; notify: (m: string) => void }) {
  const base = `/sessions/${sid}`
  const [file, setFile] = useState<File | null>(dropped ?? null)
  const [fileName, setFileName] = useState(dropped?.name ?? '')
  const [source, setSource] = useState(dropped ? sourceOf(dropped.name) : 'csv')
  const [header, setHeader] = useState(true)
  const [delimiter, setDelimiter] = useState('')
  const [encoding, setEncoding] = useState('utf-8')
  const [match, setMatch] = useState('both') // how file columns find table columns: both | name | order
  const [create, setCreate] = useState(false)
  const [nullAs, setNullAs] = useState(''), [emptyNull, setEmptyNull] = useState(false), [keepQuote, setKeepQuote] = useState(false) // cell transform: shared by the preview and the import
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
        let body: Blob = file
        if (source === 'csv' && file.size > 1 << 20) { // only the first MB, cut at its last newline (UTF-16 has no single-byte newline, so it is cut as is)
          const head = file.slice(0, 1 << 20)
          body = /^utf-?16/i.test(encoding) ? head : head.slice(0, previewLength(new Uint8Array(await head.arrayBuffer())))
        }
        const p = await api<{ columns: string[]; rows: string[][] }>('POST', `${base}/import/preview`, upload({ source, header, delimiter, encoding, null_as: nullAs || undefined, empty_is_null: emptyNull, keep_formula_quote: keepQuote }, body))
        setPrev(p); setErr('')
      } catch (e) { setPrev(null); setErr(msg(e)) }
    }, 250)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [file, source, header, delimiter, encoding, nullAs, emptyNull, keepQuote, create, cols.data])
  useEffect(() => { if (prev) setMapping(autoMap(prev.columns, tcols, match, create)) }, [prev, match, create, cols.data]) // eslint-disable-line react-hooks/exhaustive-deps

  const pick = async (f: File | undefined) => {
    if (!f) return
    setFileName(f.name); setSource(sourceOf(f.name)); setFile(f)
  }
  const start = async (confirmed: boolean) => {
    if (startingRef.current) return
    startingRef.current = true; setStarting(true)
    setErr(''); askNotify()
    try {
      const r = await api<{ job: string }>('POST', `${base}/import`, upload({ source, header, delimiter, encoding, null_as: nullAs || undefined, empty_is_null: emptyNull, keep_formula_quote: keepQuote, createTable: create, ref, mapping: source === 'sql' ? undefined : mapping, confirmed, password }, file!))
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
      desktopNotify(j.error ? `Import failed: ${j.error}` : `Processed ${j.rows} rows`)
      if (!j.error) { notify(`Processed ${j.rows} rows`); onDone() }
    } catch (e) { if (!ac.signal.aborted) setErr(msg(e)) }
  }
  const running = (!!job && !job.done) || starting

  return (
    <Dialog title={`Import into ${ref.Name}`} onClose={() => !running && onClose()}>
      <div className={ROW}><input type="file" aria-label="File" accept=".csv,.tsv,.json,.sql,text/*" onChange={(e) => pick(e.target.files?.[0])} /></div>
      {file && <>
        <div className={ROW}>
          <Select aria-label="Source" value={source} onChange={setSource} options={[{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }, { value: 'sql', label: 'SQL dump' }]} />
          {source !== 'sql' && <label className="flex items-center gap-1.5">Encoding <Select aria-label="Encoding" value={encoding} onChange={setEncoding} options={ENCODINGS} /></label>}
          {source === 'csv' && <>
            <label className="flex items-center gap-1.5"><input type="checkbox" checked={header} onChange={(e) => setHeader(e.target.checked)} />Header row</label>
            <label className="flex items-center gap-1.5">Delimiter <input className="w-12" aria-label="Delimiter" placeholder="," value={delimiter} maxLength={1} onChange={(e) => setDelimiter(e.target.value)} /></label>
          </>}
          {source !== 'sql' && <label className="flex items-center gap-1.5"><input type="checkbox" checked={create} onChange={(e) => setCreate(e.target.checked)} />Create the table</label>}
        </div>
        {source !== 'sql' && <div className={ROW}>
          <label className="flex items-center gap-1.5">NULL as <input className="w-56" aria-label="NULL as" placeholder="(empty: the [csv] null_as setting)" value={nullAs} onChange={(e) => setNullAs(e.target.value)} /></label>
          <label className="flex items-center gap-1.5"><input type="checkbox" checked={emptyNull} onChange={(e) => setEmptyNull(e.target.checked)} />Empty is NULL</label>
          <label className="flex items-center gap-1.5" title="Keep the apostrophe before = + - @ instead of reading it as spreadsheet formula quoting"><input type="checkbox" checked={keepQuote} onChange={(e) => setKeepQuote(e.target.checked)} />Keep formula apostrophe</label>
        </div>}
        {source !== 'sql' && !create && <div className={ROW}><label className="text-ink-2">Match columns by</label><Select aria-label="Match columns by" value={match} onChange={setMatch} options={[{ value: 'both', label: 'Name, then order' }, { value: 'name', label: 'Name (ignore case)' }, { value: 'order', label: 'Order' }]} /></div>}
        {source !== 'sql' && create && <div className="text-ink-2 text-[12px]">The new table gets one TEXT column per file column; choose skip to leave a column out.</div>}
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
