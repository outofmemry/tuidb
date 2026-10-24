import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, keyOf, qs, recentKey, type DbObject, type Meta, type ObjRef, type Plan, type SessionInfo } from './api'
import { Dialog } from './Dialogs'

const KINDS: [string, string][] = [['table', 'Tables'], ['view', 'Views'], ['matview', 'Materialized views'], ['function', 'Functions'], ['procedure', 'Procedures'], ['sequence', 'Sequences'], ['type', 'Types']]

type Props = {
  open?: boolean; collapsed?: boolean
  sid: string; info: SessionInfo; meta: Meta
  onOpen: (o: ObjRef) => void; onQueryText: (t: string) => void; onPlan: (p: Plan) => void; notify: (m: string) => void
}

export function Sidebar({ open, collapsed, sid, info, meta, onOpen, onQueryText, onPlan, notify }: Props) {
  const qc = useQueryClient()
  const sb = meta.config.sidebar
  const [schema, setSchema] = useState(info.schema)
  const [filter, setFilter] = useState('')
  const [menu, setMenu] = useState<DbObject | null>(null)
  const filterBox = useRef<HTMLInputElement>(null)
  useEffect(() => {
    const h = (e: Event) => { if ((e as CustomEvent).detail.action === 'sidebar_search') { e.preventDefault(); filterBox.current?.focus(); filterBox.current?.select() } }
    window.addEventListener('tuidb:action', h)
    return () => window.removeEventListener('tuidb:action', h)
  }, [])

  const schemas = useQuery({ queryKey: ['schemas', sid, info.database], queryFn: () => api<{ schemas: string[] | null }>('GET', `/sessions/${sid}/schemas`), enabled: info.caps.Schemas, gcTime: 0 })
  const objects = useQuery({ queryKey: ['objects', sid, info.database, schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema })), gcTime: 0 })
  const short = useQuery({ queryKey: ['shortcuts', sid], queryFn: () => api<{ pinned: string[]; recent: string[] }>('GET', '/state/shortcuts' + qs({ conn: info.name })), gcTime: 0 })
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

  const admin = async (op: string, ref: ObjRef, opts?: Record<string, string>) => {
    setMenu(null)
    try { onPlan(await api<Plan>('POST', `/sessions/${sid}/plan/admin`, { Op: op, Ref: ref, Opts: opts })) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const script = async (o: ObjRef, kind: string) => {
    setMenu(null)
    try { onQueryText((await api<{ text: string }>('GET', `/sessions/${sid}/object/script` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind, script: kind }))).text) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const ddl = async (o: ObjRef) => {
    setMenu(null)
    try { onQueryText((await api<{ text: string }>('GET', `/sessions/${sid}/object/ddl` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind }))).text) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }

  const item = (o: DbObject) => (
    <div key={o.Schema + '.' + o.Name + o.Kind} className="obj" onClick={() => onOpen(o)} onContextMenu={(e) => { e.preventDefault(); setMenu(o) }}>
      <span className="name" title={o.Name}>{o.Name}</span>
      {o.Detail && <span className="det" title={o.Detail}>{o.Detail}</span>}
      <span className="acts">
        <button className="ghost" title={isPinned(o) ? 'Unpin' : 'Pin'} onClick={(e) => { e.stopPropagation(); pin.mutate(o) }}>{isPinned(o) ? '★' : '☆'}</button>
        <button className="ghost" title="Actions" onClick={(e) => { e.stopPropagation(); setMenu(o) }}>⋯</button>
      </span>
    </div>
  )

  const isTable = (o: ObjRef) => o.Kind === 'table'
  const newDb = () => { const n = window.prompt(`New ${info.caps.DatabaseNoun || 'database'} name`); if (n) admin('create_database', { Schema: '', Name: n, Kind: '' }) }

  return (
    <div className={`sidebar ${open ? 'open' : ''} ${collapsed ? 'collapsed' : ''}`} data-ctx="sidebar">
      <div className="side-head">
        <div className="row">
          <input ref={filterBox} className="grow" placeholder="Filter objects" aria-label="Filter objects" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <button className="ghost" title={`New ${info.caps.DatabaseNoun || 'database'}`} aria-label={`New ${info.caps.DatabaseNoun || 'database'}`} onClick={newDb}>New</button>
          <button className="ghost" title="Refresh the list" aria-label="Refresh the list" onClick={() => { objects.refetch(); short.refetch() }}>Refresh</button>
        </div>
        {info.caps.Schemas && (
          <select value={schema} onChange={(e) => setSchema(e.target.value)} aria-label="Schema">
            {(schemas.data?.schemas ?? [schema]).map((s) => <option key={s}>{s}</option>)}
          </select>
        )}
      </div>
      <div className="scroll">
        {objects.isFetching && <div className="progress" role="progressbar" aria-label="Loading objects" />}
        {objects.isLoading && <div className="note dim">Loading objects…</div>}
        {objects.error && <div className="err note">{objects.error.message}</div>}
        {!filter && pinned.length > 0 && <><div className="side-title">Pinned</div>{pinned.map(item)}</>}
        {!filter && sb.ShowRecent !== false && recent.length > 0 && <><div className="side-title">Recent</div>{recent.map(item)}</>}
        {KINDS.filter(([k]) => !hide(k)).map(([k, label]) => {
          const list = all.filter((o) => o.Kind === k && o.Name.toLowerCase().includes(f))
          return list.length ? <div key={k}><div className="side-title">{label}<span>{list.length}</span></div>{list.map(item)}</div> : null
        })}
      </div>
      {menu && (
        <Dialog title={menu.Name} onClose={() => setMenu(null)}>
          <div className="menu">
            <button onClick={() => { onOpen(menu); setMenu(null) }}>Open</button>
            <button onClick={() => ddl(menu)}>View definition</button>
            <hr />
            {isTable(menu) && <button onClick={() => script(menu, 'select100')}>Script: SELECT top 100</button>}
            <button onClick={() => script(menu, 'create')}>Script: CREATE</button>
            <button onClick={() => script(menu, 'drop')}>Script: DROP</button>
            <hr />
            {isTable(menu) && <button onClick={() => { const n = window.prompt('Clone as'); if (n) admin('clone', menu, { new_name: n, with_data: 'false' }) }}>Clone…</button>}
            <button onClick={() => { const n = window.prompt('New name', menu.Name); if (n) admin('rename', menu, { new_name: n }) }}>Rename…</button>
            <hr />
            {isTable(menu) && <button className="danger" onClick={() => admin('truncate', menu)}>Truncate…</button>}
            <button className="danger" onClick={() => admin('drop', menu)}>Drop…</button>
          </div>
        </Dialog>
      )}
    </div>
  )
}
