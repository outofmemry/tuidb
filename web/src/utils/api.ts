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

export async function api<T = any>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const res = await fetch('/api' + path, {
    method, signal,
    headers: { Authorization: 'Bearer ' + token(), 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const text = await res.text() // an aborted or truncated body throws here instead of becoming {}
  let data: any = {}
  if (text) { try { data = JSON.parse(text) } catch (e) { if (res.ok) throw e } }
  if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText, data)
  return data as T
}

export const qs = (o: Record<string, string | number | undefined>) =>
  '?' + Object.entries(o).filter(([, v]) => v !== undefined && v !== '').map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`).join('&')

export type SSH = { Host: string; Port: number; User: string; Key: string; Password: string }
export type Connection = {
  Name: string; Driver: string; Host: string; Port?: number; User?: string; Password?: string; PasswordCmd?: string
  Database?: string; Tag?: string; Color?: string; SafeMode?: string; Group?: string; SSLMode?: string
  Options?: Record<string, string>; SSH?: SSH | null
}
export type Caps = {
  Schemas: boolean; Indexes: boolean; Triggers: boolean; ForeignKeys: boolean; Views: boolean; MatViews: boolean
  Functions: boolean; Sequences: boolean; Transactions: boolean; Document: boolean; Editor: string; Dialect: string
  DatabaseNoun: string; Types: string[] | null
}
export type SessionInfo = {
  id: string; name: string; driver: string; caps: Caps; serverInfo: string; database: string; schema: string
  level: string; levelLabel: string; rowLimit: number; color: string; tag: string
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
export type ExecItem = ResultData & { stmt: string; error?: string }
export type Meta = {
  drivers: string[]; sslModes: string[]; safeModes: string[]; operators: Record<string, string[]>
  config: { general: Record<string, any>; editor: Record<string, any>; table: Record<string, any>; sidebar: Record<string, any> }
  keymap: import('./keys').Keymap
}
export type Gate = { confirm: boolean; password: boolean }
export type Plan = { planId: string; statements: { display: string; readOnly: boolean }[]; gate: Gate; transactions: boolean; levelLabel: string }
export type Special = { $: 'null' | 'default' | 'empty' } | { $expr: string }
export type Val = string | null | Special

export const keyOf = (r: ObjRef) => (r.Schema ? r.Schema + '.' + r.Name : r.Name) // sidebar pins, like the TUI
export const recentKey = (r: ObjRef) => r.Schema + '.' + r.Name // recents and table state, like the TUI
export const sameRef = (a: ObjRef, b: ObjRef) => a.Schema === b.Schema && a.Name === b.Name && a.Kind === b.Kind

// clip shortens long values for one-line places (toasts, the changeset, tooltips); the popover shows the whole thing.
export const clip = (s: string, n = 120) => (s.length > n ? s.slice(0, n - 1).replace(/\s+\S*$/, '') + '…' : s)
