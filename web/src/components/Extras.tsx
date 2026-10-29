// Milestone 4 pieces: safe-mode picker, Open Anything, console pane, Set Value, multi-column sort, page settings, tab menu, tree view.
import { Fragment, useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, qs, type DbObject, type MenuItem, type SessionInfo, type Sort, type Val } from '../utils/api'
import { Dialog, confirm } from './Dialogs'
import { localRecent } from '../utils/files'
import { copyText } from '../utils/copy'
import { Select } from './Select'
import { Layer, MENU, atPoint } from './Layer'

const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ---- safe mode ----------------------------------------------------------------------------------------

const LEVELS: [string, string, string][] = [
  ['silent', 'Silent Mode', 'send queries with no warning'], ['alert1', 'Alert Mode 1', 'confirm every query'], ['alert2', 'Alert Mode 2', 'confirm all but SELECT/EXPLAIN/SHOW'],
  ['safe1', 'Safe Mode 1', 'password for every query'], ['safe2', 'Safe Mode 2', 'password for all but SELECT/EXPLAIN/SHOW'],
]

export function SafeModeDialog({ sid, info, onClose, onDone }: { sid: string; info: SessionInfo; onClose: () => void; onDone: () => void }) {
  const [pick, setPick] = useState<string | null>(null) // set when leaving a password level asks for the password
  const [pw, setPw] = useState(''), [err, setErr] = useState('')
  const go = async (level: string, password = '', confirmed = false) => {
    try { await api('POST', `/sessions/${sid}/safe-mode`, { level, password, ...(confirmed ? { confirmed } : {}) }); onDone() } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.body.gate?.confirm) { // no stored password to ask for: confirm instead
        if (!confirmed && await confirm(e.message || 'Change safe mode?', undefined, 'Change')) return go(level, password, true)
        setErr('')
      } else if (e instanceof ApiError && e.status === 409) { setPick(level); setErr(password ? 'Wrong password' : '') } else setErr(msg(e))
    }
  }
  return (
    <Dialog title={`Safe mode for ${info.name}`} onClose={onClose}>
      {pick === null ? (
        <div className="flex flex-col gap-0.5">
          {LEVELS.map(([v, label, d]) => (
            <button key={v} className={`text-left border-transparent hover:border-ink-3 flex gap-3 ${v === info.level ? 'btn-on' : ''}`} autoFocus={v === info.level} onClick={() => go(v)}>
              <span className="w-4">{v === info.level ? '•' : ''}</span><span className="w-32">{label}</span><span className="text-ink-2">{d}</span>
            </button>
          ))}
        </div>
      ) : (
        <form onSubmit={(e) => { e.preventDefault(); go(pick, pw) }}>
          <p className="text-ink-2 mt-0">Enter the database password to change safe mode.</p>
          <input type="password" autoFocus className="w-full" aria-label="Database password" value={pw} onChange={(e) => setPw(e.target.value)} />
          <div className={ACTIONS}><button type="button" onClick={() => setPick(null)}>Back</button><button className="btn-primary" type="submit" disabled={!pw}>Change</button></div>
        </form>
      )}
      {err && <div className="text-del mt-2">{err}</div>}
    </Dialog>
  )
}

// ---- Open Anything ------------------------------------------------------------------------------------

// run gets true for alt+Enter: open the hit in a new tab
export type PaletteItem = { group: string; label: string; detail?: string; run: (newTab?: boolean) => void }

// match scores a query against a label: -1 no match, lower is better. `^` anchors the start, `$` the end, otherwise fuzzy (substring first, then subsequence).
export function match(q: string, s: string): number {
  let t = q.toLowerCase().trim()
  const start = t.startsWith('^'), end = t.endsWith('$')
  if (start) t = t.slice(1)
  if (end) t = t.slice(0, -1)
  const h = s.toLowerCase()
  if (!t) return 0
  if ((start && !h.startsWith(t)) || (end && !h.endsWith(t))) return -1
  if (start || end) return 0
  const at = h.indexOf(t)
  if (at >= 0) return at
  let j = 0, first = -1, last = 0
  for (let i = 0; i < h.length && j < t.length; i++) if (h[i] === t[j]) { if (first < 0) first = i; last = i; j++ }
  return j === t.length ? 1000 + last - first : -1
}

