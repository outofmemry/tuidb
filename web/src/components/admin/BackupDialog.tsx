// Backup and restore dialog, with the dump/restore tool paths of the connection.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, download, etags, qs, saveBlob, watchJob, type Connection, type Gate, type Job, type SessionInfo } from '../../utils/api'
import { Dialog, GateFields, confirm } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { askNotify, desktopNotify } from '../../utils/theme'
import { commonBackupOptions, extraArgs, type BackupOption } from '../../utils/backup'
import { enterSubmits } from '../../utils/forms'
import { ACTIONS, ROW, msg } from '../shell/ui'

type Review = { display: string; tool: { Name: string; Path?: string; Found: boolean }; gate?: Gate; levelLabel: string; job?: string; exists?: boolean; create?: { Text: string }[] }

export function BackupDialog({ sid, info, restore, onClose, notify }: { sid: string; info: SessionInfo; restore: boolean; onClose: () => void; notify: (m: string) => void }) {
  const [file, setFile] = useState('')
  const [gzip, setGzip] = useState(false)
  const [format, setFormat] = useState('plain')
  const [dataOnly, setDataOnly] = useState(false)
  const [schemaOnly, setSchemaOnly] = useState(false)
  const [rev, setRev] = useState<Review | null>(null)
  const [newDb, setNewDb] = useState(false), [dbName, setDbName] = useState(''), [enc, setEnc] = useState(''), [col, setCol] = useState('') // restore into a database created on the spot
  const [paths, setPaths] = useState(false)
  const [extra, setExtra] = useState(''), [ticked, setTicked] = useState<string[]>([]), [pattern, setPattern] = useState(restore ? '' : '{database}-{date}.sql') // extra tool options and a file-name pattern; a backup starts from the default name, so Review command works at once
  // a Postgres restore runs psql for a plain .sql file and pg_restore for an archive: once a file is named, ask for that tool's options
  const [pickedFile, setPickedFile] = useState('')
  useEffect(() => { const t = setTimeout(() => setPickedFile(file.trim()), 300); return () => clearTimeout(t) }, [file])
  const byFile = restore && info.driver === 'postgres' ? pickedFile : ''
  const common = useQuery({ queryKey: ['backup-common', sid, restore, byFile, byFile ? format : ''], gcTime: 0, retry: false, placeholderData: (prev) => prev, queryFn: async (): Promise<{ options: BackupOption[]; extra: string[] }> => {
    // the server's list (backup.CommonOptionsFor / AllowedExtraFor); the static copy is only the fallback when the request fails
    try { const r = await api<{ options?: BackupOption[]; extra?: string[] }>('GET', `/sessions/${sid}/backup/options?restore=${restore ? 1 : 0}${byFile ? qs({ file: byFile, format: format === 'custom' ? 'custom' : undefined }).replace(/^\?/, '&') : ''}`); return { options: r.options ?? [], extra: r.extra ?? [] } } catch { return { options: commonBackupOptions(info.driver, restore), extra: [] } }
  } })
  useEffect(() => { // a flag the new tool does not take must not stay ticked
    if (common.data && !common.isPlaceholderData) setTicked((t) => { const keep = t.filter((f) => common.data!.options.some((o) => o.Flag === f)); return keep.length === t.length ? t : keep })
  }, [common.data, common.isPlaceholderData])
  const [job, setJob] = useState<{ id: string; data: Job } | null>(null)
  // what was chosen last time on this connection (remembered by the server, shared with the terminal UI)
  const last = useQuery({ queryKey: ['backup-options', info.name], queryFn: () => api<{ options: Record<string, string> }>('GET', '/state/backup-options' + qs({ conn: info.name })), gcTime: 0 })
  const used = useRef(false)
  useEffect(() => {
    const o = last.data?.options; if (!o || used.current) return
    used.current = true
    const p = restore ? 'restore.' : 'backup.' // the terminal UI's keys
    if (o[p + 'file']) setFile(o[p + 'file'])
    setGzip(o[p + 'gzip'] === 'true'); if (o[p + 'format']) setFormat(o[p + 'format']); setDataOnly(o[p + 'data'] === 'true'); setSchemaOnly(o[p + 'schema'] === 'true')
  }, [last.data])
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  const logEnd = useRef<HTMLPreElement>(null)
  const path = `/sessions/${sid}/${restore ? 'restore' : 'backup'}`
  const opts = info.caps.Dialect === 'postgres' || info.caps.Dialect === 'mysql'
  const body = { File: file, filename: pattern.trim() || undefined, extra: extraArgs(ticked, extra), Gzip: gzip, Format: format, DataOnly: dataOnly, SchemaOnly: schemaOnly, ...(restore && newDb && dbName.trim() ? { NewDatabase: dbName.trim(), CreateOpts: opts ? { encoding: enc.trim(), collation: col.trim() } : undefined } : {}) }
  const call = async (run: boolean) => {
    setErr('')
    if (run && !restore && rev?.exists && !(await confirm('That backup file already exists in the backups folder. Overwrite it?', undefined, 'Overwrite'))) return
    if (run) askNotify()
    try {
      const r = await api<Review>('POST', path, { ...body, Run: run, Confirmed: run, Password: password })
      if (!run) return setRev(r)
      setJob({ id: r.job!, data: { done: false, error: '', lines: [] } as unknown as Job })
    } catch (e) { setErr(msg(e)); if (e instanceof ApiError && e.status === 409 && e.body.gate) setRev((v) => v && { ...v, gate: e.body.gate }) }
  }
  useEffect(() => {
    if (!job?.id) return
    const ac = new AbortController(), id = job.id
    watchJob(sid, id, (d) => setJob({ id, data: d }), ac.signal).then((d) => {
      const m = d.error ? `${restore ? 'Restore' : 'Backup'} failed: ${d.error}` : `${restore ? 'Restore' : 'Backup'} finished`
      notify(m); desktopNotify(m)
      api<Job>('GET', `/sessions/${sid}/jobs/${id}`).then((j) => j.file && setJob({ id, data: { ...d, file: j.file } })).catch(() => {}) // the finished job names its file
    }).catch((e) => { if (!ac.signal.aborted) setErr(msg(e)) })
    return () => ac.abort()
  }, [job?.id])
  useEffect(() => { logEnd.current?.scrollTo(0, logEnd.current.scrollHeight) }, [job?.data.lines.length])
  const running = !!job && !job.data.done
  return (
    <Dialog title={restore ? 'Restore database' : 'Back up database'} onClose={running ? () => {} : onClose}>
      {!job && (
        <>
          <div className={ROW}><input autoFocus className="flex-1" onKeyDown={(e) => enterSubmits(e, () => { if (rev?.display && rev.tool.Found) { if (!rev.gate?.password || password) call(true) } else if (file || pattern.trim()) call(false) })} placeholder={restore ? 'name of the file to restore' : 'name of the backup file'} title="Files live in the backups folder of the data directory" aria-label="File path" value={file} onChange={(e) => { setFile(e.target.value); setRev(null) }} /></div>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px]">
            <label className="flex items-center gap-1.5"><input type="checkbox" checked={gzip} onChange={(e) => { setGzip(e.target.checked); setRev(null) }} />gzip</label>
            {info.driver === 'postgres' && <Select className="bg-raised border-transparent" aria-label="Format" value={format} onChange={(v) => { setFormat(v); setRev(null) }} options={['plain', 'custom']} />}
            {!restore && <label className="flex items-center gap-1.5"><input type="checkbox" checked={dataOnly} onChange={(e) => { setDataOnly(e.target.checked); setRev(null) }} />data only</label>}
            {!restore && <label className="flex items-center gap-1.5"><input type="checkbox" checked={schemaOnly} onChange={(e) => { setSchemaOnly(e.target.checked); setRev(null) }} />schema only</label>}
          </div>
          {restore && <div className="mt-2 text-[12.5px]">
            <label className="flex items-center gap-1.5"><input type="checkbox" checked={newDb} onChange={(e) => { setNewDb(e.target.checked); setRev(null) }} />Restore into a new database, created first</label>
            {newDb && <div className="flex flex-wrap gap-2 mt-1.5 pl-5"><input aria-label="New database name" placeholder="new database name" value={dbName} onChange={(e) => { setDbName(e.target.value); setRev(null) }} />
              {opts && <><input aria-label="Encoding" placeholder="encoding (optional)" value={enc} onChange={(e) => { setEnc(e.target.value); setRev(null) }} /><input aria-label="Collation" placeholder="collation (optional)" value={col} onChange={(e) => { setCol(e.target.value); setRev(null) }} /></>}</div>}
          </div>}
          <div className={ROW}><label className="w-28 text-ink-2" htmlFor="backup-pattern">File name pattern</label><input id="backup-pattern" className="flex-1 min-w-0" placeholder="{database}-{date}.sql" title="Tokens: {connection} {database} {table} {tag} {date} {time}. Used instead of the name above when filled." value={pattern} onChange={(e) => { setPattern(e.target.value); setRev(null) }} /></div>
          {!!common.data?.options.length && <fieldset className="border border-rule px-2.5 py-1.5 my-2 min-w-0"><legend className="text-ink-2 text-[12px] px-1">Common options</legend>
            {restore && info.driver === 'postgres' && !byFile && <div className="text-ink-3 text-[11.5px] mb-1">Pick a file to see its tool's options (psql for .sql, pg_restore for an archive).</div>}
            <div className="flex flex-col gap-0.5 text-[12.5px]">{common.data.options.map((o) => <label key={o.Flag} className="flex items-center gap-1.5" title={o.Desc}><input type="checkbox" checked={ticked.includes(o.Flag)} onChange={(e) => { setTicked(e.target.checked ? [...ticked, o.Flag] : ticked.filter((x) => x !== o.Flag)); setRev(null) }} /><code>{o.Flag}</code><span className="text-ink-3">{o.Desc}</span></label>)}</div></fieldset>}
          <div className={ROW}><label className="w-28 text-ink-2" htmlFor="backup-extra">Extra options</label><input id="backup-extra" className="flex-1 min-w-0 font-mono" placeholder="--no-comments --lock-wait-timeout=5" value={extra} onChange={(e) => { setExtra(e.target.value); setRev(null) }} /></div>
          {!!common.data?.extra.length && <div className="text-ink-3 text-[11.5px] ml-28 pl-1 wrap-anywhere">Also accepted: {common.data.extra.join(' ')}</div>}
          {rev && (
            <div className="mt-3">
              {rev.create && rev.create.length > 0 && <><div className="text-ink-2 mb-1">First this is sent to the server:</div><pre>{rev.create.map((c) => c.Text).join('\n')}</pre></>}
              {!rev.tool.Found && <div className="text-del">{rev.tool.Name || 'The backup tool'} {rev.tool.Path ? `was not found at ${rev.tool.Path}` : 'is not on PATH'}. Install it, or set its path.</div>}
              {rev.tool.Found && rev.tool.Path && <div className="text-ink-3 text-[12px] mb-1">Tool: {rev.tool.Path}</div>}
              {rev.display && <><div className="text-ink-2 mb-1">This will run:</div><pre>{rev.display}</pre></>}
              {rev.display && rev.gate && <GateFields gate={rev.gate} label={rev.levelLabel} password={password} setPassword={setPassword} />}
            </div>
          )}
          {err && <div className="text-del mt-2">{err}</div>}
          <div className={ACTIONS}>
            <button className="mr-auto" onClick={() => setPaths(true)} title="Where the dump and restore programs are, for this connection">Tool paths…</button>
            <button onClick={onClose}>Cancel</button>
            {!rev?.display || !rev.tool.Found ? <button className="btn-primary" disabled={(!file && !pattern.trim()) || (restore && newDb && !dbName.trim())} onClick={() => call(false)}>Review command</button>
              : <button className="btn-primary" disabled={!!rev.gate?.password && !password} onClick={() => call(true)}>{restore ? 'Restore' : 'Back up'}</button>}
          </div>
        </>
      )}
      {job && (
        <>
          <pre ref={logEnd} className="min-h-[8em] max-h-[40vh] overflow-auto" role="log" aria-live="polite" aria-label="Output">{job.data.lines.join('\n') || (job.data.done ? '(no output)' : 'Starting…')}</pre>
          {job.data.error && <div className="text-del mt-2">{job.data.error}</div>}
          {job.data.done && !job.data.error && <div className="mt-2">Done.</div>}
          {err && <div className="text-del mt-2">{err}</div>}
          <div className={ACTIONS}>
            {running ? <button className="btn-danger" onClick={() => api('POST', `/sessions/${sid}/jobs/${job.id}/cancel`).catch((e) => setErr(msg(e)))}>Cancel</button> : <>
              {job.data.file && !job.data.error && <button onClick={async () => { try { const f = await download(`/sessions/${sid}/jobs/${job.id}/file`); saveBlob(f.blob, f.name) } catch (e) { setErr(msg(e)) } }}>Download {job.data.file}</button>}
              <button className="btn-primary" autoFocus onClick={onClose}>Close</button></>}
          </div>
        </>
      )}
      {paths && <ToolPathsDialog info={info} onClose={() => setPaths(false)} onSaved={() => { setPaths(false); setRev(null) }} />}
    </Dialog>
  )
}

