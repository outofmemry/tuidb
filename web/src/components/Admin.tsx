// Milestone 3 dialogs: users, backup / restore, plugins, MCP activity.
import { Fragment, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, download, etags, qs, saveBlob, watchJob, type Connection, type DbUser, type Gate, type Job, type ObjRef, type Plan, type PluginEffect, type PluginInfo, type SessionInfo } from '../utils/api'
import { Dialog, GateFields, ask, confirm } from './Dialogs'
import { copyText } from '../utils/copy'
import { Select } from './Select'
import { askNotify, desktopNotify } from '../utils/theme'
import { commonBackupOptions, enterSubmits, extraArgs, limitHint, type BackupOption } from '../utils/m13'

const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
const ROW = 'flex items-center gap-2.5 my-2 text-[12.5px]'
const TH = 'text-left p-1 border-b border-rule-2 font-medium text-ink-2'
const TD = 'p-1 border-b border-rule align-top'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ---- users ---------------------------------------------------------------------------------

const PG_ATTRS = ['SUPERUSER', 'CREATEDB', 'CREATEROLE', 'LOGIN', 'REPLICATION', 'BYPASSRLS']
const PG_PRIVS = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'TRUNCATE', 'REFERENCES', 'TRIGGER']
const MY_PRIVS = ['SELECT', 'INSERT', 'UPDATE', 'DELETE', 'CREATE', 'DROP', 'ALTER', 'INDEX', 'ALL']

type Form = { kind: 'create' | 'password' | 'attrs' | 'grant' | 'revoke'; user?: DbUser }

