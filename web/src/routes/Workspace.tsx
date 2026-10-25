import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, ApiError, clip, qs, recentKey, sameRef, type Connection, type Meta, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { DataTab } from '../components/DataTab'
import { Dialog, PlanDialog, confirm } from '../components/Dialogs'
import { QueryTab } from '../components/QueryTab'
import { Sidebar } from '../components/Sidebar'
import { Logo } from '../components/logos'
import { Machine, canon, contextOf, display } from '../utils/keys'

// Actions the web UI answers (its own plus the ones tabs and the sidebar handle via the webtuidb:action event).
// Everything else in the keymap is listed as "not in the web UI" so the help screen stays identical to the terminal's.
export const SUPPORTED = new Set(['help', 'new_query_tab', 'new_tab', 'close_tab', 'next_tab', 'prev_tab', 'toggle_sidebar', 'disconnect', 'reload', 'open_connection', 'new_session', 'switch_session', 'quit',
  'reload_tab', 'filters', 'columns', 'add_row', 'duplicate_rows', 'delete_rows', 'commit', 'preview', 'discard', 'undo', 'next_page', 'prev_page', 'first_row', 'last_row', 'count_exact',
  'show_data', 'show_structure', 'show_json', 'toggle_row_detail', 'quick_look', 'run_current', 'run_all', 'cancel_query', 'add_to_queries', 'set_limit', 'sidebar_search', 'apply_filters',
  ...Array.from({ length: 9 }, (_, i) => `tab_${i + 1}`)])

type Tab = { id: number; kind: 'table'; obj: ObjRef } | { id: number; kind: 'query'; n: number; text: string }

