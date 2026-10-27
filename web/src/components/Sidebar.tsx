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
  const f = filter.toLowerCase()
  const hide = (k: string) => (k === 'function' || k === 'procedure') ? sb.ShowFunctions === false : k === 'sequence' ? sb.ShowSequences === false : k === 'type' ? sb.ShowTypes === false : false
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
      case 'sidebar_search': filterBox.current?.focus(); filterBox.current?.select(); return true
      case 'new_schema': if (schemaOps) newSchema(); else notify(info.caps.Schemas ? 'A schema is a database here: use New database' : `${info.driver} has no schemas`); return true
      case 'mark': if (last.current) toggleMark(last.current); else notify('Click an item in the list first'); return true
      case 'truncate': case 'delete_item':
        if (marked.size) bulk(a === 'truncate' ? 'truncate' : 'drop')
        else if (!last.current) notify('Click an item in the list first')
        else if (a === 'truncate' && last.current.Kind !== 'table') notify('Only tables can be truncated')
        else admin(a === 'truncate' ? 'truncate' : 'drop', last.current)
        return true
      case 'export': if (marked.size) { setMany(marks.filter((o) => o.Kind === 'table')); return true } return false // otherwise the open tab exports
    }
    return false
  }
  const script = async (o: ObjRef, kind: string) => {
    setMenu(null)
    try { onQueryText((await api<{ text: string }>('GET', `/sessions/${sid}/object/script` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind, script: kind }))).text) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const ddl = async (o: ObjRef) => {
    setMenu(null)
    try { onQueryText((await api<{ text: string }>('GET', `/sessions/${sid}/object/ddl` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind }))).text) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }

  const toggle = (k: string) => setExpanded((x) => { const n = new Set(x); n.has(k) ? n.delete(k) : n.add(k); return n })
  const withCols = (o: ObjRef) => sb.ShowColumns && ['table', 'view', 'matview'].includes(o.Kind)
  const item = (o: DbObject) => (<div key={o.Schema + '.' + o.Name + o.Kind}>
    <div className={`group flex items-center gap-2 h-[26px] pl-3 pr-2.5 cursor-pointer text-[12.5px] hover:bg-panel ${marked.has(keyOf(o)) ? 'bg-raised shadow-[inset_3px_0_0_var(--accent)]' : ''}`} aria-selected={marked.has(keyOf(o)) || undefined} onClick={() => { last.current = o; onOpen(o) }} onContextMenu={(e) => { e.preventDefault(); last.current = o; setMenu(o) }}>
      {withCols(o) && <button className="btn-ghost -ml-2 px-1" aria-label={expanded.has(keyOf(o)) ? 'Hide columns' : 'Show columns'} aria-expanded={expanded.has(keyOf(o))} onClick={(e) => { e.stopPropagation(); toggle(keyOf(o)) }}>{expanded.has(keyOf(o)) ? '▾' : '▸'}</button>}
      <span className="flex-1 truncate" title={o.Name}>{marked.has(keyOf(o)) ? '● ' : ''}{o.Name}</span>
      {o.Detail && <span className="font-mono text-[11px] leading-[normal] text-ink-3 max-w-[7em] truncate text-right group-hover:hidden group-focus-within:hidden" title={o.Detail}>{o.Detail}</span>}
      <span className="hidden group-hover:flex group-focus-within:flex">
        <button className="btn-ghost" title={isPinned(o) ? 'Unpin' : 'Pin'} onClick={(e) => { e.stopPropagation(); pin.mutate(o) }}>{isPinned(o) ? '★' : '☆'}</button>
        <button className="btn-ghost" title="Actions" onClick={(e) => { e.stopPropagation(); setMenu(o) }}>⋯</button>
      </span>
    </div>
    {withCols(o) && expanded.has(keyOf(o)) && <ColumnList sid={sid} obj={o} sort={!!sb.SortColumns} />}
  </div>)

  const isTable = (o: ObjRef) => o.Kind === 'table'

  return (
    <div className={`w-[260px] flex-none border-r border-rule flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-y-0 max-[760px]:left-0 max-[760px]:w-[min(86vw,320px)] max-[760px]:motion-safe:transition-transform max-[760px]:duration-150 max-[760px]:ease-out ${open ? 'max-[760px]:translate-none max-[760px]:shadow-pop' : 'max-[760px]:-translate-x-[102%]'} ${collapsed ? 'hidden max-[760px]:flex' : 'flex'}`} data-ctx="sidebar">
      <div className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule">
        <div className="flex items-center gap-1.5">
          <input ref={filterBox} className="flex-1 min-w-0 bg-panel border-transparent hover:border-rule-2" placeholder="Filter" aria-label="Filter objects" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <button className="btn-ghost" title="Create a database or schema" aria-label="Create" aria-haspopup="dialog" onClick={() => setCreate('menu')}>+</button>
          <button className="btn-ghost" title="Refresh the list" aria-label="Refresh the list" onClick={() => { objects.refetch(); short.refetch() }}>Refresh</button>
        </div>
        {info.caps.Schemas && (
          <Select className="w-full bg-panel border-transparent" aria-label="Schema" value={schema} onChange={setSchema} options={(schemas.data?.schemas ?? [schema]).filter((n) => sb.LoadSystemSchemas || n === schema || !SYSTEM_SCHEMA.test(n))} />
        )}
      </div>
      <div className="overflow-auto flex-1 pt-1 pb-3">
        {objects.isFetching && <div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-accent/20 after:absolute after:inset-0 after:w-2/5 after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Loading objects" />}
        {objects.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading objects…</div>}
        {objects.error && <div className="p-2.5 text-[12.5px] text-del">{objects.error.message}</div>}
        {marked.size > 0 && <div className="flex flex-wrap items-center gap-1 px-3 py-1.5 text-[12px] text-ink-2 border-b border-rule"><span className="flex-1">{marked.size} marked</span>
          <button className="btn-ghost" onClick={() => setMany(marks.filter((o) => o.Kind === 'table'))}>Export…</button><button className="btn-ghost" onClick={() => setAddTo(marks)}>Group…</button>
          <button className="btn-ghost btn-danger" onClick={() => bulk('truncate')}>Truncate…</button><button className="btn-ghost btn-danger" onClick={() => bulk('drop')}>Drop…</button><button className="btn-ghost" onClick={() => setMarked(new Map())}>Clear</button></div>}
        {!filter && pinned.length > 0 && <><div className={TITLE}>Pinned</div>{pinned.map(item)}</>}
        {!filter && sb.ShowRecent !== false && recent.length > 0 && <><div className={TITLE}>Recent</div>{recent.map(item)}</>}
        {groups.map((g) => {
          const its = g.items.map(lookup).filter((o): o is DbObject => !!o && o.Name.toLowerCase().includes(f))
          return its.length ? <div key={g.name}><div className={TITLE}>{g.name}<span className="text-ink-3 text-[11px]">{its.length}</span></div>{its.map(item)}</div> : null
        })}
        {KINDS.filter(([k]) => !hide(k)).map(([k, label]) => {
          const list = all.filter((o) => o.Kind === k && o.Name.toLowerCase().includes(f))
          return list.length ? <div key={k}><div className={TITLE}>{label}<span className="text-ink-3 text-[11px]">{list.length}</span></div>{list.map(item)}</div> : null
        })}
      </div>
      {tool?.kind === 'export' && <ExportDialog sid={sid} notify={notify} onClose={() => setTool(null)} source={{ mode: 'table', ref: tool.obj, filters: { Filters: [], Or: false }, sort: [], columns: [], limit: 0, offset: 0 }} />}
      {tool?.kind === 'import' && <ImportDialog sid={sid} table={tool.obj} notify={notify} onClose={() => setTool(null)} onDone={() => qc.invalidateQueries({ queryKey: ['objects', sid] })} />}
      {create === 'menu' && (
        <Dialog title="Create" onClose={() => setCreate('')}>
          <div className="flex flex-col gap-0.5">
            <button className={MI} autoFocus onClick={() => setCreate('database')}>New {info.caps.DatabaseNoun || 'database'}…</button>
            {schemaOps && <button className={MI} onClick={newSchema}>New schema…</button>}
            {schemaOps && <button className={`${MI} btn-danger`} onClick={dropSchema}>Drop schema…</button>}
          </div>
        </Dialog>
      )}
      {create === 'database' && <NewDatabaseDialog noun={info.caps.DatabaseNoun || 'database'} options={['postgres', 'mysql'].includes(info.caps.Dialect)} onClose={() => setCreate('')} onCreate={(n, opts) => admin('create_database', { Schema: '', Name: n, Kind: '' }, opts)} />}
      {addTo && <GroupDialog items={addTo} groups={groups} onClose={() => setAddTo(null)} onPick={(name) => {
        const keys = addTo.map(keyOf), has = groups.some((g) => g.name === name)
        saveGroups(has ? groups.map((g) => (g.name === name ? { ...g, items: [...new Set([...g.items, ...keys])] } : g)) : [...groups, { name, items: keys }]); setAddTo(null)
      }} />}
      {many && <MultiExportDialog sid={sid} info={info} initial={many} notify={notify} onClose={() => setMany(null)} />}
      {menu && (
        <Dialog title={menu.Name} onClose={() => setMenu(null)}>
          <div className="flex flex-col gap-0.5">
            <button className={MI} onClick={() => { onOpen(menu); setMenu(null) }}>Open</button>
            <button className={MI} onClick={() => ddl(menu)}>View definition</button>
            {isTable(menu) && <button className={MI} onClick={() => { onDiagram(menu); setMenu(null) }}>Diagram</button>}
            <button className={MI} onClick={() => { toggleMark(menu); setMenu(null) }}>{marked.has(keyOf(menu)) ? 'Unmark' : 'Mark'}</button>
            <button className={MI} onClick={() => { setAddTo([menu]); setMenu(null) }}>Add to group…</button>
            {groups.filter((g) => g.items.includes(keyOf(menu))).map((g) => <button key={g.name} className={MI} onClick={() => { saveGroups(groups.map((x) => (x === g ? { ...x, items: x.items.filter((i) => i !== keyOf(menu)) } : x))); setMenu(null) }}>Remove from group {g.name}</button>)}
            <button className={MI} onClick={() => { setTool({ kind: 'export', obj: menu }); setMenu(null) }}>Export…</button>
            {isTable(menu) && <button className={MI} onClick={() => { setMany([menu]); setMenu(null) }}>Export several tables…</button>}
            {isTable(menu) && <button className={MI} onClick={() => { setTool({ kind: 'import', obj: menu }); setMenu(null) }}>Import…</button>}
            <hr className="w-full border-t border-rule my-1" />
            {isTable(menu) && <button className={MI} onClick={() => script(menu, 'select100')}>Script: SELECT top 100</button>}
            <button className={MI} onClick={() => script(menu, 'create')}>Script: CREATE</button>
            <button className={MI} onClick={() => script(menu, 'drop')}>Script: DROP</button>
            <hr className="w-full border-t border-rule my-1" />
            {isTable(menu) && <button className={MI} onClick={async () => { const n = await ask('Clone as'); if (n) admin('clone', menu, { new_name: n, with_data: 'false' }) }}>Clone…</button>}
            <button className={MI} onClick={async () => { const n = await ask('New name', menu.Name); if (n) admin('rename', menu, { new_name: n }) }}>Rename…</button>
            <hr className="w-full border-t border-rule my-1" />
            {isTable(menu) && <button className={`${MI} btn-danger`} onClick={() => admin('truncate', menu)}>Truncate…</button>}
            <button className={`${MI} btn-danger`} onClick={() => admin('drop', menu)}>Drop…</button>
          </div>
        </Dialog>
      )}
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
