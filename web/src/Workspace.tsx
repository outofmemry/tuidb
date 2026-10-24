import { useCallback, useEffect, useRef, useState, type CSSProperties } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, ApiError, clip, qs, recentKey, sameRef, type Connection, type Meta, type ObjRef, type Plan, type SessionInfo } from './api'
import { DataTab } from './DataTab'
import { Dialog, PlanDialog } from './Dialogs'
import { QueryTab } from './QueryTab'
import { Sidebar } from './Sidebar'
import { Logo } from './logos'
import { Machine, canon, contextOf, display } from './keys'

// Actions the web UI answers (its own plus the ones tabs and the sidebar handle via the tuidb:action event).
// Everything else in the keymap is listed as "not in the web UI" so the help screen stays identical to the terminal's.
export const SUPPORTED = new Set(['help', 'new_query_tab', 'new_tab', 'close_tab', 'next_tab', 'prev_tab', 'toggle_sidebar', 'disconnect', 'reload', 'open_connection', 'quit',
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
  if (info.error) { forget(sid); return <div className="start"><header><h1>This session has ended</h1></header><p className="lede">{info.error.message}. Connect again from the connections list.</p><p><button className="primary" onClick={() => nav({ to: '/' })}>Back to connections</button></p></div> }
  if (!info.data || !meta.data) return <div className="app" role="status"><div className="progress" aria-label="Connecting" /><div className="empty">Connecting…</div></div>
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
    if (tabs.length && !window.confirm('Switching closes all open tabs. Continue?')) return
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
      case 'open_connection': return setSwitcher(true)
    }
    if (n) return tabs[Number(n[1]) - 1] && setActive(tabs[Number(n[1]) - 1].id)
    // tab-level actions: the active tab (or the sidebar) claims the event by calling preventDefault
    const ev = new CustomEvent('tuidb:action', { detail: { action }, cancelable: true })
    window.dispatchEvent(ev)
    if (!ev.defaultPrevented) notify(SUPPORTED.has(action) ? `${bindings.find((b) => b.Action === action)?.Desc ?? action}: nothing to do here` : `Not in the web UI: ${bindings.find((b) => b.Action === action)?.Desc ?? action}`)
  }

  return (
    <div className="app" style={accent}>
      <div className="topbar">
        <div className="badge">
          <button className="ghost only-narrow" onClick={() => setSide(!side)} aria-expanded={side}>Objects</button>
          <button className="ghost switch" onClick={() => setSwitcher(!switcher)} aria-expanded={switcher} aria-haspopup="menu" title="Switch connection">
            <Logo driver={i.driver} size={16} />{i.name}<span className="caret" aria-hidden>▾</span>
          </button>
        </div>
        {switcher && (
          <>
            <div className="backdrop" onClick={() => setSwitcher(false)} aria-hidden />
            <div className="switcher popover" role="menu" aria-label="Connections">
              {conns.isLoading && <div className="note dim">Loading…</div>}
              {(conns.data ?? []).map((c) => {
                const open = openSessions()[c.Name]
                return (
                  <button key={c.Name} role="menuitem" className={c.Name === i.name ? 'on' : ''} onClick={() => switchTo(c.Name)} style={{ '--c': c.Color || undefined } as CSSProperties}>
                    <span className="sw" aria-hidden /><Logo driver={c.Driver} size={14} /><span className="grow">{c.Name}</span>
                    {open && <span className="dim">{c.Name === i.name ? 'current' : 'open'}</span>}
                  </button>
                )
              })}
              <hr />
              <button role="menuitem" onClick={() => nav({ to: '/' })}>All connections</button>
            </div>
          </>
        )}
        <span className="info">{i.serverInfo}</span>
        <span className="level" title="Safe mode decides what needs confirmation before it is sent">{i.levelLabel}</span>
        {(dbs.data?.databases.length ?? 0) > 1 && (
          <select value={i.database} onChange={(e) => switchDb(e.target.value)} title={i.caps.DatabaseNoun}>
            {dbs.data!.databases.map((d) => <option key={d}>{d}</option>)}
          </select>
        )}
        <span className="grow" />
        <button onClick={() => newQuery()}>New query</button>
        <button className={drawer === 'history' ? 'on' : ''} onClick={() => setDrawer(drawer === 'history' ? '' : 'history')}>History</button>
        <button className={drawer === 'queries' ? 'on' : ''} onClick={() => setDrawer(drawer === 'queries' ? '' : 'queries')}>Saved queries</button>
        <button onClick={disconnect}>Disconnect</button>
      </div>
      <div className="main">
        {side && <div className="scrim" onClick={() => setSide(false)} aria-hidden />}
        <Sidebar key={i.database} open={side} collapsed={collapsed} sid={sid} info={i} meta={meta.data} onOpen={openObj} onQueryText={newQuery} onPlan={setPlan} notify={notify} />
        <div className="content">
          <div className="tabs">
            {tabs.map((t) => (
              <div key={t.id} className={`tab ${t.id === active ? 'on' : ''}`} onClick={() => setActive(t.id)}>
                {t.kind === 'table' ? t.obj.Name : <>Query <span className="q">{t.n}</span></>}
                <button className="ghost" onClick={(e) => { e.stopPropagation(); close(t.id) }}>✕</button>
              </div>
            ))}
          </div>
          {tabs.length === 0 && <div className="empty">Pick a table from the list, or start a new query.</div>}
          {tabs.map((t) => (
            <div key={t.id} className="pane" hidden={t.id !== active}>
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
        <div className="whichkey" role="status" aria-label="Keys after the prefix">
          <div className="dim">{display(km!.prefix)} then</div>
          {machine.current.bindings(pending).map((b) => (
            <div key={b.Context + b.Key} className={SUPPORTED.has(b.Action) ? '' : 'dim'}><kbd>{display(b.Key)}</kbd>{b.Desc}</div>
          ))}
        </div>
      )}
      {help && <Help km={km!} onClose={() => setHelp(false)} />}
      {toast && (
        <div className="toast" role="status">
          <span className="grow">{clip(toast.split('\n')[0], 140)}</span>
          {(toast.length > 140 || toast.includes('\n')) && <button className="ghost" onClick={() => setDetails(toast)}>Details</button>}
          <button className="ghost" onClick={() => setToast('')} aria-label="Dismiss">✕</button>
        </div>
      )}
      {details && <Dialog title="Details" onClose={() => setDetails('')}><pre>{details}</pre><div className="actions"><button onClick={() => navigator.clipboard?.writeText(details)}>Copy</button><button className="primary" onClick={() => setDetails('')}>Close</button></div></Dialog>}
    </div>
  )
}