export function UsersDialog({ sid, info, onPlan, onClose }: { sid: string; info: SessionInfo; onPlan: (p: Plan) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['users', sid], queryFn: () => api<{ supported: boolean; users: DbUser[] }>('GET', `/sessions/${sid}/users`), gcTime: 0 })
  const [form, setForm] = useState<Form | null>(null)
  const [name, setName] = useState('')
  const [host, setHost] = useState('%')
  const [pw, setPw] = useState('')
  const [picked, setPicked] = useState<string[]>([])
  const [object, setObject] = useState('')
  const [columns, setColumns] = useState('') // grant / revoke on just these columns (SELECT, INSERT, UPDATE, REFERENCES)
  const [err, setErr] = useState('')
  const pg = info.driver === 'postgres'
  const choices = pg ? PG_ATTRS : MY_PRIVS
  const open = (f: Form) => { setForm(f); setErr(''); setPw(''); setObject(''); setColumns(''); setPicked(f.kind === 'attrs' && pg ? f.user?.Attrs ?? [] : []); if (f.user) setName(f.user.Name) }
  const plan = async (body: Record<string, unknown>) => {
    setErr('')
    try { const p = await api<Plan>('POST', `/sessions/${sid}/users/plan`, body); onClose(); onPlan(p) } catch (e) { setErr(msg(e)) }
  }
  const who = (u: DbUser) => ({ Name: u.Name, Host: u.Host })
  const submit = () => {
    if (!form) return
    const u = form.user
    if (form.kind === 'create') return plan({ Kind: 'create', User: { Name: name, Host: pg ? '' : host, Attrs: pg ? picked : [] }, Password: pw })
    if (!u) return
    if (form.kind === 'password') return plan({ Kind: 'password', User: who(u), Password: pw })
    if (form.kind === 'attrs') {
      // PostgreSQL turns a flag off with NO<flag>; MySQL only adds global privileges
      const had = u.Attrs ?? []
      const attrs = pg ? [...picked.filter((a) => !had.includes(a)), ...had.filter((a) => !picked.includes(a)).map((a) => 'NO' + a)] : picked
      return plan({ Kind: 'attrs', User: { ...who(u), Attrs: attrs } })
    }
    const cols = columns.split(',').map((c) => c.trim()).filter(Boolean)
    return plan({ Kind: form.kind, User: who(u), Grant: { Object: object, Privs: picked, Columns: cols.length ? cols : undefined } })
  }
  const drop = async (u: DbUser) => {
    const t = await ask(`Type ${u.Name} to drop this ${pg ? 'role' : 'account'}`)
    if (t === null) return
    if (t !== u.Name) return setErr('The name did not match; nothing dropped.')
    plan({ Kind: 'drop', User: who(u) })
  }
  const toggle = (a: string) => setPicked((p) => (p.includes(a) ? p.filter((x) => x !== a) : [...p, a]))
  const privs = form?.kind === 'grant' || form?.kind === 'revoke' ? (pg ? PG_PRIVS : MY_PRIVS) : choices
  return (
    <Dialog title="Users" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Loading…</div>}
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !q.data.supported && <div className="text-ink-2">User management is not supported by {info.driver}.</div>}
      {q.data?.supported && (
        <>
          <div className="overflow-auto max-h-[36vh] border border-rule">
            <table className="text-[12px] font-mono border-collapse w-full">
              <thead><tr>{['User', pg ? 'Attributes' : 'Global privileges', 'Grants', ''].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
              <tbody>{q.data.users.map((u) => (
                <tr key={u.Name + '@' + u.Host}>
                  <td className={`${TD} whitespace-nowrap`}>{u.Name}{u.Host && <span className="text-ink-2">@{u.Host}</span>}</td>
                  <td className={TD}>{(u.Attrs ?? []).join(', ')}</td>
                  <td className={TD}>{(u.Grants ?? []).map((g, k) => <div key={g.Object + k}>{g.Object}: <span className="text-ink-2">{g.Columns?.length ? g.Privs.map((x) => `${x} (${g.Columns!.join(', ')})`).join(', ') : g.Privs.join(', ')}</span></div>)}</td>
                  <td className={`${TD} whitespace-nowrap`}>
                    <button className="btn-ghost" onClick={() => open({ kind: 'password', user: u })}>Password</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'attrs', user: u })}>{pg ? 'Attributes' : 'Privileges'}</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'grant', user: u })}>Grant</button>
                    <button className="btn-ghost" onClick={() => open({ kind: 'revoke', user: u })}>Revoke</button>
                    <button className="btn-ghost btn-danger" onClick={() => drop(u)}>Drop</button>
                  </td>
                </tr>))}</tbody>
            </table>
          </div>
          {!form && <div className={ACTIONS}><button onClick={() => open({ kind: 'create' })}>New user</button></div>}
          {form && (
            <div className="mt-3 border-t border-rule pt-2">
              <strong className="font-medium">{form.kind === 'create' ? 'New user' : `${form.kind[0].toUpperCase() + form.kind.slice(1)}: ${form.user!.Name}`}</strong>
              {form.kind === 'create' && <div className={ROW}><input autoFocus placeholder="name" aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} />{!pg && <input placeholder="host" aria-label="Host" value={host} onChange={(e) => setHost(e.target.value)} />}</div>}
              {(form.kind === 'create' || form.kind === 'password') && <div className={ROW}><input type="password" placeholder="password" aria-label="Password" value={pw} onChange={(e) => setPw(e.target.value)} /></div>}
              {(form.kind === 'grant' || form.kind === 'revoke') && <div className={ROW}><input autoFocus className="flex-1" placeholder={pg ? 'schema.table' : 'db.table or db.*'} aria-label="Object" value={object} onChange={(e) => setObject(e.target.value)} /></div>}
              {(form.kind === 'grant' || form.kind === 'revoke') && <div className={ROW}><input className="flex-1" placeholder="columns, comma separated (optional: only for SELECT, INSERT, UPDATE, REFERENCES)" aria-label="Columns" value={columns} onChange={(e) => setColumns(e.target.value)} /></div>}
              {(form.kind === 'attrs' || form.kind === 'grant' || form.kind === 'revoke' || (form.kind === 'create' && pg)) && (
                <div className="flex flex-wrap gap-x-4 gap-y-1 my-2 text-[12.5px]">{privs.map((a) => <label key={a} className="flex items-center gap-1.5"><input type="checkbox" checked={picked.includes(a)} onChange={() => toggle(a)} />{a}</label>)}</div>
              )}
              {err && <div className="text-del">{err}</div>}
              <div className={ACTIONS}><button onClick={() => setForm(null)}>Cancel</button><button className="btn-primary" onClick={submit}>Preview</button></div>
            </div>
          )}
        </>
      )}
      {!form && err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}

