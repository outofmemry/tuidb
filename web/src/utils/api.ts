// Types use the Go field names of the db/config structs (the API serializes them as-is).
const KEY = 'webtuidb-token'

// The server prints http://host/#token=...; the fragment never reaches the server or logs.
export function initToken() {
  const m = location.hash.match(/token=([^&]+)/)
  if (!m) return
  try { sessionStorage.setItem(KEY, m[1]) } catch { /* private mode: token lives only until reload */ }
  memToken = m[1]
  history.replaceState(null, '', location.pathname + location.search)
}
let memToken = ''
function token() {
  try { return sessionStorage.getItem(KEY) ?? memToken } catch { return memToken }
}

export class ApiError extends Error {
  constructor(public status: number, message: string, public body: any) { super(message) }
}

// Requests that run user queries are tracked, so cancel_queries can abort whatever is in flight.
const inflight = new Set<AbortController>()
const CANCELLABLE = /^\/sessions\/[^/]+\/(exec|rows|count|explain)(\/|$)/
export const inflightCount = () => inflight.size
export function cancelInflight() { const n = inflight.size; inflight.forEach((c) => c.abort()); inflight.clear(); return n }

// ETag of the last GET per path (the connection list's version: PUT /connections sends it back as If-Match).
export const etags: Record<string, string> = {}

export async function api<T = any>(method: string, path: string, body?: unknown, signal?: AbortSignal, headers?: Record<string, string>): Promise<T> {
  let own: AbortController | undefined
  if (method === 'POST' && CANCELLABLE.test(path)) {
    own = new AbortController(); inflight.add(own)
    signal?.addEventListener('abort', () => own!.abort())
    signal = own.signal
  }
  try { return await send<T>(method, path, body, signal, headers) } finally { if (own) inflight.delete(own) }
}

async function send<T>(method: string, path: string, body?: unknown, signal?: AbortSignal, headers?: Record<string, string>): Promise<T> {
  const res = await fetch('/api' + path, {
    method, signal,
    headers: { Authorization: 'Bearer ' + token(), ...(body instanceof FormData ? {} : { 'Content-Type': 'application/json' }), ...headers },
    body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body),
  })
  const tag = res.headers.get('ETag')
  if (tag && method === 'GET') etags[path] = tag
  const text = await res.text() // an aborted or truncated body throws here instead of becoming {}
  let data: any = {}
  if (text) { try { data = JSON.parse(text) } catch (e) { if (res.ok) throw e } }
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText, data)
  return data as T
}

