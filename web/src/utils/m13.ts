// Pure helpers of the M13 web views (diagram foreign keys, board variables, process list, column drag), tested without a browser.
import type { TableDef, ForeignKey } from './tabledef'

// withForeignKey is the "new" side of a structure plan that adds one foreign key to the table as loaded (old stays untouched).
export function withForeignKey(old: TableDef, fk: ForeignKey): TableDef {
  if (!fk.Columns.length || !fk.RefTable || !fk.RefColumns.length) throw new Error('A foreign key needs columns, a referenced table and referenced columns')
  if (fk.Columns.length !== fk.RefColumns.length) throw new Error(`${fk.Columns.length} column(s) cannot reference ${fk.RefColumns.length}`)
  const have = new Set(old.Columns.map((c) => c.Name))
  const bad = fk.Columns.find((c) => !have.has(c))
  if (bad) throw new Error(`The table has no column ${bad}`)
  return { ...old, Columns: old.Columns.map((c) => ({ ...c })), ForeignKeys: [...old.ForeignKeys, fk] }
}

// ---- boards ----
type PanelLike = { Kind: string; var?: string; default?: string; options?: string[] | null }
const VARIABLE = new Set(['input', 'dropdown'])
export const isVarPanel = (p: { Kind: string }) => VARIABLE.has(p.Kind)

// panelVals gives every input / dropdown panel's variable its starting value: the panel's default, else the board variable's, else the first fixed option.
export function panelVals(vars: { Name: string; Default: string }[], panels: PanelLike[]): Record<string, string> {
  const out: Record<string, string> = Object.fromEntries(vars.map((v) => [v.Name, v.Default]))
  for (const p of panels) if (isVarPanel(p) && p.var) out[p.var] = p.default || out[p.var] || (p.options?.[0] ?? '')
  return out
}

// ---- process list ----
export type ProcRow = { id: string; user: string; db: string; state: string; elapsedMs: number; query: string }
export function sortProcs(l: ProcRow[], by: string, desc: boolean): ProcRow[] {
  const dir = desc ? -1 : 1
  const key = (p: ProcRow) => (by === 'elapsed' ? p.elapsedMs : by === 'id' ? Number(p.id) : (p as any)[by] ?? '')
  return [...l].sort((a, b) => {
    const x = key(a), y = key(b)
    const r = typeof x === 'number' && typeof y === 'number' && !Number.isNaN(x) && !Number.isNaN(y) ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true })
    return r * dir
  })
}

// ---- lists ----
// moveItem returns l with the element at from moved so it sits at index to.
export function moveItem<T>(l: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || from >= l.length) return l
  const o = l.filter((_, i) => i !== from)
  o.splice(Math.max(0, Math.min(o.length, to)), 0, l[from])
  return o
}

// colour overrides of the diagram: stored as {"schema.table": "#rrggbb"}; an override is only used when it is a hex colour
export const validColour = (c: string | undefined) => (c && /^#[0-9a-fA-F]{6}$/.test(c) ? c : undefined)

// chat title: the first user message cut to 60 characters, like store.Chat
export function chatTitle(msgs: { role: string; content: string }[]): string {
  const m = msgs.find((x) => x.role === 'user')?.content.trim().replace(/\s+/g, ' ') ?? ''
  const a = Array.from(m)
  return a.length > 60 ? a.slice(0, 60).join('') : m
}

// ---- backup / restore ----
export type BackupOption = { Flag: string; Desc: string }
// shortcut: a static copy of backup.CommonOptions(driver, restore) for when GET /backup/options is not served; upgrade: drop it once the route is stable
export function commonBackupOptions(driver: string, restore: boolean): BackupOption[] {
  if (driver === 'postgres') return restore
    ? [{ Flag: '--no-owner', Desc: 'Do not restore object ownership' }, { Flag: '--no-privileges', Desc: 'Do not restore grants' }, { Flag: '--clean', Desc: 'Drop objects before creating them' }, { Flag: '--if-exists', Desc: 'Use IF EXISTS with --clean' }, { Flag: '--single-transaction', Desc: 'Restore in one transaction' }, { Flag: '--exit-on-error', Desc: 'Stop at the first error' }]
    : [{ Flag: '--no-owner', Desc: 'Do not dump object ownership' }, { Flag: '--no-privileges', Desc: 'Do not dump grants' }, { Flag: '--clean', Desc: 'Add DROP statements before CREATE' }, { Flag: '--if-exists', Desc: 'Use IF EXISTS with --clean' }, { Flag: '--inserts', Desc: 'Dump data as INSERT instead of COPY' }, { Flag: '--column-inserts', Desc: 'INSERT with column names' }]
  if (driver === 'mysql' || driver === 'mariadb') return restore
    ? [{ Flag: '--force', Desc: 'Continue after SQL errors' }, { Flag: '--binary-mode', Desc: 'Allow binary data in the file' }]
    : [{ Flag: '--single-transaction', Desc: 'Consistent snapshot without locking' }, { Flag: '--routines', Desc: 'Include stored routines' }, { Flag: '--triggers', Desc: 'Include triggers' }, { Flag: '--events', Desc: 'Include events' }, { Flag: '--no-tablespaces', Desc: 'Skip tablespace statements' }, { Flag: '--skip-lock-tables', Desc: 'Do not lock tables' }]
  return []
}

// extraArgs joins the ticked common options with the free-form text (split on spaces, double quotes group), in that order, without repeats.
export function extraArgs(picked: string[], text: string): string[] {
  const out = [...picked]
  for (const m of text.matchAll(/"([^"]*)"|(\S+)/g)) { const a = m[1] ?? m[2]; if (a && !out.includes(a)) out.push(a) }
  return out
}

// ---- keyboard ----
// enterSubmits runs fn when Enter is pressed in a single-line text field of a dialog (not a button, checkbox, select or textarea).
export function enterSubmits(e: { key: string; target: EventTarget | null; defaultPrevented?: boolean; preventDefault: () => void }, fn: () => void): boolean {
  const t = e.target as { tagName?: string; type?: string } | null
  if (e.key !== 'Enter' || e.defaultPrevented || t?.tagName !== 'INPUT' || !['text', 'number', 'search', 'password', 'email', ''].includes(t.type ?? '')) return false
  e.preventDefault(); fn(); return true
}

// limitHint: 0 means unlimited for the per-hour limits but "use the server's max_user_connections" for MAX_USER_CONNECTIONS (Postgres: -1 = no limit)
export const limitHint = (key: string, pg: boolean) => (pg ? '-1 = no limit' : key === 'MAX_USER_CONNECTIONS' ? '0 = the server default' : '0 = unlimited')

// previewLength is how many bytes of a CSV prefix to upload for the preview: up to and including the last newline,
// so a slice that cuts the last row does not fail it (a short row is an error). The first row is always kept.
export function previewLength(bytes: Uint8Array): number {
  const i = bytes.lastIndexOf(10)
  return i < 0 ? bytes.length : i + 1
}
