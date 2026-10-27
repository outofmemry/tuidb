import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, keyOf, qs, recentKey, type Column, type DbObject, type ItemGroup, type Meta, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { Dialog, ask, confirm } from './Dialogs'
import { TabMenu } from './Extras'
import { MultiExportDialog } from './MultiExport'
import { Select } from './Select'
import { ExportDialog, ImportDialog } from './Tools'
import { StructureEditor } from './StructureEditor'
import { buildKeyTree, flattenKeys, isFuzzy, matchName } from '../utils/matchname'
import { copyText } from '../utils/copy'
import { filterMenu, whyNot, type MenuItem } from '../utils/menus'
import { createTemplate } from '../utils/templates'

const TITLE = 'flex items-baseline gap-1.5 px-3 pt-3.5 pb-1 text-[11.5px] text-ink-2 select-none'
const MI = 'menu-item'
const colorKey = (conn: string) => `webtuidb-objcolors:${conn}`
// shortcut: object colours live in this browser's localStorage (per connection); the terminal keeps its own in state.json
const loadColors = (conn: string): Record<string, string> => { try { return JSON.parse(localStorage.getItem(colorKey(conn)) || '{}') } catch { return {} } }
const SWATCHES = ['#7aa2f7', '#4fc4a0', '#e0af68', '#ff9a5c', '#f7768e', '#bb9af7']

// shortcut: system schemas are recognised by name; the drivers do not flag them
const SYSTEM_SCHEMA = /^(information_schema|pg_.*|mysql|performance_schema|sys|system|INFORMATION_SCHEMA)$/
const KINDS: [string, string][] = [['table', 'Tables'], ['view', 'Views'], ['matview', 'Materialized views'], ['function', 'Functions'], ['procedure', 'Procedures'], ['sequence', 'Sequences'], ['type', 'Types']]

type Props = {
  open?: boolean; collapsed?: boolean
  sid: string; info: SessionInfo; meta: Meta
  onOpen: (o: ObjRef) => void; onQueryText: (t: string) => void; onPlan: (p: Plan) => void; notify: (m: string) => void; onDiagram: (o: ObjRef) => void
}