export function Palette({ items, onClose }: { items: PaletteItem[]; onClose: () => void }) {
  const [q, setQ] = useState(''), [at, setAt] = useState(0)
  const shown = items.map((it) => ({ it, s: match(q, it.label) })).filter((x) => x.s >= 0).sort((a, b) => a.s - b.s).slice(0, 60).map((x) => x.it)
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => { list.current?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' }) }, [at])
  const pick = (it?: PaletteItem, newTab = false) => { if (it) { onClose(); it.run(newTab) } }
  return (
    <Dialog title="Open anything" onClose={onClose}>
      <input autoFocus className="w-full text-[14px] py-1.5" aria-label="Search" placeholder="Tables, queries, databases, actions…" value={q}
        onChange={(e) => { setQ(e.target.value); setAt(0) }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') { e.preventDefault(); setAt(Math.min(shown.length - 1, at + 1)) }
          else if (e.key === 'ArrowUp') { e.preventDefault(); setAt(Math.max(0, at - 1)) }
          else if (e.key === 'Enter') { e.preventDefault(); pick(shown[at], e.altKey) }
          else if (e.key === 'Tab' && shown[at]) { e.preventDefault(); setQ(shown[at].label); setAt(0) } // complete the keyword with the selected name
        }} />
      <div ref={list} className="mt-2 max-h-[50vh] overflow-auto font-mono text-[12.5px]" role="listbox">
        {shown.map((it, i) => (
          <div key={it.group + it.label + i} role="option" aria-selected={i === at} className={`flex items-center gap-3 px-2 py-1 rounded-[2px] cursor-pointer ${i === at ? 'bg-raised shadow-[inset_2px_0_0_var(--accent)]' : 'hover:bg-panel'}`} onMouseEnter={() => setAt(i)} onClick={() => pick(it)}>
            <span className={`w-20 flex-none text-[11px] ${it.group === 'action' || it.group === 'feature' ? 'text-accent' : 'text-ink-3'}`}>{it.group}</span><span className="flex-1 min-w-0 truncate">{it.label}</span>{it.detail && <span className="text-ink-3 text-[11.5px] truncate max-w-[40%]">{it.detail}</span>}
          </div>
        ))}
        {!shown.length && <div className="p-2 text-ink-2">Nothing matches.</div>}
      </div>
      <div className="flex gap-3 mt-2 text-[11.5px] text-ink-3 font-mono"><span><kbd>↑</kbd> <kbd>↓</kbd> move</span><span><kbd>Enter</kbd> open</span><span><kbd>Alt+Enter</kbd> new tab</span><span><kbd>Tab</kbd> complete</span><span><kbd>^</kbd> starts with, <kbd>$</kbd> ends with</span></div>
    </Dialog>
  )
}

// ---- console ------------------------------------------------------------------------------------------

const KW = /\b(select|from|where|insert|into|values|update|set|delete|create|alter|drop|table|index|join|left|right|inner|outer|on|group|order|by|limit|offset|having|and|or|not|null|as|in|is|like|begin|commit|rollback|truncate)\b|('(?:[^']|'')*')|(--[^\n]*)|(\b\d+(?:\.\d+)?\b)/gi
function highlight(sql: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  for (const m of sql.matchAll(KW)) {
    out.push(sql.slice(last, m.index))
    out.push(<span key={m.index} className={m[1] ? 'text-accent font-semibold' : m[2] ? 'text-new' : m[3] ? 'text-ink-3 italic' : 'text-edit'}>{m[0]}</span>)
    last = m.index! + m[0].length
  }
  out.push(sql.slice(last))
  return out
}