// Open sessions survive navigation within this browser tab, so the switcher can jump back to one.
const OPEN = 'tuidb-open'
function openSessions(): Record<string, string> { try { return JSON.parse(sessionStorage.getItem(OPEN) || '{}') } catch { return {} } }
function remember(name: string, sid: string) { const o = openSessions(); if (o[name] !== sid) { o[name] = sid; try { sessionStorage.setItem(OPEN, JSON.stringify(o)) } catch { /* private mode */ } } }
function forget(sid: string) { const o = openSessions(); for (const k in o) if (o[k] === sid) delete o[k]; try { sessionStorage.setItem(OPEN, JSON.stringify(o)) } catch { /* private mode */ } }

// Help lists every binding the terminal UI has, in the same order, so both screens read the same.
function Help({ km, onClose }: { km: Meta['keymap']; onClose: () => void }) {
  const ctxs = [...new Set(km.bindings.map((b) => b.Context))]
  return (
    <Dialog title="Key bindings" onClose={onClose}>
      <p className="dim" style={{ margin: '0 0 10px' }}>Press <kbd>{display(km.prefix)}</kbd> then a key. <kbd>Esc</kbd> cancels, pressing the prefix twice sends it to the page. Greyed bindings only exist in the terminal UI.</p>
      <div className="keys">
        {ctxs.map((c) => (
          <section key={c}>
            <h3>{c}</h3>
            {km.bindings.filter((b) => b.Context === c).map((b) => (
              <div key={b.Action + b.Key} className={`row ${SUPPORTED.has(b.Action) ? '' : 'dim'}`} title={SUPPORTED.has(b.Action) ? undefined : 'Not in the web UI'}>
                <kbd>{b.NoPrefix ? '' : display(km.prefix) + ' '}{display(b.Key)}</kbd><span>{b.Desc}</span>
              </div>
            ))}
          </section>
        ))}
      </div>
      <div className="actions"><button onClick={onClose}>Close</button></div>
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
    <div className="drawer">
      <div className="toolbar"><strong className="grow">{kind === 'history' ? 'History' : 'Saved queries'}</strong>
        {kind === 'history' && <button className="ghost" onClick={async () => { if (!window.confirm('Clear history for this connection?')) return; try { await api('DELETE', '/history' + qs({ conn })) } catch (e) { notify(`Could not clear history: ${e instanceof Error ? e.message : e}`) } finally { hist.refetch() } }}>Clear</button>}
        <button className="ghost" onClick={onClose} aria-label="Close">✕</button></div>
      {kind === 'history' && <input placeholder="Search history" value={q} onChange={(e) => setQ(e.target.value)} />}
      <div className="scroll">
        {(kind === 'history' ? hist : saved).error && <div className="err note">{(kind === 'history' ? hist : saved).error!.message} <button className="ghost" onClick={() => (kind === 'history' ? hist : saved).refetch()}>Retry</button></div>}
        {kind === 'history' && (hist.data ?? []).map((h) => (
          <div key={h.id} className="hist-item" onClick={() => onUse(h.sql)}><pre>{h.sql}</pre><div className="dim">{new Date(h.time).toLocaleString()}</div></div>
        ))}
        {kind === 'queries' && (saved.data ?? []).map((s) => (
          <div key={s.Path} className="hist-item" onClick={() => onUse(s.SQL)}><strong>{s.Folder ? s.Folder + ' / ' : ''}{s.Name}</strong><pre>{s.SQL}</pre></div>
        ))}
      </div>
    </div>
  )
}