export function Workspace({ sid }: { sid: string }) {
  const qc = useQueryClient()
  const nav = useNavigate()
  const info = useQuery({ queryKey: ['session', sid], queryFn: () => api<SessionInfo>('GET', `/sessions/${sid}`), retry: false, gcTime: 0 })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  const dbs = useQuery({ queryKey: ['dbs', sid, info.data?.database], queryFn: () => api<{ databases: string[] }>('GET', `/sessions/${sid}/databases`), enabled: !!info.data, gcTime: 0 })

  const [tabs, setTabs] = useState<Tab[]>([])
  const [active, setActive] = useState(0)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [drawer, setDrawer] = useState<'' | 'history' | 'queries'>('')
  const [side, setSide] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const [help, setHelp] = useState(false)
  const [switcher, setSwitcher] = useState(false)
  const [dbMenu, setDbMenu] = useState(false)
  const setAskDb = (from: string | null) => { try { from ? sessionStorage.setItem(ASK_DB, from) : sessionStorage.removeItem(ASK_DB) } catch { /* private mode */ } }
  const nDbs = dbs.data?.databases.length
  useEffect(() => { // new_session: once the workspace we switched to has its database list, offer it
    let from: string | null = null
    try { from = sessionStorage.getItem(ASK_DB) } catch { /* private mode */ }
    if (!from || from === sid || nDbs === undefined) return
    setAskDb(null)
    if (nDbs > 1) setDbMenu(true)
  }, [sid, nDbs])
  const conns = useQuery({ queryKey: ['conns'], queryFn: () => api<Connection[]>('GET', '/connections'), enabled: switcher })
  const [pending, setPending] = useState<string | null>(null) // context the prefix was pressed in, for which-key
  const machine = useRef<Machine | null>(null)
  const act = useRef<(a: string) => void>(() => {})
  const [toast, setToast] = useState('')
  const [details, setDetails] = useState('') // the full text behind a clipped toast
  const seq = useRef(1)
  const queries = useRef(0)

  // A toast is one short line; a server error can run to pages, so the rest waits behind "Details" and the toast stays until dismissed.
  const notify = useCallback((m: string) => { setToast(m); setDetails(''); if (m.length <= 140 && !m.includes('\n')) setTimeout(() => setToast((t) => (t === m ? '' : t)), 3500) }, [])
  const km = meta.data?.keymap
  useEffect(() => {
    if (!km) return
    const m = (machine.current = new Machine(km))
    const onKey = (e: KeyboardEvent) => {
      const k = canon(e); if (!k) return
      const ctx = contextOf(), wasPending = m.pending
      const r = m.feed(ctx, k)
      if (r.passthrough) return
      if (r.action && !wasPending && !SUPPORTED.has(r.action)) return // a no-prefix key for an action the web lacks: let the browser have it
      e.preventDefault(); e.stopPropagation()
      setPending(r.pending ? ctx : null)
      if (r.action) act.current(r.action)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [km])
  if (info.error) { forget(sid); return <div className="min-h-full bg-[radial-gradient(var(--rule)_1px,transparent_1px)] bg-[size:22px_22px]"><div className="max-w-[920px] mx-auto px-7 pt-10 pb-20 max-[760px]:px-4 max-[760px]:pt-7 max-[760px]:pb-12"><header className="flex items-baseline gap-4"><h1 className={START_H1}>This session has ended</h1></header><p className="mt-2.5 max-w-[56ch] text-[15px] text-ink-2">{info.error.message}. Connect again from the connections list.</p><p className="my-[1em]"><button className="btn-primary" onClick={() => nav({ to: '/' })}>Back to connections</button></p></div></div> }
  if (!info.data || !meta.data) return <div className="flex flex-col h-full" role="status"><div className={PROGRESS} aria-label="Connecting" /><div className={EMPTY}>Connecting…</div></div>
  const i = info.data
  remember(i.name, sid)
  const accent = i.color ? ({ '--accent': i.color, '--on-accent': onAccent(i.color) } as CSSProperties) : undefined

  const openObj = (o: ObjRef) => {
    if (!['table', 'view', 'matview'].includes(o.Kind)) return
    const ex = tabs.find((t) => t.kind === 'table' && sameRef(t.obj, o))
    if (ex) return setActive(ex.id)
    const id = seq.current++
    setSide(false)
    setTabs((t) => [...t, { id, kind: 'table', obj: o }]); setActive(id)
    api('POST', '/state/touch', { Conn: i.name, Item: recentKey(o) }).then(() => qc.invalidateQueries({ queryKey: ['shortcuts', sid] })).catch(() => {})
  }
  const newQuery = (text = '') => {
    const id = seq.current++
    setSide(false)
    setTabs((t) => [...t, { id, kind: 'query', n: ++queries.current, text }]); setActive(id)
  }
  const close = (id: number) => {
    setTabs((t) => t.filter((x) => x.id !== id))
    if (active === id) setActive(tabs.find((t) => t.id !== id)?.id ?? 0)
  }
  const switchDb = async (name: string) => {
    if (tabs.length && !(await confirm('Switching closes all open tabs. Continue?'))) return
    try { await api('POST', `/sessions/${sid}/database`, { Name: name }); setTabs([]); setActive(0); qc.invalidateQueries() } catch (e) { notify(String(e)) }
  }
  const disconnect = async () => {
    try { await api('DELETE', `/sessions/${sid}`) } catch (e) { if (!(e instanceof ApiError && e.status === 404)) return notify(`Could not disconnect: ${e instanceof Error ? e.message : e}. Try again.`) }
    forget(sid); qc.clear(); nav({ to: '/' }) }
  // Switching keeps this session open on the server, so coming back is instant; Disconnect closes it.
  const switchTo = async (name: string) => {
    setSwitcher(false)
    const open = openSessions()[name]
    if (open && open !== sid) return nav({ to: '/s/$sessionId', params: { sessionId: open } })
    if (open === sid) return
    try { const r = await api<{ id: string }>('POST', '/sessions', { Name: name }); nav({ to: '/s/$sessionId', params: { sessionId: r.id } }) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const bindings = meta.data.keymap.bindings
  act.current = (action: string) => {
    const idx = tabs.findIndex((t) => t.id === active)
    const go = (j: number) => tabs.length && setActive(tabs[(j + tabs.length) % tabs.length].id)
    const n = /^tab_(\d)$/.exec(action)
    switch (action) {
      case 'help': return setHelp(true)
      case 'new_query_tab': case 'new_tab': return newQuery()
      case 'close_tab': return active && close(active)
      case 'next_tab': return go(idx + 1)
      case 'prev_tab': return go(idx - 1)
      case 'toggle_sidebar': setSide((s) => !s); return setCollapsed((c) => !c)
      case 'disconnect': case 'quit': return disconnect()
      case 'reload': return qc.invalidateQueries()
      case 'open_connection': case 'switch_session': return setSwitcher(true)
      case 'new_session': setAskDb(sid); return setSwitcher(true)
    }
    if (n) return tabs[Number(n[1]) - 1] && setActive(tabs[Number(n[1]) - 1].id)
    // tab-level actions: the active tab (or the sidebar) claims the event by calling preventDefault
    const ev = new CustomEvent('webtuidb:action', { detail: { action }, cancelable: true })
    window.dispatchEvent(ev)
    if (!ev.defaultPrevented) notify(SUPPORTED.has(action) ? `${bindings.find((b) => b.Action === action)?.Desc ?? action}: nothing to do here` : `Not in the web UI: ${bindings.find((b) => b.Action === action)?.Desc ?? action}`)
  }

  return (
    <div className="flex flex-col h-full" style={accent}>
      <div className="relative flex items-center gap-3 h-10 text-[12px] pr-2.5 bg-panel border-b border-rule font-mono max-[760px]:h-auto max-[760px]:min-h-10 max-[760px]:flex-wrap max-[760px]:gap-x-2.5 max-[760px]:gap-y-1.5 max-[760px]:pr-2">
        <div className="self-stretch flex items-center gap-2.5 px-3.5 bg-accent text-on-accent font-semibold text-[13px] whitespace-nowrap max-[760px]:self-auto max-[760px]:h-10">
          <button className={`${BADGE_GHOST} hover:bg-transparent! hidden max-[760px]:inline-block`} onClick={() => setSide(!side)} aria-expanded={side}>Objects</button>
          <button className={`${BADGE_GHOST} flex items-center gap-2 -mx-1.5 font-semibold text-[14px] hover:bg-[rgb(255_255_255/0.16)]! aria-expanded:bg-[rgb(255_255_255/0.16)]!`} onClick={() => setSwitcher(!switcher)} aria-expanded={switcher} aria-haspopup="menu" title="Switch connection">
            <Logo driver={i.driver} size={16} className="text-inherit opacity-85" />{i.name}<span className={CARET} aria-hidden>▾</span>
          </button>
        </div>
        {switcher && (
          <>
            <div className="fixed inset-0 z-40" onClick={() => { setSwitcher(false); setAskDb(null) }} aria-hidden />
            <div className={`${SWITCHER} top-10 left-0`} role="menu" aria-label="Connections">
              {conns.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading…</div>}
              {(conns.data ?? []).map((c) => {
                const open = openSessions()[c.Name]
                return (
                  <button key={c.Name} role="menuitem" className={`${SW_ITEM} ${c.Name === i.name ? 'btn-on' : ''}`} onClick={() => switchTo(c.Name)} style={{ '--c': c.Color || undefined } as CSSProperties}>
                    <span className="w-1 h-[18px] rounded-[1px] bg-[var(--c,var(--rule-2))]" aria-hidden /><Logo driver={c.Driver} size={14} /><span className="flex-1 min-w-0 truncate">{c.Name}</span>
                    {open && <span className="text-ink-2">{c.Name === i.name ? 'current' : 'open'}</span>}
                  </button>
                )
              })}
              <hr className="border-t border-rule my-1.5" />
              <button role="menuitem" className={SW_ITEM} onClick={() => nav({ to: '/' })}>All connections</button>
            </div>
          </>
        )}
        <span className="font-mono text-[11.5px] text-ink-2 truncate max-[760px]:hidden">{i.serverInfo}</span>
        <span className="px-2 border border-rule-2 rounded-[2px] text-[11.5px] text-ink-2 whitespace-nowrap" title="Safe mode decides what needs confirmation before it is sent">{i.levelLabel}</span>
        {(dbs.data?.databases.length ?? 0) > 1 && (
          <div className="relative">
            <button className="flex items-center gap-1.5 px-2 py-0.5 font-medium text-[14px] border-transparent hover:border-rule-2 aria-expanded:border-rule-2" onClick={() => setDbMenu(!dbMenu)} aria-expanded={dbMenu} aria-haspopup="menu" title={`Switch ${i.caps.DatabaseNoun || 'database'}`}>{i.database}<span className={CARET} aria-hidden>▾</span></button>
            {dbMenu && (
              <>
                <div className="fixed inset-0 z-40" onClick={() => setDbMenu(false)} aria-hidden />
                <div className={`${SWITCHER} top-7 -left-2`} role="menu" aria-label={i.caps.DatabaseNoun || 'Databases'}>
                  {dbs.data!.databases.map((d) => (
                    <button key={d} role="menuitem" className={`${SW_ITEM} ${d === i.database ? 'btn-on' : ''}`} autoFocus={d === i.database} onClick={() => { setDbMenu(false); if (d !== i.database) switchDb(d) }}>
                      <span className="flex-1 min-w-0 truncate">{d}</span>{d === i.database && <span className="text-ink-2">current</span>}
                    </button>
                  ))}
                </div>
              </>
            )}
          </div>
        )}
        <span className="flex-1 min-w-0" />
        <button onClick={() => newQuery()}>New query</button>
        <button className={drawer === 'history' ? 'btn-on' : ''} onClick={() => setDrawer(drawer === 'history' ? '' : 'history')}>History</button>
        <button className={drawer === 'queries' ? 'btn-on' : ''} onClick={() => setDrawer(drawer === 'queries' ? '' : 'queries')}>Saved queries</button>
        <button onClick={disconnect}>Disconnect</button>
      </div>
      <div className="flex flex-1 min-h-0 relative">
        {side && <div className="hidden max-[760px]:block fixed inset-0 z-[25] bg-backdrop" onClick={() => setSide(false)} aria-hidden />}
        <Sidebar key={i.database} open={side} collapsed={collapsed} sid={sid} info={i} meta={meta.data} onOpen={openObj} onQueryText={newQuery} onPlan={setPlan} notify={notify} />
        <div className="flex-1 min-w-0 flex flex-col bg-canvas">
          <div className="flex h-8 text-[12px] border-b border-rule overflow-x-auto [scrollbar-width:none] bg-canvas font-mono">
            {tabs.map((t) => (
              <div key={t.id} className={`group flex items-center gap-2 pl-3.5 pr-2.5 cursor-pointer whitespace-nowrap border-r border-rule hover:text-ink hover:bg-panel ${t.id === active ? 'text-ink bg-panel shadow-[inset_0_-2px_0_var(--accent)]' : 'text-ink-2'}`} onClick={() => setActive(t.id)}>
                {t.kind === 'table' ? t.obj.Name : <>Query <span className="font-mono text-[11px] text-ink-3">{t.n}</span></>}
                <button className={`btn-ghost ${t.id === active ? '' : 'opacity-0 group-hover:opacity-100'}`} onClick={(e) => { e.stopPropagation(); close(t.id) }}>✕</button>
              </div>
            ))}
          </div>
          {tabs.length === 0 && <div className={EMPTY}>Pick a table from the list, or start a new query.</div>}
          {tabs.map((t) => (
            <div key={t.id} className="flex-1 min-h-0 flex flex-col" hidden={t.id !== active}>
              {t.kind === 'table'
                ? <DataTab sid={sid} info={i} meta={meta.data!} obj={t.obj} notify={notify} active={t.id === active} />
                : <QueryTab sid={sid} info={i} initial={t.text} notify={notify} shown={t.id === active} />}
            </div>
          ))}
        </div>
        {drawer && <Drawer kind={drawer} conn={i.name} notify={notify} onUse={(t) => newQuery(t)} onClose={() => setDrawer('')} />}
      </div>
      {plan && <PlanDialog sid={sid} plan={plan} onClose={() => setPlan(null)} onApplied={() => { setPlan(null); qc.invalidateQueries(); notify('Changes sent') }} />}
      {pending && machine.current && (
        <div className="popover fixed right-3 bottom-3 z-[45] w-[min(760px,calc(100vw-24px))] grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-x-[18px] gap-y-[3px] px-3.5 py-2.5 max-h-[60vh] overflow-auto text-[12px] font-mono" role="status" aria-label="Keys after the prefix">
          <div className={`${WK_ROW} col-span-full mb-1 text-ink-2`}>{display(km!.prefix)} then</div>
          {machine.current.bindings(pending).map((b) => (
            <div key={b.Context + b.Key} className={`${WK_ROW} ${SUPPORTED.has(b.Action) ? '' : 'text-ink-2'}`}><kbd>{display(b.Key)}</kbd>{b.Desc}</div>
          ))}
        </div>
      )}
      {help && <Help km={km!} onClose={() => setHelp(false)} />}
      {toast && (
        <div className="fixed bottom-[18px] left-1/2 -translate-x-1/2 flex items-center gap-2.5 bg-ink text-canvas py-1.5 pr-2 pl-4 rounded-[2px] z-[60] font-mono font-medium max-w-[min(720px,90vw)] shadow-pop" role="status">
          <span className="flex-1 min-w-0 truncate">{clip(toast.split('\n')[0], 140)}</span>
          {(toast.length > 140 || toast.includes('\n')) && <button className={TOAST_GHOST} onClick={() => setDetails(toast)}>Details</button>}
          <button className={TOAST_GHOST} onClick={() => setToast('')} aria-label="Dismiss">✕</button>
        </div>
      )}
      {details && <Dialog title="Details" onClose={() => setDetails('')}><pre className="bg-canvas border border-rule rounded-[2px] px-3 py-2.5 max-h-[40vh] overflow-auto text-[12px] whitespace-pre-wrap m-0">{details}</pre><div className={ACTIONS}><button onClick={() => navigator.clipboard?.writeText(details)}>Copy</button><button className="btn-primary" onClick={() => setDetails('')}>Close</button></div></Dialog>}
    </div>
  )
}

const PROGRESS = "relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-accent/20 after:absolute after:inset-0 after:w-2/5 after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60"
const EMPTY = 'px-8 py-14 max-w-[46ch] text-ink-2 text-[15px] leading-normal'
const START_H1 = "m-0 text-[36px] leading-none tracking-[-.03em] font-medium font-mono max-[760px]:text-[32px] after:inline-block after:w-[.5em] after:h-[.85em] after:ml-[.12em] after:align-[-.08em] after:bg-accent after:animate-blink motion-reduce:after:animate-none"
const BADGE_GHOST = 'btn-ghost text-inherit! opacity-75 hover:opacity-100 hover:border-transparent!'
const CARET = 'text-[10px] opacity-70'
const SWITCHER = 'popover absolute z-[41] w-max min-w-[240px] max-w-[min(480px,calc(100vw-24px))] max-h-[70vh] overflow-auto m-0 p-1.5'
const SW_ITEM = 'flex items-center gap-2.5 w-full text-left border-transparent hover:border-ink-3'
const WK_ROW = 'flex gap-2 items-center whitespace-nowrap'
const TOAST_GHOST = 'btn-ghost text-inherit! opacity-80 flex-none hover:opacity-100 hover:bg-[rgb(128_128_128/0.25)]!'
const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
const HIST = 'px-3 py-2.5 border-b border-rule cursor-pointer hover:bg-panel'
const HIST_PRE = 'm-0 mb-1 whitespace-pre-wrap [word-break:break-word] text-[11.5px] max-h-[5em] overflow-hidden'

// Open sessions survive navigation within this browser tab, so the switcher can jump back to one.
const OPEN = 'webtuidb-open'
const ASK_DB = 'webtuidb-ask-db' // new_session: focus the database picker once the next workspace has loaded
function openSessions(): Record<string, string> { try { return JSON.parse(sessionStorage.getItem(OPEN) || '{}') } catch { return {} } }
function remember(name: string, sid: string) { const o = openSessions(); if (o[name] !== sid) { o[name] = sid; try { sessionStorage.setItem(OPEN, JSON.stringify(o)) } catch { /* private mode */ } } }
function forget(sid: string) { const o = openSessions(); for (const k in o) if (o[k] === sid) delete o[k]; try { sessionStorage.setItem(OPEN, JSON.stringify(o)) } catch { /* private mode */ } }

// Help lists every binding the terminal UI has, in the same order, so both screens read the same.
function Help({ km, onClose }: { km: Meta['keymap']; onClose: () => void }) {
  const ctxs = [...new Set(km.bindings.map((b) => b.Context))]
  return (
    <Dialog title="Key bindings" onClose={onClose}>
      <p className="text-ink-2" style={{ margin: '0 0 10px' }}>Press <kbd>{display(km.prefix)}</kbd> then a key. <kbd>Esc</kbd> cancels, pressing the prefix twice sends it to the page. Greyed bindings only exist in the terminal UI.</p>
      <div className="grid grid-cols-[repeat(auto-fill,minmax(280px,1fr))] gap-x-7 gap-y-2 font-mono">
        {ctxs.map((c) => (
          <section key={c}>
            <h3 className="mt-3 mb-1 text-[12.5px] text-ink-2">{c}</h3>
            {km.bindings.filter((b) => b.Context === c).map((b) => (
              <div key={b.Action + b.Key} className={`flex items-center gap-2.5 py-0.5 text-[12.5px] ${SUPPORTED.has(b.Action) ? '' : 'text-ink-2'}`} title={SUPPORTED.has(b.Action) ? undefined : 'Not in the web UI'}>
                <kbd className="min-w-[7em] text-center">{b.NoPrefix ? '' : display(km.prefix) + ' '}{display(b.Key)}</kbd><span>{b.Desc}</span>
              </div>
            ))}
          </section>
        ))}
      </div>
      <div className={ACTIONS}><button onClick={onClose}>Close</button></div>
    </Dialog>
  )
}

// Text colour that reads on the connection's colour; non-hex colours fall back to the default.
function onAccent(hex: string) {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim())
  if (!m) return undefined
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(m[1].slice(i, i + 2), 16) / 255)
  return 0.2126 * r + 0.7152 * g + 0.0722 * b > 0.45 ? '#1e1f23' : '#f6f5f1'
}

function Drawer({ kind, conn, notify, onUse, onClose }: { kind: 'history' | 'queries'; conn: string; notify: (m: string) => void; onUse: (t: string) => void; onClose: () => void }) {
  const [q, setQ] = useState('')
  const hist = useQuery({ queryKey: ['history', conn, q], queryFn: () => api<{ id: string; SQL?: string; sql: string; time: string }[]>('GET', '/history' + qs({ conn, q, limit: 200 })), enabled: kind === 'history', gcTime: 0 })
  const saved = useQuery({ queryKey: ['queries'], queryFn: () => api<{ Path: string; Name: string; Folder: string; SQL: string }[]>('GET', '/queries'), enabled: kind === 'queries', gcTime: 0 })
  return (
    <div className="w-[340px] flex-none border-l border-rule flex flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-0 max-[760px]:w-full">
      <div className="toolbar"><strong className="flex-1 min-w-0 font-medium">{kind === 'history' ? 'History' : 'Saved queries'}</strong>
        {kind === 'history' && <button className="btn-ghost" onClick={async () => { if (!(await confirm('Clear history for this connection?'))) return; try { await api('DELETE', '/history' + qs({ conn })) } catch (e) { notify(`Could not clear history: ${e instanceof Error ? e.message : e}`) } finally { hist.refetch() } }}>Clear</button>}
        <button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      {kind === 'history' && <input className="mx-2.5 mt-2 mb-1 bg-panel border-transparent" placeholder="Search history" value={q} onChange={(e) => setQ(e.target.value)} />}
      <div className="overflow-auto flex-1">
        {(kind === 'history' ? hist : saved).error && <div className="text-del p-2.5 text-[12.5px]">{(kind === 'history' ? hist : saved).error!.message} <button className="btn-ghost" onClick={() => (kind === 'history' ? hist : saved).refetch()}>Retry</button></div>}
        {kind === 'history' && (hist.data ?? []).map((h) => (
          <div key={h.id} className={HIST} onClick={() => onUse(h.sql)}><pre className={HIST_PRE}>{h.sql}</pre><div className="text-ink-2 text-[11.5px]">{new Date(h.time).toLocaleString()}</div></div>
        ))}
        {kind === 'queries' && (saved.data ?? []).map((s) => (
          <div key={s.Path} className={HIST} onClick={() => onUse(s.SQL)}><strong className="block mb-1 font-medium">{s.Folder ? s.Folder + ' / ' : ''}{s.Name}</strong><pre className={HIST_PRE}>{s.SQL}</pre></div>
        ))}
      </div>
    </div>
  )
}