// shortcut: console lines carry no failed flag yet, so "Errors only" matches text that says error or failed; a `failed` field from the server is used when present.
const isErr = (l: { text: string; failed?: boolean }) => !!l.failed || /(^|\W)(error|failed|exception)(\W|$)/i.test(l.text)

export function ConsolePane({ conn, items, notify, onClose }: { conn: string; items?: MenuItem[]; notify?: (m: string) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['console', conn], queryFn: () => api<{ time: string; text: string; failed?: boolean }[]>('GET', '/console' + qs({ conn })), refetchInterval: 2000, gcTime: 0 })
  const end = useRef<HTMLDivElement>(null)
  const [find, setFind] = useState(''), [errs, setErrs] = useState(false), [menu, setMenu] = useState<{ x: number; y: number; text: string } | null>(null)
  const lines = (q.data ?? []).filter((l) => (!errs || isErr(l)) && (!find || l.text.toLowerCase().includes(find.toLowerCase())))
  const [sel, setSel] = useState(-1) // the selected line (n / N move it, y copies it, m opens its menu); -1 follows the newest
  const at = Math.min(sel < 0 ? lines.length - 1 : sel, lines.length - 1)
  useEffect(() => { if (sel < 0) end.current?.scrollTo(0, end.current.scrollHeight) }, [q.data?.length, errs, sel])
  useEffect(() => { setSel(-1) }, [find, errs])
  useEffect(() => { if (sel >= 0) end.current?.querySelector('[data-sel]')?.scrollIntoView({ block: 'nearest' }) }, [sel])
  const clear = async () => { await api('DELETE', '/console').catch(() => {}); q.refetch() }
  const copy = (text: string) => copyText(text).then(() => notify?.('Copied the line'), (e) => notify?.(msg(e)))
  const mark = (t: string): ReactNode => { // the find text is highlighted inside a plain-text line
    if (!find) return highlight(t)
    const at = t.toLowerCase().indexOf(find.toLowerCase())
    return at < 0 ? highlight(t) : <>{highlight(t.slice(0, at))}<mark className="bg-edit/40 text-ink">{t.slice(at, at + find.length)}</mark>{highlight(t.slice(at + find.length))}</>
  }
  // keymap actions of the console context (data-ctx="console"): the TUI's / n N y e m
  const rowRef = useRef<HTMLDivElement>(null)
  const acts = useRef<(a: string) => boolean>(() => false)
  acts.current = (a) => {
    const step = (d: number) => lines.length && setSel((at + d + lines.length) % lines.length)
    switch (a) {
      case 'console_find': document.getElementById('console-find')?.focus(); return true
      case 'console_next': step(1); return true
      case 'console_prev': step(-1); return true
      case 'console_copy': if (lines[at]) copy(lines[at].text); else notify?.('No line to copy'); return true
      case 'console_errors': setErrs((v) => !v); return true
      case 'console_menu': { const r = rowRef.current?.getBoundingClientRect(), b = end.current?.getBoundingClientRect(); if (lines[at]) setMenu({ x: (b?.left ?? 0) + 24, y: r?.bottom ?? b?.top ?? 0, text: lines[at].text }); return true }
    }
    return false
  }
  useEffect(() => {
    const h = (e: Event) => { if (!e.defaultPrevented && acts.current((e as CustomEvent).detail?.action)) e.preventDefault() }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  }, [])
  // the right-click menu is meta.menus.console (labels and order); Find focuses the box, Errors only toggles, Copy line copies the clicked line
  const run = (a: string, text: string) => (a === 'find' ? document.getElementById('console-find')?.focus() : a === 'copy' ? copy(text) : a === 'errors_only' ? setErrs((v) => !v) : clear())
  const list: MenuItem[] = items?.length ? items : [{ label: 'Find…', action: 'find' }, { label: 'Copy line', action: 'copy' }, { label: 'Errors only', action: 'errors_only' }, { label: 'Clear', action: 'clear_console' }]
  return (
    <div className="h-[200px] flex-none border-t border-rule flex flex-col bg-canvas font-mono" data-ctx="console" role="region" aria-label="Console log">
      <div className="toolbar bg-canvas"><strong className="font-medium">Console</strong>
        <input id="console-find" className="flex-1 min-w-[8em] max-w-[22em] text-[12px] py-0" aria-label="Find in the console" placeholder="Find" value={find} onChange={(e) => setFind(e.target.value)} onKeyDown={(e) => { if (e.key === 'Escape' && find) { e.stopPropagation(); setFind('') } }} />
        <button className={`btn-ghost ${errs ? 'btn-on' : ''}`} aria-pressed={errs} onClick={() => setErrs(!errs)}>Errors only</button>
        <span className="flex-1" />
        <button className="btn-ghost" onClick={clear}>Clear</button>
        <button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      <div ref={end} className="overflow-auto flex-1 px-3 py-1.5 text-[12px]">
        {lines.map((l, i) => <div key={i} ref={i === at ? rowRef : undefined} {...(i === at && sel >= 0 ? { 'data-sel': '' } : {})} className={`group/l flex items-start gap-2 whitespace-pre-wrap wrap-anywhere py-px hover:bg-panel ${i === at && sel >= 0 ? 'bg-panel shadow-[inset_2px_0_0_var(--accent)]' : ''}`} onContextMenu={(e) => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, text: l.text }) }}>
          <span className="flex-1 min-w-0"><span className="text-ink-3 select-none">{new Date(l.time).toLocaleTimeString()} </span>{mark(l.text)}</span>
          <button className="btn-ghost text-[11px] px-1 opacity-0 group-hover/l:opacity-100 focus-visible:opacity-100" aria-label="Copy this line" onClick={() => copy(l.text)}>Copy</button></div>)}
        {q.data?.length === 0 && <div className="text-ink-2">Statements sent to the database show up here.</div>}
        {!!q.data?.length && !lines.length && <div className="text-ink-2">No line matches.</div>}
      </div>
      {menu && <TabMenu x={menu.x} y={menu.y} label="Console" onClose={() => setMenu(null)} items={list.map((it): [string, () => void] => [it.label, () => run(it.action, menu.text)])} />}
    </div>
  )
}