// Where this connection's dump and restore programs are, instead of looking them up on PATH. Saved on the connection.
const TOOLS: Record<string, string[]> = { postgres: ['pg_dump', 'pg_restore', 'psql'], mysql: ['mysqldump', 'mysql'], mariadb: ['mariadb-dump', 'mariadb', 'mysqldump', 'mysql'], clickhouse: ['clickhouse-client'], mongo: ['mongodump', 'mongorestore'], redis: ['redis-cli'] }

function ToolPathsDialog({ info, onClose, onSaved }: { info: SessionInfo; onClose: () => void; onSaved: () => void }) {
  const conns = useQuery({ queryKey: ['conns-tools'], queryFn: () => api<Connection[]>('GET', '/connections'), gcTime: 0 })
  const [vals, setVals] = useState<Record<string, string>>({}), [err, setErr] = useState('')
  const mine = conns.data?.find((c) => c.Name === info.name)
  const names = TOOLS[info.driver] ?? []
  useEffect(() => { if (mine) setVals({ ...mine.Tools }) }, [mine?.Name])
  const save = async () => {
    const tools = Object.fromEntries(Object.entries(vals).filter(([k, v]) => names.includes(k) && v.trim()).map(([k, v]) => [k, v.trim()]))
    try {
      await api('PUT', '/connections', conns.data!.map((c) => (c.Name === info.name ? { ...c, Tools: Object.keys(tools).length ? tools : undefined } : c)), undefined, etags['/connections'] ? { 'If-Match': etags['/connections'] } : undefined)
      onSaved()
    } catch (e) { setErr(msg(e)) }
  }
  return (
    <Dialog title={`Tool paths for ${info.name}`} onClose={onClose}>
      {conns.isLoading && <div className="text-ink-2">Loading…</div>}
      {mine?.Source && <div className="text-ink-2">This connection comes from a connection source file: edit that file to set tool paths.</div>}
      {mine && !mine.Source && <>
        {names.length === 0 && <div className="text-ink-2">{info.driver} needs no dump program.</div>}
        {names.map((n) => <div key={n} className={ROW}><label className="w-40 text-ink-2 font-mono">{n}</label><input className="flex-1 min-w-0 font-mono" aria-label={`Path of ${n}`} placeholder="empty = look it up on PATH" value={vals[n] ?? ''} onChange={(e) => setVals({ ...vals, [n]: e.target.value })} /></div>)}
        <p className="text-ink-3 text-[12px]">An absolute path to the program, named like it (pg_dump, pg_dump-16). The server runs it, so it must be a dump tool.</p>
      </>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={!mine || !!mine.Source || !names.length} onClick={save}>Save</button></div>
    </Dialog>
  )
}