// ---- backup / restore -------------------------------------------------------------------------

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
  const body = { File: file, Gzip: gzip, Format: format, DataOnly: dataOnly, SchemaOnly: schemaOnly, ...(restore && newDb && dbName.trim() ? { NewDatabase: dbName.trim(), CreateOpts: opts ? { encoding: enc.trim(), collation: col.trim() } : undefined } : {}) }
  const call = async (run: boolean) => {
    setErr('')
    if (run && !restore && rev?.exists && !(await confirm('That backup file already exists in the backups folder. Overwrite it?'))) return
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
    }).catch((e) => { if (!ac.signal.aborted) setErr(msg(e)) })
    return () => ac.abort()
  }, [job?.id])
  useEffect(() => { logEnd.current?.scrollTo(0, logEnd.current.scrollHeight) }, [job?.data.lines.length])
  const running = !!job && !job.data.done
  return (
    <Dialog title={restore ? 'Restore database' : 'Back up database'} onClose={running ? () => {} : onClose}>
      {!job && (
        <>
          <div className={ROW}><input autoFocus className="flex-1" placeholder={restore ? 'name of the file to restore' : 'name of the backup file'} title="Files live in the backups folder of the data directory" aria-label="File path" value={file} onChange={(e) => { setFile(e.target.value); setRev(null) }} /></div>
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
            {!rev?.display || !rev.tool.Found ? <button className="btn-primary" disabled={!file || (restore && newDb && !dbName.trim())} onClick={() => call(false)}>Review command</button>
              : <button className="btn-primary" disabled={!!rev.gate?.password && !password} onClick={() => call(true)}>{restore ? 'Restore' : 'Back up'}</button>}
          </div>
        </>
      )}
      {job && (
        <>
          <pre ref={logEnd} className="min-h-[8em]">{job.data.lines.join('\n') || (job.data.done ? '(no output)' : 'Starting…')}</pre>
          {job.data.error && <div className="text-del mt-2">{job.data.error}</div>}
          {job.data.done && !job.data.error && <div className="mt-2">Done.</div>}
          {err && <div className="text-del mt-2">{err}</div>}
          <div className={ACTIONS}>
            {running ? <button className="btn-danger" onClick={() => api('DELETE', `/sessions/${sid}/jobs/${job.id}`).catch(() => {})}>Cancel</button> : <button className="btn-primary" onClick={onClose}>Close</button>}
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

// ---- plugins -----------------------------------------------------------------------------------

export type PluginCtx = { context: string; object?: ObjRef; selection: string }

export function PluginsDialog({ sid, ctx, onClose, onSql, notify }: { sid: string; ctx: PluginCtx; onClose: () => void; onSql: (t: string) => void; notify: (m: string) => void }) {
  const q = useQuery({ queryKey: ['plugins'], queryFn: () => api<PluginInfo[]>('GET', '/plugins'), gcTime: 0 })
  const [out, setOut] = useState<{ name: string; text: string } | null>(null)
  const [busy, setBusy] = useState('')
  const list = (q.data ?? []).filter((p) => !p.error && (!ctx.context || p.contexts.length === 0 || p.contexts.includes(ctx.context)))
  const run = async (p: PluginInfo) => {
    setBusy(p.name)
    try {
      // shortcut: the current cell is whatever text is selected in the grid; a focused-cell API is the upgrade
      const r = await api<{ output: string; text: string }>('POST', `/sessions/${sid}/plugins/run`, { Name: p.name, Context: ctx.context, Object: ctx.object, Selection: ctx.selection, Cell: ctx.context === 'grid' ? ctx.selection : '' })
      if (r.output === 'sql') { onSql(r.text); onClose() } else setOut({ name: p.name, text: r.text })
    } catch (e) { notify(msg(e)); setBusy('') }
  }
  return (
    <Dialog title={out ? out.name : 'Plugins'} onClose={onClose}>
      {out ? <pre>{out.text || '(no output)'}</pre> : (
        <>
          {q.isLoading && <div className="text-ink-2">Loading…</div>}
          {q.error && <div className="text-del">{q.error.message}</div>}
          {q.data && list.length === 0 && <div className="text-ink-2">No plugins for this place. Add a .toml file to the plugins folder in the config directory.</div>}
          <div className="flex flex-col gap-0.5">{list.map((p) => <button key={p.name} className="text-left border-transparent hover:border-ink-3" disabled={!!busy} onClick={() => run(p)}>{p.name} <span className="text-ink-2">{busy === p.name ? 'running…' : p.output}</span></button>)}</div>
        </>
      )}
      <div className={ACTIONS}>{out && <button onClick={() => setOut(null)}>Back</button>}<button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}

// ---- MCP activity ----------------------------------------------------------------------------------

export function McpDialog({ onClose }: { onClose: () => void }) {
  const q = useQuery({ queryKey: ['mcplog'], queryFn: () => api<{ enabled: boolean; port?: number; listen?: string; tls?: boolean; entries: { time: string; token: string; tool: string; connection: string; sql: string; error: string }[] }>('GET', '/mcp/log'), gcTime: 0, refetchInterval: 3000 })
  const cl = useQuery({ queryKey: ['mcpclients'], queryFn: () => api<{ enabled: boolean; source: string; clients: { token: string; lastSeen: string; calls: number }[] }>('GET', '/mcp/clients'), gcTime: 0, refetchInterval: 3000 })
  const [err, setErr] = useState('')
  const d = q.data
  const csv = async () => { try { const f = await download('/mcp/log?format=csv'); saveBlob(f.blob, f.name) } catch (e) { setErr(msg(e)) } }
  return (
    <Dialog title="MCP activity" onClose={onClose}>
      {q.error && <div className="text-del">{q.error.message}</div>}
      {d && !d.enabled && <div className="text-ink-2">The MCP server is off. Set enabled = true in the [mcp] section of the config and restart.</div>}
      {d?.enabled && (
        <>
          <div className="flex items-center gap-3 text-ink-2 mb-2"><span className="flex-1 min-w-0">Listening on {d.listen ?? `127.0.0.1:${d.port}`} over {d.tls ? 'HTTPS (TLS)' : 'plain HTTP'}</span><button onClick={csv}>Export log as CSV</button></div>
          <h3 className="m-0 mt-3 mb-1 text-[12.5px] text-ink-2 font-medium">Connected clients</h3>
          {cl.data?.clients.length ? (
            <table className="text-[12px] font-mono border-collapse w-full mb-2">
              <thead><tr>{['Token', 'Requests', 'Last seen'].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
              <tbody>{cl.data.clients.map((c) => <tr key={c.token}><td className={TD}>{c.token}</td><td className={TD}>{c.calls}</td><td className={TD}>{new Date(c.lastSeen).toLocaleTimeString()}</td></tr>)}</tbody>
            </table>
          ) : <div className="text-ink-2 mb-2">No client called a tool in the last 10 minutes.</div>}
          <div className="text-ink-3 text-[12px] mb-2">Tools a client can call: list_connections, list_open_connections, open_connection, disconnect, execute_read_query, execute_query, list_saved_queries, get_saved_query, list_history, list_boards, run_board.</div>
          <div className="text-ink-3 text-[12px] mb-2">A client is a token seen in the activity log ({cl.data?.source ?? 'last 10 minutes'}). Calls are separate HTTP requests, so there is no session to disconnect: to cut a client off, remove its token from [mcp] in the config and restart.</div>
          <h3 className="m-0 mt-3 mb-1 text-[12.5px] text-ink-2 font-medium">Activity</h3>
          {d.entries.length === 0 ? <div className="text-ink-2">No tool calls yet.</div> : (
            <div className="overflow-auto max-h-[40vh] border border-rule">
              <table className="text-[12px] font-mono border-collapse w-full">
                <thead><tr>{['Time', 'Token', 'Tool', 'Connection', 'SQL / error'].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
                <tbody>{[...d.entries].reverse().map((e, i) => (
                  <tr key={i}><td className={TD}>{e.time}</td><td className={TD}>{e.token}</td><td className={TD}>{e.tool}</td><td className={TD}>{e.connection}</td>
                    <td className={`${TD} max-w-[40ch] whitespace-pre-wrap break-words`}>{e.sql}{e.error && <div className="text-del">{e.error}</div>}</td></tr>))}</tbody>
              </table>
            </div>
          )}
        </>
      )}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}