// ---- Set Value ----------------------------------------------------------------------------------------

// Set Value menu: EMPTY / NULL / DEFAULT / NOW(), a date, or a file. Cells of binary columns travel as \x<hex> text (db.FormatValue), so a file is sent that way on PostgreSQL.
// shortcut: on other engines a file that is not valid UTF-8 text is also sent as \x<hex>, which they store as text; native binary literals are the upgrade.
// Only these run without the server's advanced flag (mirrors exprMacros in internal/web/changes.go).
const MACROS = new Set(['now()', 'current_timestamp', 'current_date', 'current_time', 'localtimestamp', 'gen_random_uuid()', 'uuid_generate_v4()', 'uuid()', 'unix_timestamp()'])
async function expression(e: string, onPick: (v: Val) => void) {
  if (MACROS.has(e.split(/\s+/).filter(Boolean).join('').toLowerCase())) return onPick({ $expr: e })
  if (await confirm('This expression is sent to the database as unescaped SQL. Use it?', e, 'Use it')) onPick({ $expr: e, $advanced: 'true' })
}
export function SetValueDialog({ sid, schema, col, dialect, onPick, onClose }: { sid: string; schema: string; col: string; dialect: string; onPick: (v: Val) => void; onClose: () => void }) {
  const [date, setDate] = useState(''), [err, setErr] = useState(''), [expr, setExpr] = useState(''), [seq, setSeq] = useState('')
  // PostgreSQL: a column can take the next value of a sequence
  const objs = useQuery({ queryKey: ['setvalue-seqs', sid, schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema })), enabled: dialect === 'postgres', gcTime: 0 })
  const seqs = (objs.data ?? []).filter((o) => o.Kind === 'sequence').map((o) => {
    const q = (x: string) => '"' + x.replace(/"/g, '""') + '"' // quoted, so a mixed-case sequence resolves
    return { value: o.Schema ? `${q(o.Schema)}.${q(o.Name)}` : q(o.Name), label: o.Schema ? `${o.Schema}.${o.Name}` : o.Name }
  })
  const file = async (f?: File) => {
    if (!f) return
    const b = new Uint8Array(await f.arrayBuffer())
    let text: string | null = null
    if (dialect !== 'postgres') { try { text = new TextDecoder('utf-8', { fatal: true }).decode(b) } catch { /* binary */ } }
    if (text !== null) return onPick(text)
    if (b.length > 8 << 20) return setErr('That file is larger than 8 MB')
    onPick('\\x' + Array.from(b, (x) => x.toString(16).padStart(2, '0')).join(''))
  }
  return (
    <Dialog title={`Set value of ${col}`} onClose={onClose}>
      <div className="flex flex-wrap gap-1.5">
        <button autoFocus onClick={() => onPick({ $: 'empty' })}>EMPTY</button>
        <button onClick={() => onPick({ $: 'null' })}>NULL</button>
        <button onClick={() => onPick({ $: 'default' })}>DEFAULT</button>
        <button onClick={() => onPick({ $expr: 'NOW()' })}>NOW()</button>
        {dialect === 'postgres' && <button onClick={() => onPick({ $expr: 'gen_random_uuid()' })}>gen_random_uuid()</button>}
      </div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Date</label>
        <input type="datetime-local" step={1} aria-label="Date" value={date} onChange={(e) => setDate(e.target.value)} />
        <button disabled={!date} onClick={() => onPick(date.replace('T', ' '))}>Set date</button></div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">File</label>
        <input type="file" aria-label="Add a file" onChange={(e) => file(e.target.files?.[0])} /></div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Advanced</label>
        <input className="flex-1 min-w-0 font-mono" aria-label="SQL expression" placeholder="now() + interval '1 day'" value={expr} onChange={(e) => setExpr(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && expr.trim()) expression(expr.trim(), onPick) }} />
        <button disabled={!expr.trim()} onClick={() => expression(expr.trim(), onPick)}>Use expression</button></div>
      {seqs.length > 0 && <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Sequence</label>
        <Select aria-label="Sequence" value={seq} onChange={setSeq} options={[{ value: '', label: 'pick a sequence' }, ...seqs]} />
        <button disabled={!seq} onClick={() => expression(`nextval('${seq.replace(/'/g, "''")}'::regclass)`, onPick)}>Use nextval</button></div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button></div>
    </Dialog>
  )
}