export function Sidebar({ open, collapsed, sid, info, meta, onOpen, onQueryText, onPlan, notify, onDiagram }: Props) {
  const qc = useQueryClient()
  const sb = meta.config.sidebar
  const [schema, setSchema] = useState(info.schema)
  const [filter, setFilter] = useState('')
  const [menuAt, setMenuAt] = useState<{ o: DbObject; x: number; y: number } | null>(null) // the object menu and where it opens
  const setMenu = (o: DbObject | null, at?: { x: number; y: number }) => setMenuAt(o ? { o, x: at?.x ?? 120, y: at?.y ?? 120 } : null)
  const [tool, setTool] = useState<{ kind: 'export' | 'import'; obj: DbObject; file?: File } | null>(null)
  const [create, setCreate] = useState<'' | 'menu' | 'database'>('')
  const [createAt, setCreateAt] = useState({ x: 0, y: 0 })
  const [colors, setColors] = useState<Record<string, string>>(() => loadColors(info.name))
  const [colorFor, setColorFor] = useState<DbObject | null>(null)
  const [over, setOver] = useState('') // the row a file is being dragged over
  const click = useRef<ReturnType<typeof setTimeout>>(undefined) // a single click opens after a beat, so a double click can open a new tab instead
  const [addTo, setAddTo] = useState<DbObject[] | null>(null) // items about to join a custom group
  const [many, setMany] = useState<DbObject[] | null>(null) // the several-tables export
  const [marked, setMarked] = useState<Map<string, DbObject>>(new Map()) // multi-select: delete, export and truncate act on every marked item
  const onAction = useRef<(a: string) => boolean>(() => false)
  const filterBox = useRef<HTMLInputElement>(null)
  const last = useRef<DbObject | null>(null) // the item last clicked: truncate / delete_item act on it
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [folders, setFolders] = useState<Set<string>>(new Set()) // open Redis key folders
  const [newTable, setNewTable] = useState<string | null>(null) // the New table designer, by name
  useEffect(() => {
    const h = (e: Event) => { if (onAction.current((e as CustomEvent).detail.action)) e.preventDefault() }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  }, [])

  const schemas = useQuery({ queryKey: ['schemas', sid, info.database], queryFn: () => api<{ schemas: string[] | null }>('GET', `/sessions/${sid}/schemas`), enabled: info.caps.Schemas, gcTime: 0 })
  const objects = useQuery({ queryKey: ['objects', sid, info.database, schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema })), gcTime: 0 })
  const short = useQuery({ queryKey: ['shortcuts', sid], queryFn: () => api<{ pinned: string[]; recent: string[] }>('GET', '/state/shortcuts' + qs({ conn: info.name })), gcTime: 0 })
  const groupsQ = useQuery({ queryKey: ['groups', sid], queryFn: () => api<{ groups: ItemGroup[] }>('GET', '/state/groups' + qs({ conn: info.name })), gcTime: 0 })
  const groups = groupsQ.data?.groups ?? []
  const saveGroups = (next: ItemGroup[]) => api('PUT', '/state/groups' + qs({ conn: info.name }), { groups: next.filter((g) => g.items.length) }).then(() => groupsQ.refetch(), (e) => notify(e instanceof Error ? e.message : String(e)))
  const pin = useMutation({ mutationFn: (o: ObjRef) => api('POST', '/state/pin', { Conn: info.name, Item: keyOf(o) }), onSuccess: () => qc.invalidateQueries({ queryKey: ['shortcuts', sid] }) })

  const all = objects.data ?? []
  const fz = isFuzzy(sb.Matching)
  // [sidebar] matching: fuzzy or contains, with ^ and $ anchors, as matchName does in the TUI; a search lists best matches first
  const found = <T extends { Name: string }>(l: T[]): T[] => (filter ? l.map((o) => ({ o, m: matchName(filter, o.Name, fz) })).filter((x) => x.m[1]).sort((a, b) => b.m[0] - a.m[0]).map((x) => x.o) : l)
  const hide = (k: string) => (k === 'function' || k === 'procedure') ? sb.ShowFunctions === false || !info.caps.Functions : k === 'sequence' ? sb.ShowSequences === false || !info.caps.Sequences : k === 'type' ? sb.ShowTypes === false : false
  const byKey = (k: string) => all.find((o) => keyOf(o) === k || recentKey(o) === k)
  // pins and recents can name another schema's objects, which are not in `all`; keep them (assumed tables) so they can still be opened or unpinned
  const lookup = (k: string) => byKey(k) ?? (objects.data && k.includes('.') ? { Schema: k.slice(0, k.indexOf('.')), Name: k.slice(k.indexOf('.') + 1), Kind: 'table', Detail: '' } : undefined)
  const pinned = (short.data?.pinned ?? []).map(lookup).filter(Boolean) as DbObject[]
  const recent = (short.data?.recent ?? []).map(lookup).filter(Boolean).slice(0, 8) as DbObject[]
  const isPinned = (o: ObjRef) => (short.data?.pinned ?? []).includes(keyOf(o))

  const admin = async (op: string, ref: ObjRef, opts?: Record<string, string>, refs?: ObjRef[]) => {
    setMenu(null); setCreate('')
    try { onPlan(await api<Plan>('POST', `/sessions/${sid}/plan/admin`, { Op: op, Ref: ref, Refs: refs, Opts: opts })); if (refs) setMarked(new Map()) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  // Delete / Truncate of one object asks what the terminal's form asks (M14 check 6): its name typed, Cascade, and Restart identity on PostgreSQL
  const dropOrTruncate = async (op: 'drop' | 'truncate', o: DbObject) => {
    const verb = op === 'drop' ? 'Delete' : 'Truncate'
    const typed = await ask(`${verb} ${o.Name}? This cannot be undone. Type its name to confirm`)
    if (typed === null) return
    if (typed !== o.Name) return notify(`Type "${o.Name}" to confirm; nothing changed.`)
    const opts: Record<string, string> = {}
    if (await confirm(`Cascade: also ${op === 'drop' ? 'drop' : 'truncate'} everything that depends on ${o.Name}? Cancel: no.`)) opts.cascade = 'true'
    if (op === 'truncate' && info.caps.Dialect === 'postgres' && await confirm('Restart identity: reset the sequences of the table? Cancel: no.')) opts.restart_identity = 'true'
    admin(op, o, opts)
  }
  const marks = [...marked.values()]
  // delete / truncate act on every marked item (a truncate only on the tables among them), else on the item last clicked
  const bulk = (op: 'drop' | 'truncate') => {
    const list = op === 'truncate' ? marks.filter((o) => o.Kind === 'table') : marks
    if (!list.length) return notify('Mark some tables first (space)')
    admin(op, list[0], undefined, list)
  }
  const toggleMark = (o: DbObject) => setMarked((m) => { const n = new Map(m); const k = keyOf(o); n.has(k) ? n.delete(k) : n.set(k, o); return n })
  const newSchema = async () => { setCreate(''); const n = await ask('New schema name'); if (n) admin('create_schema', { Schema: n, Name: '', Kind: '' }) }
  const dropSchema = async () => {
    setCreate('')
    const t = await ask(`Type the name of the schema to drop (${schema} is the one shown)`, schema); if (!t) return
    const again = await ask(`Type ${t} again to drop schema ${t} and everything in it`); if (again === null) return
    if (again !== t) return notify('The names did not match; nothing dropped.')
    const cascade = await confirm(`Also drop everything inside ${t} (CASCADE)? Cancel drops it only if it is empty.`)
    admin('drop_schema', { Schema: t, Name: '', Kind: '' }, cascade ? { cascade: 'true' } : {})
  }
  const schemaOps = info.caps.Schemas && info.caps.Dialect === 'postgres'
  onAction.current = (a) => {
    switch (a) {
      case 'new_database': setCreate('database'); return true // the database picker's menu asks for it
      case 'sidebar_search': filterBox.current?.focus(); filterBox.current?.select(); return true
      case 'new_schema': if (schemaOps) newSchema(); else notify(info.caps.Schemas ? 'A schema is a database here: use New database' : `${info.driver} has no schemas`); return true
      case 'mark': if (last.current) toggleMark(last.current); else notify('Click an item in the list first'); return true
      case 'truncate': case 'delete_item':
        if (marked.size) bulk(a === 'truncate' ? 'truncate' : 'drop')
        else if (!last.current) notify('Click an item in the list first')
        else if (a === 'truncate' && last.current.Kind !== 'table') notify('Only tables can be truncated')
        else dropOrTruncate(a === 'truncate' ? 'truncate' : 'drop', last.current)
        return true
      case 'export': if (marked.size) { setMany(marks.filter((o) => o.Kind === 'table')); return true } return false // otherwise the open tab exports
    }
    return false
  }
  const script = async (o: ObjRef, kind: string) => { // "Copy script: ..." puts the script on the clipboard
    setMenu(null)
    try { await copyText((await api<{ text: string }>('GET', `/sessions/${sid}/object/script` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind, script: kind }))).text); notify(`Copied the ${kind === 'select100' ? 'SELECT' : kind.toUpperCase()} script`) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const ddl = async (o: ObjRef) => {
    setMenu(null)
    try { onQueryText((await api<{ text: string }>('GET', `/sessions/${sid}/object/ddl` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind }))).text) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }

  const copyName = (o: ObjRef) => copyText(o.Schema ? `${o.Schema}.${o.Name}` : o.Name).then(() => notify('Copied the name'), (e) => notify(String(e)))
  const askNewTable = async () => {
    if (info.caps.Editor !== 'sql') return notify(`${info.driver} has no table designer: create it with a query`)
    const n = await ask('New table name'); if (n?.trim()) setNewTable(n.trim())
  }
  const fireOpen = (o: DbObject, d: Record<string, unknown>) => window.dispatchEvent(new CustomEvent('webtuidb:open', { cancelable: true, detail: { obj: o, ...d } }))
  // menu entries are dispatched by the action name menus.SidebarObject gives them (the terminal dispatches on the same names)
  const doObject = async (a: string, o: DbObject) => {
    switch (a) {
      case 'open': return onOpen(o)
      case 'open_new_tab': return fireOpen(o, { newTab: true })
      case 'new_table': return askNewTable()
      case 'show_structure': return fireOpen(o, { view: 'structure' })
      case 'edit_definition': case 'show_ddl': return ddl(o)
      case 'triggers': fireOpen(o, {}); return void setTimeout(() => window.dispatchEvent(new CustomEvent('webtuidb:action', { detail: { action: 'triggers' }, cancelable: true })), 400)
      case 'copy_name': return copyName(o)
      case 'script_create': return script(o, 'create')
      case 'script_drop': return script(o, 'drop')
      case 'script_truncate': return script(o, 'truncate')
      case 'script_select100': return script(o, 'select100')
      case 'export': return setTool({ kind: 'export', obj: o })
      case 'export_many': return setMany([o])
      case 'import': return setTool({ kind: 'import', obj: o })
      case 'clone': { const n = await ask('Clone as'); if (!n) return; const withData = info.caps.Dialect === 'cql' ? false : await confirm('Duplicate the content too? Cancel copies only the structure.'); admin('clone', o, { new_name: n, with_data: String(withData) }); return }
      case 'rename': { const n = await ask('New name', o.Name); if (n) admin('rename', o, { new_name: n }); return }
      case 'truncate': return dropOrTruncate('truncate', o)
      case 'delete_item': return dropOrTruncate('drop', o)
      case 'pin': return pin.mutate(o)
      case 'mark': return toggleMark(o)
      case 'group_add': return setAddTo([o])
      case 'diagram': return onDiagram(o)
      case 'set_color': return setColorFor(o)
      case 'new_schema': return schemaOps ? newSchema() : notify(info.caps.Schemas ? 'A schema is a database here: use New database' : `${info.driver} has no schemas`)
      case 'drop_schema': return schemaOps ? dropSchema() : notify('Only PostgreSQL schemas can be dropped here')
    }
    if (a.startsWith('group_remove:')) { const g = a.slice(13); saveGroups(groups.map((x) => (x.name === g ? { ...x, items: x.items.filter((i) => i !== keyOf(o)) } : x))) }
  }
  const objectItems = (o: DbObject): [string, () => void, string?][] => {
    const inGroups = groups.filter((g) => g.items.includes(keyOf(o)))
    const list = filterMenu(meta.menus?.sidebar_object ?? [{ label: 'Open', action: 'open' }], { kind: o.Kind, caps: info.caps as unknown as Record<string, unknown>, marked: marked.has(keyOf(o)), inGroup: inGroups.length > 0 })
    // one "Remove from group" entry per group the item is in
    return list.flatMap((it: MenuItem): [string, () => void, string?][] => it.action === 'group_remove'
      ? inGroups.map((g) => [inGroups.length > 1 ? `${it.label} ${g.name}` : it.label, () => doObject('group_remove:' + g.name, o)])
      : [[it.label, () => doObject(it.action, o), whyNot(it)]])
  }
  const newItems = (): [string, () => void, string?][] => filterMenu(meta.menus?.sidebar_new ?? [{ label: 'New table…', action: 'new_table' }], { caps: info.caps as unknown as Record<string, unknown> }).map((it) => [it.label, () => doNew(it.action), whyNot(it)])
  const doNew = async (a: string) => {
    switch (a) {
      case 'new_table': return askNewTable()
      case 'new_view': return onQueryText(createTemplate('view', info.caps.Dialect, schema))
      case 'new_matview': return onQueryText(createTemplate('matview', info.caps.Dialect, schema))
      case 'new_function': return onQueryText(createTemplate('function', info.caps.Dialect, schema))
      case 'new_schema': return schemaOps ? newSchema() : notify(info.caps.Schemas ? 'A schema is a database here: use New database' : `${info.driver} has no schemas`)
      case 'new_database': return setCreate('database')
      case 'new_group': { // shortcut: a group exists only once it holds items, so the marked items become its first members
        if (!marked.size) return notify('Mark some items first (space), then create the group from them')
        const n = await ask('New item group name'); if (!n?.trim()) return
        saveGroups([...groups, { name: n.trim(), items: marks.map(keyOf) }])
      }
    }
  }
  const setColor = (o: ObjRef, c: string) => {
    const n = { ...colors }; c ? (n[keyOf(o)] = c) : delete n[keyOf(o)]
    setColors(n); try { localStorage.setItem(colorKey(info.name), JSON.stringify(n)) } catch { /* private mode */ }
  }
  const dropFile = (o: DbObject, e: React.DragEvent) => { // a CSV / JSON / SQL file dropped on a table opens the import wizard on it
    setOver(''); if (o.Kind !== 'table') return
    const f = e.dataTransfer.files?.[0]
    if (!f) return
    e.preventDefault()
    if (!/\.(csv|tsv|json|sql)$/i.test(f.name)) return notify('Drop a CSV, JSON or SQL file')
    setTool({ kind: 'import', obj: o, file: f })
  }
  const rowKey = (o: DbObject, e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key === 'Enter') { e.preventDefault(); last.current = o; onOpen(o) }
    else if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) { e.preventDefault(); last.current = o; const r = e.currentTarget.getBoundingClientRect(); setMenu(o, { x: r.left + 24, y: r.bottom }) }
    else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      const rows = [...(e.currentTarget.closest('[data-ctx=sidebar]')?.querySelectorAll<HTMLElement>('[data-sb-item]') ?? [])], i = rows.indexOf(e.currentTarget)
      rows[i + (e.key === 'ArrowDown' ? 1 : -1)]?.focus(); e.preventDefault()
    } else return
    e.stopPropagation()
  }

  const toggle = (k: string) => setExpanded((x) => { const n = new Set(x); n.has(k) ? n.delete(k) : n.add(k); return n })
  const withCols = (o: ObjRef) => sb.ShowColumns && ['table', 'view', 'matview'].includes(o.Kind)
  const item = (o: DbObject) => (<div key={o.Schema + '.' + o.Name + o.Kind}>
    <div className={`group flex items-center gap-2 h-[26px] pl-3 pr-2.5 cursor-pointer text-[12.5px] hover:bg-panel focus-visible:bg-panel ${marked.has(keyOf(o)) ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : ''} ${over === keyOf(o) ? 'outline outline-1 -outline-offset-1 outline-accent' : ''}`} data-sb-item tabIndex={0} role="treeitem" aria-selected={marked.has(keyOf(o)) || undefined}
      onClick={() => { last.current = o; clearTimeout(click.current); click.current = setTimeout(() => onOpen(o), 200) }}
      onDoubleClick={() => { clearTimeout(click.current); last.current = o; fireOpen(o, { newTab: true }) }}
      onKeyDown={(e) => rowKey(o, e)} onContextMenu={(e) => { e.preventDefault(); last.current = o; setMenu(o, { x: e.clientX, y: e.clientY }) }}
      onDragOver={(e) => { if (o.Kind === 'table' && e.dataTransfer.types.includes('Files')) { e.preventDefault(); setOver(keyOf(o)) } }} onDragLeave={() => setOver('')} onDrop={(e) => dropFile(o, e)}>
      {withCols(o) && <button className="btn-ghost -ml-2 px-1" aria-label={expanded.has(keyOf(o)) ? 'Hide columns' : 'Show columns'} aria-expanded={expanded.has(keyOf(o))} onClick={(e) => { e.stopPropagation(); toggle(keyOf(o)) }}>{expanded.has(keyOf(o)) ? '▾' : '▸'}</button>}
      {colors[keyOf(o)] && <span className="w-2 h-2 rounded-full flex-none" style={{ background: colors[keyOf(o)] }} aria-hidden />}<span className="flex-1 truncate" title={`${o.Name} (${o.Kind})`}>{marked.has(keyOf(o)) ? <span className="text-accent">● </span> : ''}{o.Name}</span>
      {o.Detail && <span className="font-mono text-[11px] leading-[normal] text-ink-3 max-w-[7em] truncate text-right group-hover:hidden group-focus-within:hidden" title={o.Detail}>{o.Detail}</span>}
      <span className="hidden group-hover:flex group-focus-within:flex">
        <button className={`btn-ghost btn-icon ${isPinned(o) ? 'text-accent' : ''}`} title={isPinned(o) ? 'Unpin' : 'Pin'} aria-label={isPinned(o) ? `Unpin ${o.Name}` : `Pin ${o.Name}`} onClick={(e) => { e.stopPropagation(); pin.mutate(o) }}>{isPinned(o) ? '★' : '☆'}</button>
        <button className="btn-ghost btn-icon" title="Actions" aria-label={`Actions for ${o.Name}`} onClick={(e) => { e.stopPropagation(); const r = e.currentTarget.getBoundingClientRect(); setMenu(o, { x: r.left, y: r.bottom }) }}>⋯</button>
      </span>
    </div>
    {withCols(o) && expanded.has(keyOf(o)) && <ColumnList sid={sid} obj={o} sort={!!sb.SortColumns} />}
  </div>)

  // nothing to list: the filter matches no object, or the schema (or key space) is empty
  const kindCount = KINDS.filter(([k]) => !hide(k)).reduce((n, [k]) => n + found(all.filter((o) => o.Kind === k)).length, 0)
  const empty = objects.isSuccess && kindCount === 0 && !groups.some((g) => found(g.items.map(lookup).filter((o): o is DbObject => !!o)).length) && !(info.caps.Editor === 'redis' && !filter && all.length > 0)

  return (
    <div className={`w-[260px] flex-none border-r border-rule flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-y-0 max-[760px]:left-0 max-[760px]:w-[min(86vw,320px)] max-[760px]:motion-safe:transition-transform max-[760px]:duration-150 max-[760px]:ease-out ${open ? 'max-[760px]:translate-none max-[760px]:shadow-pop' : 'max-[760px]:-translate-x-[102%]'} ${collapsed ? 'hidden max-[760px]:flex' : 'flex'}`} data-ctx="sidebar">
      <div className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule">
        <div className="flex items-center gap-1">
          <div className="relative flex-1 min-w-0">
            <input ref={filterBox} className="w-full bg-panel border-transparent hover:border-rule-2 pr-6" placeholder="Filter objects" aria-label="Filter objects" value={filter} onChange={(e) => setFilter(e.target.value)} onKeyDown={(e) => { if (e.key === 'Escape' && filter) { e.stopPropagation(); setFilter('') } }} />
            {filter && <button className="btn-ghost btn-icon absolute right-0.5 top-1/2 -translate-y-1/2 h-5 w-5 text-[11px] text-ink-3" aria-label="Clear the filter" title="Clear" onClick={() => { setFilter(''); filterBox.current?.focus() }}>✕</button>}
          </div>
          <button className="btn-ghost btn-icon" title="Create a database, schema or table" aria-label="Create" aria-haspopup="menu" onClick={(e) => { const r = e.currentTarget.getBoundingClientRect(); setCreateAt({ x: r.left, y: r.bottom + 2 }); setCreate('menu') }}>+</button>
          <button className={`btn-ghost btn-icon ${objects.isFetching ? 'motion-safe:animate-[spin_1s_linear_infinite]' : ''}`} title="Refresh the list" aria-label="Refresh the list" onClick={() => { objects.refetch(); short.refetch() }}>↻</button>
        </div>
        {info.caps.Schemas && (
          <Select className="w-full bg-panel border-transparent" aria-label="Schema" value={schema} onChange={setSchema} options={(schemas.data?.schemas ?? [schema]).filter((n) => sb.LoadSystemSchemas || n === schema || !SYSTEM_SCHEMA.test(n))} />
        )}
      </div>
      <div className="overflow-auto flex-1 pt-1 pb-3" role="tree" aria-label="Objects">
        {objects.isFetching && <div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-accent/20 after:absolute after:inset-0 after:w-2/5 after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Loading objects" />}
        {objects.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading objects…</div>}
        {objects.error && <div className="px-3 py-2.5 text-[12.5px] text-del wrap-anywhere">{objects.error.message} <button className="btn-ghost" onClick={() => objects.refetch()}>Retry</button></div>}
        {empty && <div className="px-3 py-6 text-[12.5px] text-ink-2 font-sans leading-normal">{filter ? <>Nothing matches <b className="font-medium text-ink">{filter}</b>.</> : 'Nothing here yet.'}</div>}
        {marked.size > 0 && <div className="flex flex-wrap items-center gap-1 px-3 py-1.5 text-[12px] text-ink-2 border-b border-rule bg-panel shadow-[inset_3px_0_0_var(--accent)]" role="toolbar" aria-label="Marked items"><span className="flex-1 text-ink"><b className="font-semibold">{marked.size}</b> marked</span>
          <button className="btn-ghost" onClick={() => setMany(marks.filter((o) => o.Kind === 'table'))}>Export…</button><button className="btn-ghost" onClick={() => setAddTo(marks)}>Group…</button>
          <button className="btn-ghost btn-danger" onClick={() => bulk('truncate')}>Truncate…</button><button className="btn-ghost btn-danger" onClick={() => bulk('drop')}>Drop…</button><button className="btn-ghost" onClick={() => setMarked(new Map())}>Clear</button></div>}
        {!filter && pinned.length > 0 && <><div className={TITLE}>Pinned</div>{pinned.map(item)}</>}
        {!filter && sb.ShowRecent !== false && recent.length > 0 && <><div className={TITLE}>Recent</div>{recent.map(item)}</>}
        {groups.map((g) => {
          const its = found(g.items.map(lookup).filter((o): o is DbObject => !!o))
          return its.length ? <div key={g.name}><div className={TITLE}>{g.name}<span className="text-ink-3 text-[11px]">{its.length}</span></div>{its.map(item)}</div> : null
        })}
        {info.caps.Editor === 'redis' && !filter && (() => {
          const rows = flattenKeys(buildKeyTree(all), folders)
          return <><div className={TITLE}>Keys<span className="text-ink-3 text-[11px]">{all.length}</span></div>{rows.map((r) => r.folder
            ? <div key={'f' + r.path} className="flex items-center gap-1.5 h-[26px] pr-2.5 cursor-pointer text-[12.5px] hover:bg-panel" style={{ paddingLeft: 12 + r.depth * 14 }} role="treeitem" aria-expanded={r.open}
                onClick={() => setFolders((x) => { const n = new Set(x); n.has(r.path) ? n.delete(r.path) : n.add(r.path); return n })}>
                <span aria-hidden>{r.open ? '▾' : '▸'}</span><span className="flex-1 truncate">{r.seg}</span><span className="text-ink-3 text-[11px]">{r.count}</span></div>
            : <div key={'k' + r.path} className="flex items-center gap-2 h-[26px] pr-2.5 cursor-pointer text-[12.5px] hover:bg-panel" style={{ paddingLeft: 12 + r.depth * 14 }} role="treeitem" title={r.obj!.Name}
                onClick={() => { last.current = r.obj!; onOpen(r.obj!) }} onContextMenu={(e) => { e.preventDefault(); last.current = r.obj!; setMenu(r.obj!, { x: e.clientX, y: e.clientY }) }}>
                <span className="flex-1 truncate">{r.seg}</span>{r.obj!.Detail && <span className="font-mono text-[11px] text-ink-3 max-w-[7em] truncate">{r.obj!.Detail}</span>}</div>)}</>
        })()}
        {(info.caps.Editor !== 'redis' || filter) && KINDS.filter(([k]) => !hide(k)).map(([k, label]) => {
          const list = found(all.filter((o) => o.Kind === k))
          return list.length ? <div key={k}><div className={TITLE}>{label}<span className="text-ink-3 text-[11px]">{list.length}</span></div>{list.map(item)}</div> : null
        })}
      </div>
      {tool?.kind === 'export' && <ExportDialog sid={sid} notify={notify} onClose={() => setTool(null)} source={{ mode: 'table', ref: tool.obj, filters: { Filters: [], Or: false }, sort: [], columns: [], limit: 0, offset: 0 }} />}
      {tool?.kind === 'import' && <ImportDialog sid={sid} table={tool.obj} file={tool.file} notify={notify} onClose={() => setTool(null)} onDone={() => qc.invalidateQueries({ queryKey: ['objects', sid] })} />}
      {create === 'menu' && <TabMenu x={createAt.x} y={createAt.y} label="Create" items={newItems()} onClose={() => setCreate('')} />}
      {create === 'database' && <NewDatabaseDialog noun={info.caps.DatabaseNoun || 'database'} dialect={info.caps.Dialect} onClose={() => setCreate('')} onCreate={(n, opts) => admin('create_database', { Schema: '', Name: n, Kind: '' }, opts)} />}
      {addTo && <GroupDialog items={addTo} groups={groups} onClose={() => setAddTo(null)} onPick={(name) => {
        const keys = addTo.map(keyOf), has = groups.some((g) => g.name === name)
        saveGroups(has ? groups.map((g) => (g.name === name ? { ...g, items: [...new Set([...g.items, ...keys])] } : g)) : [...groups, { name, items: keys }]); setAddTo(null)
      }} />}
      {newTable !== null && <Dialog title={`New table ${newTable}`} onClose={() => setNewTable(null)}>
        <div className="flex flex-col min-h-0 max-h-[70vh]"><StructureEditor creating sid={sid} caps={info.caps} obj={{ Schema: schema, Name: newTable, Kind: 'table' }} notify={notify} onPlan={(p) => { setNewTable(null); onPlan(p) }} /></div>
      </Dialog>}
      {many && <MultiExportDialog sid={sid} info={info} initial={many} notify={notify} onClose={() => setMany(null)} />}
      {menuAt && <TabMenu x={menuAt.x} y={menuAt.y} label={menuAt.o.Name} items={objectItems(menuAt.o)} onClose={() => setMenu(null)} />}
      {colorFor && <Dialog title={`Colour of ${colorFor.Name}`} onClose={() => setColorFor(null)}>
        <div className="flex items-center gap-2">
          {SWATCHES.map((c) => <button key={c} className={`w-6 h-6 p-0 rounded-[2px] ${colors[keyOf(colorFor)] === c ? 'outline outline-2 outline-ink' : ''}`} style={{ background: c }} aria-label={`Use ${c}`} onClick={() => { setColor(colorFor, c); setColorFor(null) }} />)}
          <input type="color" aria-label="Pick a colour" className="w-8 h-6 p-0" value={/^#[0-9a-f]{6}$/i.test(colors[keyOf(colorFor)] ?? '') ? colors[keyOf(colorFor)] : '#7aa2f7'} onChange={(e) => setColor(colorFor, e.target.value)} />
          <button className="btn-ghost" onClick={() => { setColor(colorFor, ''); setColorFor(null) }}>None</button>
        </div>
        <div className="flex justify-end mt-[18px]"><button className="btn-primary" onClick={() => setColorFor(null)}>Done</button></div>
      </Dialog>}
    </div>
  )
}

// Columns under a table (sidebar show_columns), loaded when the table is expanded.
function ColumnList({ sid, obj, sort }: { sid: string; obj: ObjRef; sort: boolean }) {
  const q = useQuery({ queryKey: ['sidecols', sid, obj], queryFn: () => api<Column[]>('GET', `/sessions/${sid}/object/columns` + qs({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })), gcTime: 0 })
  const cols = sort ? [...(q.data ?? [])].sort((a, b) => a.Name.localeCompare(b.Name)) : q.data ?? []
  return <div className="pl-8 pr-2.5 text-[11.5px] text-ink-2">
    {q.isLoading && <div>Loading…</div>}{q.error && <div className="text-del">{q.error.message}</div>}
    {cols.map((c) => <div key={c.Name} className="flex gap-2 h-[20px] items-center"><span className="flex-1 truncate">{c.PK ? '◆ ' : ''}{c.Name}</span><span className="text-ink-3 truncate max-w-[7em]">{c.Type}</span></div>)}
  </div>
}

// New database: a name, and the encoding and collation where the engine takes them (PostgreSQL, MySQL).
function NewDatabaseDialog({ noun, options, onCreate, onClose }: { noun: string; options: boolean; onCreate: (name: string, opts?: Record<string, string>) => void; onClose: () => void }) {
  const [name, setName] = useState(''), [enc, setEnc] = useState(''), [col, setCol] = useState('')
  return (
    <Dialog title={`New ${noun}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (name.trim()) onCreate(name.trim(), options ? { encoding: enc.trim(), collation: col.trim() } : undefined) }}>
        <div className="grid grid-cols-[110px_1fr] gap-x-3 gap-y-2 items-center text-[12.5px] [&_label]:text-ink-2">
          <label>Name</label><input autoFocus aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} />
          {options && <><label>Encoding</label><input aria-label="Encoding" placeholder="server default, e.g. UTF8" value={enc} onChange={(e) => setEnc(e.target.value)} />
            <label>Collation</label><input aria-label="Collation" placeholder="server default" value={col} onChange={(e) => setCol(e.target.value)} /></>}
        </div>
        <div className="flex justify-end gap-2 mt-[18px]"><button type="button" onClick={onClose}>Cancel</button><button className="btn-primary" type="submit" disabled={!name.trim()}>Preview</button></div>
      </form>
    </Dialog>
  )
}

// Add to group: an existing group or a new one.
function GroupDialog({ items, groups, onPick, onClose }: { items: DbObject[]; groups: ItemGroup[]; onPick: (name: string) => void; onClose: () => void }) {
  const [name, setName] = useState('')
  return (
    <Dialog title={items.length === 1 ? `Add ${items[0].Name} to a group` : `Add ${items.length} items to a group`} onClose={onClose}>
      <div className="flex flex-col gap-0.5">{groups.map((g) => <button key={g.name} className={MI} onClick={() => onPick(g.name)}>{g.name} <span className="text-ink-3">{g.items.length}</span></button>)}</div>
      <form className="flex gap-2 mt-3" onSubmit={(e) => { e.preventDefault(); if (name.trim()) onPick(name.trim()) }}>
        <input autoFocus className="flex-1 min-w-0" aria-label="New group" placeholder="new group name" value={name} onChange={(e) => setName(e.target.value)} />
        <button className="btn-primary" type="submit" disabled={!name.trim()}>Create</button>
      </form>
      <div className="flex justify-end mt-[18px]"><button onClick={onClose}>Cancel</button></div>
    </Dialog>
  )
}