// stream sends a request whose answer is a Server-Sent Events stream and calls onEvent for each event; EventSource cannot
// carry the bearer token, so this reads the body itself. An error before the stream starts is the usual JSON error (ApiError).
export async function stream(method: string, path: string, body: unknown, onEvent: (name: string, data: any) => void, signal?: AbortSignal): Promise<void> {
  let own: AbortController | undefined
  if (method === 'POST' && CANCELLABLE.test(path)) { own = new AbortController(); inflight.add(own); signal?.addEventListener('abort', () => own!.abort()); signal = own.signal }
  try {
    const res = await fetch('/api' + path, { method, signal, headers: { Authorization: 'Bearer ' + token(), 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
    if (!res.ok) {
      const text = await res.text(); let data: any = {}
      try { data = JSON.parse(text) } catch { /* not JSON */ }
      throw new ApiError(res.status, data.error ?? res.statusText, data)
    }
    const rd = res.body!.getReader(), dec = new TextDecoder()
    let buf = ''
    for (;;) {
      const { value, done } = await rd.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      for (let at = buf.indexOf('\n\n'); at >= 0; at = buf.indexOf('\n\n')) {
        const block = buf.slice(0, at); buf = buf.slice(at + 2)
        const name = /^event: (.*)$/m.exec(block)?.[1] ?? 'message', data = /^data: (.*)$/m.exec(block)?.[1]
        if (data !== undefined) onEvent(name, JSON.parse(data))
      }
    }
  } finally { if (own) inflight.delete(own) }
}

// decodeName undoes a percent-encoded file name; a raw name with a stray % (100%.csv) is used as it is.
function decodeName(n: string) { try { return decodeURIComponent(n) } catch { return n } }

// download posts a request (a GET when there is no body) whose answer is a file and returns it with the name the server chose.
export async function download(path: string, body?: unknown): Promise<{ blob: Blob; name: string }> {
  const res = await fetch('/api' + path, { method: body === undefined ? 'GET' : 'POST', headers: { Authorization: 'Bearer ' + token(), 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body) })
  if (!res.ok) {
    let msg = res.statusText
    try { msg = (await res.json()).error ?? msg } catch { /* not JSON */ }
    throw new ApiError(res.status, msg, {})
  }
  const m = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(res.headers.get('Content-Disposition') ?? '')
  return { blob: await res.blob(), name: m ? decodeName(m[1]) : 'export' }
}

// beacon sends a DELETE that survives the page going away (pagehide), where a normal fetch is cancelled.
export function beacon(path: string) {
  try { fetch('/api' + path, { method: 'DELETE', keepalive: true, headers: { Authorization: 'Bearer ' + token(), 'Content-Type': 'application/json' } }).catch(() => {}) } catch { /* nothing to do */ }
}

// postQuery saves a query; when the name is taken it asks before replacing it (the server answers 409 {exists}).
export async function postQuery(path: string, body: Record<string, unknown>) {
  try { return await api('POST', path, body) } catch (e) {
    if (e instanceof ApiError && e.status === 409 && e.body.exists && confirm(`A saved query "${body.Name}" already exists. Replace it?`)) return api('POST', path, { ...body, Overwrite: true })
    throw e
  }
}

// upload builds the multipart body the import endpoints take: the `options` JSON first, then the raw file.
export function upload(options: unknown, file: Blob): FormData {
  const f = new FormData()
  f.append('options', JSON.stringify(options)); f.append('file', file)
  return f
}

export function saveBlob(blob: Blob, name: string) {
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob); a.download = name; a.click()
  setTimeout(() => URL.revokeObjectURL(a.href), 1000)
}

export const qs = (o: Record<string, string | number | undefined>) =>
  '?' + Object.entries(o).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`).join('&')

export type SSH = { Host: string; Port: number; User: string; Key: string; Password: string; ProxyJump?: string }
export type Connection = {
  Name: string; Driver: string; Host: string; Port?: number; User?: string; Password?: string; PasswordCmd?: string
  Database?: string; Tag?: string; Color?: string; SafeMode?: string; Group?: string; SSLMode?: string; Encoding?: string
  Options?: Record<string, string>; SSH?: SSH | null
  ReadOnly?: boolean; Source?: string; Open?: OpenTarget | null; Tools?: Record<string, string>
}
export type OpenTarget = { Schema: string; Table: string; Column: string; Operation: string; Value: string; Condition: string }
export type Caps = {
  Schemas: boolean; Indexes: boolean; Triggers: boolean; ForeignKeys: boolean; Views: boolean; MatViews: boolean
  Functions: boolean; Sequences: boolean; Transactions: boolean; Document: boolean; Editor: string; Dialect: string
  DatabaseNoun: string; Types: string[] | null; ReorderColumns?: boolean; ProcessList?: boolean
}
export type SessionInfo = {
  id: string; name: string; driver: string; caps: Caps; serverInfo: string; database: string; schema: string
  level: string; levelLabel: string; rowLimit: number; color: string; tag: string; encoding?: string
  readOnly?: boolean; open?: OpenTarget | null; pinnable?: boolean
  host?: string; port?: number; user?: string; sslMode?: string; ssh?: boolean
}
export type ObjRef = { Schema: string; Name: string; Kind: string }
export type DbObject = ObjRef & { Detail: string }
export type Column = { Name: string; Type: string; Nullable: boolean; Default: string; HasDef: boolean; PK: boolean; Extra: string; FK: { RefSchema: string; RefTable: string; RefColumns: string[] } | null }
export type Filter = { Column: string; Op: string; Value: string; Enabled: boolean }
export type FilterSet = { Filters: Filter[]; Or: boolean }
export type Sort = { Column: string; Desc: boolean }
export type ResultCol = { name: string; type: string; pk?: boolean }
export type ResultData = {
  resultId?: string; columns: ResultCol[]; cells: (string | null)[][]; docs?: string[]; affected: number
  hasRows: boolean; message?: string; durationMs: number; truncated: boolean; editable: boolean
}
export type ExecItem = ResultData & { stmt: string; label?: string; error?: string; noticeId?: string; pageSize?: number }
export type Meta = {
  drivers: string[]; sslModes: string[]; safeModes: string[]; operators: Record<string, string[]>
  config: { general: Record<string, any>; editor: Record<string, any>; table: Record<string, any>; sidebar: Record<string, any>; csv: { Delimiter: string; Quote: string; LineBreak: string; Decimal: string; NullAs?: string; QuoteMode?: string } }
  theme: { Name: string; Colors: Record<string, string> | null }
  web?: { fontSize: number; lineHeight: string; notifications: boolean }
  keymap: import('./keys').Keymap
  fileBased?: string[]; queriesDir?: string; tags?: { name: string; color: string }[]
  mcp?: { enabled: boolean; listen: string; tls: boolean }; export?: { filename: string }; ai?: { provider: string; model: string; providers?: string[] }; csvWarning?: string
  login?: boolean
}
export type Job = { done: boolean; error: string; rows: number; batches: number; total: number; checked: number; hits: { ref: ObjRef; count: number; filters: FilterSet; error?: string }[]; lines: string[] }
// watchJob follows a background job over its event stream (progress, line, done) and calls onSnap with the whole state after each event;
// when the stream breaks it polls the job instead, so a proxy that buffers event streams only costs the live updates.
export async function watchJob(sid: string, id: string, onSnap: (j: Job) => void, signal?: AbortSignal): Promise<Job> {
  const j: Job = { done: false, error: '', rows: 0, batches: 0, total: 0, checked: 0, hits: [], lines: [] }
  const emit = () => onSnap({ ...j, lines: [...j.lines] })
  try {
    await stream('GET', `/sessions/${sid}/jobs/${id}/events`, undefined, (ev, d) => {
      if (ev === 'progress') Object.assign(j, d)
      else if (ev === 'line') j.lines.push(d.line)
      else if (ev === 'done') { j.done = true; j.error = d.error ?? '' }
      emit()
    }, signal)
  } catch (e) {
    if (signal?.aborted || (e instanceof ApiError && e.status === 404)) throw e
  }
  while (!j.done) { // the stream ended early: poll
    if (signal?.aborted) throw new DOMException('Aborted', 'AbortError')
    Object.assign(j, await api<Job>('GET', `/sessions/${sid}/jobs/${id}`, undefined, signal)); emit()
    if (!j.done) await new Promise((r) => setTimeout(r, 500))
  }
  return j
}
export type Gate = { confirm: boolean; password: boolean }
export type Plan = { planId: string; statements: { display: string; readOnly: boolean }[]; gate: Gate; transactions: boolean; levelLabel: string }
export type Special = { $: 'null' | 'default' | 'empty' } | { $expr: string; $advanced?: 'true' }
export type Val = string | null | Special

export const keyOf = (r: ObjRef) => (r.Schema ? r.Schema + '.' + r.Name : r.Name) // sidebar pins, like the TUI
export const recentKey = (r: ObjRef) => r.Schema + '.' + r.Name // recents and table state, like the TUI
export const sameRef = (a: ObjRef, b: ObjRef) => a.Schema === b.Schema && a.Name === b.Name && a.Kind === b.Kind

// clip shortens long values for one-line places (toasts, the changeset, tooltips); the popover shows the whole thing.
export const clip = (s: string, n = 120) => (s.length > n ? s.slice(0, n - 1).replace(/\s+\S*$/, '') + '…' : s)

// ---- milestone 3 (Board, Var, Panel, User and Grant use the Go field names, like the rest) ----
export type DbUser = { Name: string; Host: string; Attrs: string[] | null; Grants: { Object: string; Privs: string[]; Columns?: string[] | null }[] | null }
export type BoardVar = { Name: string; Label: string; Default: string; Options: string[] | null }
export type BoardPanel = { Title: string; Kind: string; Query: string; Chart: string; Target?: string; RefreshOn?: string; Table?: string }
export type BoardInfo = { name: string; refreshSecs: number; connection?: string; vars: BoardVar[]; panels: BoardPanel[]; error: string }
export type PanelOut = { title: string; kind: string; number?: string; error?: string; result?: ResultData; labels: string[]; values: number[]; series?: { name: string; values: number[] }[] }
export type ItemGroup = { name: string; items: string[] }
export type PluginInfo = { name: string; contexts: string[]; output: string; error: string }
export type DiagramTable = { ref: ObjRef; columns: ResultCol[]; fks: { columns: string[]; refSchema: string; refTable: string; refColumns: string[] }[]; cluster: number }