// ---- sort by several columns --------------------------------------------------------------------------

export function SortDialog({ columns, sort, onApply, onClose }: { columns: string[]; sort: Sort[]; onApply: (s: Sort[]) => void; onClose: () => void }) {
  const [rows, setRows] = useState<Sort[]>(sort.length ? sort : [{ Column: columns[0] ?? '', Desc: false }])
  const set = (i: number, p: Partial<Sort>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...p } : r)))
  return (
    <Dialog title="Sort by several columns" onClose={onClose}>
      {rows.map((r, i) => (
        <div key={i} className="flex items-center gap-2 my-1.5">
          <span className="w-5 text-ink-3">{i + 1}</span>
          <Select aria-label={`Column ${i + 1}`} value={r.Column} onChange={(v) => set(i, { Column: v })} options={columns} />
          <Select aria-label={`Direction ${i + 1}`} value={r.Desc ? 'desc' : 'asc'} onChange={(v) => set(i, { Desc: v === 'desc' })} options={[{ value: 'asc', label: 'Ascending' }, { value: 'desc', label: 'Descending' }]} />
          <button className="btn-ghost" onClick={() => setRows(rows.filter((_, j) => j !== i))}>Remove</button>
        </div>
      ))}
      <div className={ACTIONS}>
        <button onClick={() => setRows([...rows, { Column: columns.find((c) => !rows.some((r) => r.Column === c)) ?? columns[0], Desc: false }])}>Add column</button>
        <span className="flex-1" /><button onClick={onClose}>Cancel</button>
        <button className="btn-primary" onClick={() => onApply(rows.filter((r, i) => r.Column && rows.findIndex((x) => x.Column === r.Column) === i))}>Sort</button>
      </div>
    </Dialog>
  )
}

