import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, keyOf, qs, recentKey, type DbObject, type Meta, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { Dialog, ask } from './Dialogs'
import { Select } from './Select'

const TITLE = 'flex items-baseline gap-1.5 px-3 pt-3.5 pb-1 text-[12px] text-ink-2'
const MI = 'text-left border-transparent hover:border-ink-3' // the old menu kept the base hover border

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
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
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
    <div key={o.Schema + '.' + o.Name + o.Kind} className="group flex items-center gap-2 h-[26px] pl-3 pr-2.5 cursor-pointer text-[12.5px] hover:bg-panel" onClick={() => onOpen(o)} onContextMenu={(e) => { e.preventDefault(); setMenu(o) }}>
      <span className="flex-1 truncate" title={o.Name}>{o.Name}</span>
      {o.Detail && <span className="font-mono text-[11px] leading-[normal] text-ink-3 max-w-[7em] truncate text-right group-hover:hidden group-focus-within:hidden" title={o.Detail}>{o.Detail}</span>}
      <span className="hidden group-hover:flex group-focus-within:flex">
        <button className="btn-ghost" title={isPinned(o) ? 'Unpin' : 'Pin'} onClick={(e) => { e.stopPropagation(); pin.mutate(o) }}>{isPinned(o) ? '★' : '☆'}</button>
        <button className="btn-ghost" title="Actions" onClick={(e) => { e.stopPropagation(); setMenu(o) }}>⋯</button>
      </span>
    </div>
  )

  const isTable = (o: ObjRef) => o.Kind === 'table'
  const newDb = async () => { const n = await ask(`New ${info.caps.DatabaseNoun || 'database'} name`); if (n) admin('create_database', { Schema: '', Name: n, Kind: '' }) }

  return (
    <div className={`w-[260px] flex-none border-r border-rule flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-y-0 max-[760px]:left-0 max-[760px]:w-[min(86vw,320px)] max-[760px]:motion-safe:transition-transform max-[760px]:duration-150 max-[760px]:ease-out ${open ? 'max-[760px]:translate-none max-[760px]:shadow-pop' : 'max-[760px]:-translate-x-[102%]'} ${collapsed ? 'hidden max-[760px]:flex' : 'flex'}`} data-ctx="sidebar">
      <div className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule">
        <div className="flex items-center gap-1.5">
          <input ref={filterBox} className="flex-1 min-w-0 bg-panel border-transparent hover:border-rule-2" placeholder="Filter" aria-label="Filter objects" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <button className="btn-ghost" title={`New ${info.caps.DatabaseNoun || 'database'}`} aria-label={`New ${info.caps.DatabaseNoun || 'database'}`} onClick={newDb}>New</button>
          <button className="btn-ghost" title="Refresh the list" aria-label="Refresh the list" onClick={() => { objects.refetch(); short.refetch() }}>Refresh</button>
        </div>
        {info.caps.Schemas && (
          <Select className="w-full bg-panel border-transparent" aria-label="Schema" value={schema} onChange={setSchema} options={schemas.data?.schemas ?? [schema]} />
        )}
      </div>
      <div className="overflow-auto flex-1 pt-1 pb-3">
        {objects.isFetching && <div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-accent/20 after:absolute after:inset-0 after:w-2/5 after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Loading objects" />}
        {objects.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading objects…</div>}
        {objects.error && <div className="p-2.5 text-[12.5px] text-del">{objects.error.message}</div>}
        {!filter && pinned.length > 0 && <><div className={TITLE}>Pinned</div>{pinned.map(item)}</>}
        {!filter && sb.ShowRecent !== false && recent.length > 0 && <><div className={TITLE}>Recent</div>{recent.map(item)}</>}
        {KINDS.filter(([k]) => !hide(k)).map(([k, label]) => {
          const list = all.filter((o) => o.Kind === k && o.Name.toLowerCase().includes(f))
          return list.length ? <div key={k}><div className={TITLE}>{label}<span className="text-ink-3 text-[11px]">{list.length}</span></div>{list.map(item)}</div> : null
        })}
      </div>
      {menu && (
        <Dialog title={menu.Name} onClose={() => setMenu(null)}>
          <div className="flex flex-col gap-0.5">
            <button className={MI} onClick={() => { onOpen(menu); setMenu(null) }}>Open</button>
            <button className={MI} onClick={() => ddl(menu)}>View definition</button>
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