// ---- row limit and offset -----------------------------------------------------------------------------

export function PageDialog({ limit, offset, onApply, onClose }: { limit: number; offset: number; onApply: (limit: number, offset: number) => void; onClose: () => void }) {
  const [l, setL] = useState(String(limit)), [o, setO] = useState(String(offset))
  const ok = Number(l) > 0 && Number(o) >= 0 && Number.isInteger(Number(l)) && Number.isInteger(Number(o))
  return (
    <Dialog title="Row limit and offset" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (ok) onApply(Number(l), Number(o)) }}>
        <div className="flex items-center gap-2 my-2"><label className="w-24 text-ink-2">Limit</label><input autoFocus type="number" min={1} value={l} onChange={(e) => setL(e.target.value)} /></div>
        <div className="flex items-center gap-2 my-2"><label className="w-24 text-ink-2">Offset</label><input type="number" min={0} value={o} onChange={(e) => setO(e.target.value)} /></div>
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button className="btn-primary" type="submit" disabled={!ok}>Apply</button></div>
      </form>
    </Dialog>
  )
}

// ---- tab menu -----------------------------------------------------------------------------------------

// items: [label, run, why]; a third entry greys the item and shows it as the tooltip. "-" is a separator.
export function TabMenu({ x, y, items, onClose, label = 'Menu' }: { x: number; y: number; items: [string, () => void, string?][]; onClose: () => void; label?: string }) {
  return (
    <Layer onClose={onClose}>
      <div className={`${MENU} min-w-[200px]`} style={atPoint(x, y, { height: 30 * items.length + 16 })} role="menu" aria-label={label}
        onKeyDown={(e) => { const l = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role=menuitem]:not(:disabled)')], i = l.indexOf(document.activeElement as HTMLButtonElement); if (e.key === 'ArrowDown') l[(i + 1) % l.length]?.focus(); else if (e.key === 'ArrowUp') l[(i - 1 + l.length) % l.length]?.focus(); else if (e.key === 'Home') l[0]?.focus(); else if (e.key === 'End') l[l.length - 1]?.focus(); else if (e.key === 'Escape' || e.key === 'Tab') onClose(); else return; e.preventDefault(); e.stopPropagation() }}>
        {items.map(([text, run, why], i) => text === '-' ? <hr key={i} className="border-t border-rule my-1" /> : <button key={text} role="menuitem" autoFocus={i === 0} disabled={!!why} title={why} className={`menu-item ${/^(Delete|Drop|Truncate|Close all)/.test(text) ? 'btn-danger' : ''}`} onClick={() => { onClose(); run() }}>{text}</button>)}
      </div>
    </Layer>
  )
}

// ---- tree view (MongoDB, Redis documents) -------------------------------------------------------------

export function Tree({ docs }: { docs: string[] }) {
  return <div className="flex-1 overflow-auto px-4 py-3 text-[12px] font-mono" data-ctx="tree">{docs.map((d, i) => { let v: unknown = d; try { v = JSON.parse(d) } catch { /* plain text */ } return <Node key={i} k={`#${i + 1}`} v={v} open /> })}</div>
}
function Node({ k, v, open }: { k: string; v: unknown; open?: boolean }) {
  if (v === null || typeof v !== 'object') return <div className="pl-4"><span className="text-ink-2">{k}: </span><span className={typeof v === 'string' ? 'text-new' : 'text-edit'}>{typeof v === 'string' ? JSON.stringify(v) : String(v)}</span></div>
  const entries = Object.entries(v as object)
  return (
    <details open={open} className="pl-4"><summary className="cursor-pointer"><span className="text-ink-2">{k} </span>{Array.isArray(v) ? `[${entries.length}]` : `{${entries.length}}`}</summary>
      {entries.map(([ek, ev]) => <Node key={ek} k={ek} v={ev} />)}</details>
  )
}

// ---- connection details -------------------------------------------------------------------------------

const SWATCHES = ['#7aa2f7', '#4fc4a0', '#e0af68', '#ff9a5c', '#f7768e', '#bb9af7']

export function DetailsDialog({ sid, info, onClose, onSaved }: { sid: string; info: SessionInfo; onClose: () => void; onSaved: () => void }) {
  const [color, setColor] = useState(info.color || ''), [err, setErr] = useState('')
  const tls = [info.sslMode || 'default', info.ssh ? 'through an SSH tunnel' : ''].filter(Boolean).join(', ')
  const server = [info.host && `${info.host}${info.port ? ':' + info.port : ''}`].filter(Boolean).join('')
  const rows: [string, string][] = [['Connection', info.name], ['Driver', info.driver], ['Server version', info.serverInfo], ['Host', server], [info.caps.DatabaseNoun ? info.caps.DatabaseNoun[0].toUpperCase() + info.caps.DatabaseNoun.slice(1) : 'Database', info.database], ['Schema', info.schema], ['User', info.user ?? ''], ['TLS', info.host ? tls : ''], ['Tag', info.tag], ['Safe mode', info.levelLabel], ['Encoding', info.encoding ?? ''], ['Row limit', String(info.rowLimit)]]
  const save = async () => {
    try { await api('POST', `/sessions/${sid}/color`, { color }); onSaved() } catch (e) { setErr(msg(e)) }
  }
  return (
    <Dialog title="Connection details" onClose={onClose}>
      <div className="grid grid-cols-[8em_1fr] gap-x-3 gap-y-1 font-mono text-[12.5px]">{rows.filter(([, v]) => v).map(([k, v]) => <Fragment key={k}><span className="text-ink-2">{k}</span><span className="wrap-anywhere">{v}</span></Fragment>)}</div>
      <div className="flex items-center gap-2 mt-4 text-[12.5px]"><span className="w-[8em] text-ink-2 font-mono">Colour</span>
        {SWATCHES.map((c) => <button key={c} className={`w-5 h-5 p-0 rounded-[2px] ${color === c ? 'outline outline-2 outline-ink' : ''}`} style={{ background: c }} aria-label={`Use ${c}`} onClick={() => setColor(c)} />)}
        <input type="color" aria-label="Pick a colour" className="w-8 h-6 p-0" value={/^#[0-9a-f]{6}$/i.test(color) ? color : '#7aa2f7'} onChange={(e) => setColor(e.target.value)} />
        <button className="btn-ghost" disabled={!color} onClick={() => setColor('')}>Default</button></div>
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Close</button><button className="btn-primary" disabled={color === (info.color || '')} onClick={save}>Save colour</button></div>
    </Dialog>
  )
}

// ---- recent files ---------------------------------------------------------------------------------------------------

export function RecentDialog({ onPick, onClose }: { onPick: (entry: string) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['recent-files'], queryFn: () => api<{ files: string[] }>('GET', '/state/recent-files'), gcTime: 0 })
  // the server's list holds paths (the terminal's); files picked in this browser are only names, kept in this browser
  const files = q.data ? [...new Set([...q.data.files, ...localRecent()])] : []
  return (
    <Dialog title="Open a recent file" onClose={onClose}>
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !files.length && <div className="text-ink-2">No file was opened yet.</div>}
      <div className="flex flex-col gap-0.5">{files.map((f, i) => <button key={f} autoFocus={i === 0} className="text-left border-transparent hover:border-ink-3 font-mono truncate" title={f} onClick={() => { onClose(); onPick(f) }}>{f}</button>)}</div>
      <p className="text-ink-3 text-[12px]">A browser cannot reopen a file by name: files first opened here ask for the file again.</p>
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button></div>
    </Dialog>
  )
}
