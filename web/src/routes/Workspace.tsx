import { Fragment, useCallback, useEffect, useRef, useState, type CSSProperties } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, ApiError, cancelInflight, clip, download, inflightCount, postQuery, qs, recentKey, sameRef, saveBlob, withToken, type Connection, type FilterSet, type Meta, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { DataTab } from '../components/DataTab'
import { Dialog, PlanDialog, ask, confirm, openSession } from '../components/Dialogs'
import { DataDiffDialog, FakeDataDialog, SchemaDiffDialog } from '../components/Wave'
import { ConsolePane, DetailsDialog, Palette, RecentDialog, SafeModeDialog, TabMenu, type PaletteItem } from '../components/Extras'
import { SettingsDialog } from '../components/Settings'
import { JobsDrawer, useJobs } from '../components/Jobs'
import { LOCK_EVENT } from '../components/Lock'
import { ImportDialog } from '../components/Tools'
import { filterMenu, whyNot } from '../utils/menus'
import { openRecentFile, openSqlFile, revealPath } from '../utils/files'
import { QueryTab } from '../components/QueryTab'
import { Sidebar } from '../components/Sidebar'
import { BackupDialog, McpDialog, PluginsDialog, UsersDialog, type PluginCtx } from '../components/Admin'
import { ProcessDialog, SearchDialog } from '../components/Tools'
import { AssistantTab, DiagramTab, MetricsTab } from '../components/Views'
import { Logo } from '../components/logos'
import { Select } from '../components/Select'
import { copyText } from '../utils/copy'
import { queryLink, tableLink } from '../utils/links'
import { Machine, canon, contextOf, display, inTextField, keysLocked, moveFocus } from '../utils/keys'
import { dropWorkspace, forget, loadTabs, markReopened, noteWorkspace, openSessions, remember, saveTabs } from '../utils/workspaces'

// a deep link (M15.19): /s/<sid>/t/<schema>/<table>?filter=… opens that table, /s/<sid>/q/<saved query> opens that query
export type Target = { kind: 't'; schema: string; table: string; filter?: FilterSet } | { kind: 'q'; path: string }

// Actions the web UI answers (its own plus the ones tabs and the sidebar handle via the webtuidb:action event).
// Everything else in the keymap is listed as "not in the web UI" so the help screen stays identical to the terminal's.
export const SUPPORTED = new Set(['help', 'new_query_tab', 'new_tab', 'close_tab', 'next_tab', 'prev_tab', 'toggle_sidebar', 'disconnect', 'reload', 'open_connection', 'new_session', 'switch_session', 'quit',
  'reload_tab', 'filters', 'columns', 'add_row', 'duplicate_rows', 'delete_rows', 'commit', 'preview', 'discard', 'undo', 'next_page', 'prev_page', 'first_row', 'last_row', 'count_exact',
  'show_data', 'show_structure', 'show_json', 'toggle_row_detail', 'quick_look', 'run_current', 'run_all', 'cancel_query', 'add_to_queries', 'set_limit', 'sidebar_search', 'apply_filters',
  'export', 'import', 'search_in_database', 'process_list', 'beautify', 'uglify', 'find', 'find_replace', 'variables', 'copy_rows', 'copy_rows_as', 'copy_cells', 'chart', 'load_more',
  'users', 'diagram', 'metrics', 'backup', 'restore', 'assistant', 'plugins', 'go_back', 'go_forward', 'split_right', 'next_pane', 'prev_pane', 'workspaces',
  'fold', 'select_next_occurrence', 'select_all_occurrences', 'vim_mode', 'invisibles',
  'redo', 'reconnect', 'safe_mode', 'toggle_console', 'toggle_results', 'set_value', 'paste_rows', 'paste_cells', 'quick_filter', 'follow_fk', 'page_settings', 'show_index', 'show_ddl', 'show_tree', 'triggers',
  'truncate', 'delete_item', 'open_anything', 'open_database', 'next_workspace', 'prev_workspace', 'toggle_comment', 'block_comment', 'save_sql_as', 'focus_left', 'focus_right', 'focus_up', 'focus_down',
  'close_other_tabs', 'close_tabs_right', 'close_all_tabs', 'rename_tab', 'cancel_queries', 'connection_details', 'wrap_lines', 'cleanup_variables', 'upper_case', 'lower_case', 'capitalize', 'use_selection_find',
  'split_down', 'close_other_connections', 'open_file', 'open_recent', 'settings', 'find_next', 'find_prev', 'block_select', 'filter_sql', 'filter_clear_all', 'filter_export', 'filter_add_copy', 'mark', 'new_schema',
  'jump_to_selection', 'complete', 'hide_column', 'show_all_columns', 'sort_columns', 'bulk_update', 'explain',
  'close_pane', 'toggle_panels', 'move_tab_left', 'move_tab_right', 'jobs', 'lock', 'assistant_new', 'assistant_clear', 'assistant_history', 'copy_cells_as', 'copy_column', 'column_menu', 'select_word', 'select_all',
  'console_find', 'console_next', 'console_prev', 'console_copy', 'console_errors', 'console_menu',
  ...Array.from({ length: 9 }, (_, i) => `tab_${i + 1}`)])
// plugin shortcuts are bindings named plugin:<plugin name> (config.KeymapWith); the web runs them through POST /plugins/run
const supported = (a: string) => SUPPORTED.has(a) || a.startsWith('plugin:')

type Body = { kind: 'table'; obj: ObjRef; filters?: FilterSet; view?: 'structure' } | { kind: 'query'; n: number; text: string; autorun?: boolean } | { kind: 'diagram'; focus?: ObjRef } | { kind: 'metrics' | 'assistant' }
type Tab = Body & { id: number; pane: number; name?: string } // pane 0 = left group, 1 = right group; name is a rename
const PAGES = { diagram: 'Diagram', metrics: 'Metrics', assistant: 'Assistant' }
const tabLabel = (t: Tab) => (t.name ? t.name : t.kind === 'table' ? t.obj.Name : t.kind === 'query' ? <>Query <span className="font-mono text-[11px] text-ink-3">{t.n}</span></> : PAGES[t.kind])

export function Workspace({ sid, target }: { sid: string; target?: Target }) {
  const qc = useQueryClient()
  const nav = useNavigate()
  const info = useQuery({ queryKey: ['session', sid], queryFn: () => api<SessionInfo>('GET', `/sessions/${sid}`), retry: false, gcTime: 0 })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  const dbs = useQuery({ queryKey: ['dbs', sid, info.data?.database], queryFn: () => api<{ databases: string[] }>('GET', `/sessions/${sid}/databases`), enabled: !!info.data, gcTime: 0 })

  const [tabs, setTabs] = useState<Tab[]>([])
  const [actives, setActives] = useState<[number, number]>([0, 0]) // the shown tab of each pane
  const [split, setSplit] = useState(false)
  const [focus, setFocus] = useState(0)
  const trail = useRef<{ list: ObjRef[]; i: number }>({ list: [], i: -1 }) // go back / forward over opened tables
  const [plan, setPlan] = useState<Plan | null>(null)
  const [drawer, setDrawer] = useState<'' | 'history' | 'queries' | 'jobs'>('')
  const [zen, setZen] = useState(false) // M15.24: sidebar, console, row detail and status bar hidden
  const jobs = useJobs(sid, drawer === 'jobs')
  const [dragTab, setDragTab] = useState<number | null>(null)
  const [dbMenuAt, setDbMenuAt] = useState<{ x: number; y: number; db: string } | null>(null)
  const [dropImport, setDropImport] = useState<{ obj: ObjRef; file: File } | null>(null)
  const [side, setSide] = useState(false)
  const [collapsed, setCollapsed] = useState(false)
  const [help, setHelp] = useState(false)
  const [switcher, setSwitcher] = useState(false)
  const [dbMenu, setDbMenu] = useState(false)
  const [consoleOn, setConsoleOn] = useState(false)
  const [safeDlg, setSafeDlg] = useState(false)
  const [palette, setPalette] = useState(false)
  const [infoDlg, setInfoDlg] = useState(false)
  const [settingsDlg, setSettingsDlg] = useState(false)
  const [recentDlg, setRecentDlg] = useState(false)
  const [tmenu, setTmenu] = useState<{ x: number; y: number; id: number } | null>(null)
  const [tool, setTool] = useState<'' | 'search' | 'procs' | 'users' | 'backup' | 'restore' | 'plugins' | 'mcp' | 'schemadiff' | 'datadiff'>('')
  const [fake, setFake] = useState<ObjRef | null>(null)
  const [pluginCtx, setPluginCtx] = useState<PluginCtx>({ context: '', selection: '', cell: '' })
  const setAskDb = (from: string | null) => { try { from ? sessionStorage.setItem(ASK_DB, from) : sessionStorage.removeItem(ASK_DB) } catch { /* private mode */ } }
  const nDbs = dbs.data?.databases.length
  const conflictsShown = useRef(false)
  useEffect(() => { // plugins whose shortcuts clash with a built-in binding: say so once
    const c = meta.data?.pluginConflicts
    if (c?.length && !conflictsShown.current) { conflictsShown.current = true; notify(`Plugin shortcut conflicts: ${c.join('; ')}`) }
  }, [meta.data]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { // new_session: once the workspace we switched to has its database list, offer it
    let from: string | null = null
    try { from = sessionStorage.getItem(ASK_DB) } catch { /* private mode */ }
    if (!from || from === sid || nDbs === undefined) return
    setAskDb(null)
    if (nDbs > 1) setDbMenu(true)
  }, [sid, nDbs])
  const conns = useQuery({ queryKey: ['conns'], queryFn: () => api<Connection[]>('GET', '/connections'), enabled: switcher })
  // workspaces: every open session with the database it is on
  const opens = useQuery({ queryKey: ['opens', switcher], enabled: switcher, gcTime: 0, queryFn: async () => Object.fromEntries(await Promise.all(Object.entries(openSessions()).map(async ([n, id]) => [n, (await api<SessionInfo>('GET', `/sessions/${id}`).catch(() => null))?.database ?? '']))) as Record<string, string> })
  // MCP approval sheet: a client sent a non-read-only statement and waits for an answer
  const approvals = useQuery({ queryKey: ['mcpapprovals'], queryFn: () => api<{ id: string; token: string; connection: string; sql: string }[]>('GET', '/mcp/approvals'), refetchInterval: 2000, gcTime: 0 })
  const approval = approvals.data?.[0]
  const answerApproval = (approve: boolean) => { if (approval) api('POST', `/mcp/approvals/${approval.id}`, { approve }).catch((e) => notify(String(e.message ?? e))).finally(() => approvals.refetch()) }
  const [pending, setPending] = useState<string | null>(null) // context the prefix was pressed in, for which-key
  const machine = useRef<Machine | null>(null)
  const act = useRef<(a: string) => void>(() => {})
  const [toast, setToast] = useState('')
  const [details, setDetails] = useState('') // the full text behind a clipped toast
  const seq = useRef(1)
  const queries = useRef(0)
  // auto_save_tabs: the open tabs are saved (debounced) and restored once per workspace; texts holds each editor's current content
  const texts = useRef<Record<number, string>>({})
  const saveReady = useRef(false), saveTimer = useRef<ReturnType<typeof setTimeout>>(undefined), saveNow = useRef<() => void>(() => {})
  const autoSave: string = meta.data?.config.editor.AutoSaveTabs || 'none'
  const schedule = () => { if (!saveReady.current || autoSave === 'none') return; clearTimeout(saveTimer.current); saveTimer.current = setTimeout(() => saveNow.current(), 800) }
  // M12.5: a reload comes back to the tabs this browser tab had open (per session, editor text included); only a fresh session reads auto_save_tabs
  const restored = useRef(false)
  useEffect(() => {
    if (!info.data || !meta.data || restored.current) return
    restored.current = true
    const sv = loadTabs(sid)
    if (!sv?.tabs.length) return
    const ts = sv.tabs.map((t) => ({ ...t, id: seq.current++ }) as unknown as Tab)
    for (const t of ts) if (t.kind === 'query') { texts.current[t.id] = t.text; queries.current = Math.max(queries.current, t.n) }
    setTabs(ts); setSplit(sv.split && ts.some((t) => t.pane === 1)); setActives([ts[sv.active[0]]?.id ?? 0, ts[sv.active[1]]?.id ?? 0])
    saveReady.current = true; openedTarget.current = true
  }, [info.data, meta.data])
  useEffect(() => { // keep the browser tab's copy current (debounced; editor text is read from texts)
    if (!restored.current) return
    const t = setTimeout(() => persistTabs.current(), 300)
    return () => clearTimeout(t)
  }, [tabs, actives, split])
  const persistTabs = useRef<() => void>(() => {})
  const openRef = useRef<(o: ObjRef, record?: boolean, view?: 'structure', newTab?: boolean) => void>(() => {})
  useEffect(() => {
    const h = (e: Event) => { const d = (e as CustomEvent).detail; if (d?.obj) { e.preventDefault(); openRef.current(d.obj, true, d.view, !!d.newTab) } }
    window.addEventListener('webtuidb:open', h)
    return () => window.removeEventListener('webtuidb:open', h)
  }, [])
  const pluginRun = useRef<(name: string) => void>(() => {})
  useEffect(() => { // webtuidb:plugin {name}: another component asks for a plugin to run
    const h = (e: Event) => { const n = (e as CustomEvent).detail?.name; if (n) pluginRun.current(n) }
    window.addEventListener('webtuidb:plugin', h)
    return () => window.removeEventListener('webtuidb:plugin', h)
  }, [])
  const deepRun = useRef<() => void>(() => {})
  const deepDone = useRef(false)
  useEffect(() => { // the route named a table or a saved query: open it once the workspace is up
    if (!target || deepDone.current || !info.data || !meta.data) return
    deepDone.current = true; deepRun.current()
  }, [info.data, meta.data]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => {
    if (!info.data || !meta.data || saveReady.current) return
    if (restored.current && loadTabs(sid)?.tabs.length) return
    if (autoSave === 'none') { saveReady.current = true; return }
    api<{ kind: string; name?: string; text?: string }[]>('GET', '/state/tabs' + qs({ conn: info.data.name })).then((list) => {
      for (const t of autoSave === 'first' ? list.filter((x) => x.kind === 'sql').slice(0, 1) : list) {
        if (t.kind === 'sql') newQuery(t.text ?? '')
        else if (t.kind === 'table' && t.name) { const dot = t.name.indexOf('.'); openObj({ Schema: t.name.slice(0, dot), Name: t.name.slice(dot + 1), Kind: 'table' }, false) }
      }
      saveReady.current = true // only after a good read: saving over tabs that failed to load would store []
    }).catch(() => {})
  }, [info.data, meta.data])
  useEffect(() => { schedule() }, [tabs])
  // a connection URL that named a table (schema, table, column, operation, value, condition) opens it right after the connect
  const openedTarget = useRef(false)
  const dirty = useRef<Record<number, boolean>>({}) // table tabs with pending edits, reported by DataTab
  const okToDrop = async (ts: Tab[]) => !ts.some((t) => dirty.current[t.id]) || confirm('Discard pending changes in the tab(s) being closed?')
  useEffect(() => {
    const warn = (e: BeforeUnloadEvent) => { if (Object.values(dirty.current).some(Boolean)) e.preventDefault() }
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [])
  useEffect(() => {
    const o = info.data?.open
    if (!info.data || !meta.data || openedTarget.current || !o?.Table) return
    openedTarget.current = true
    const ref: ObjRef = { Schema: o.Schema || (info.data.caps.Schemas ? info.data.schema : ''), Name: o.Table, Kind: 'table' }
    if (!o.Column) return void openObj(ref)
    const want = (OPEN_OPS[(o.Operation || '=').toLowerCase()] ?? o.Operation) || '='
    const op = (meta.data.operators[info.data.caps.Dialect] ?? []).find((x) => x.toLowerCase() === want.toLowerCase()) ?? want
    openHit(ref, { Filters: [{ Column: o.Column, Op: op, Value: o.Value, Enabled: true }], Or: (o.Condition || '').toLowerCase() === 'or' })
  }, [info.data, meta.data])

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
  saveNow.current = () => {
    const list: { kind: string; name?: string; text?: string }[] = tabs.flatMap((t): { kind: string; name?: string; text?: string }[] => (t.kind === 'query' ? [{ kind: 'sql', text: texts.current[t.id] ?? t.text }] : t.kind === 'table' ? [{ kind: 'table', name: recentKey(t.obj) }] : []))
    api('PUT', '/state/tabs' + qs({ conn: i.name }), autoSave === 'first' ? list.filter((x) => x.kind === 'sql').slice(0, 1) : list).catch(() => {})
  }
  remember(i.name, sid)
  const accent = i.color ? ({ '--accent': i.color, '--on-accent': onAccent(i.color) } as CSSProperties) : undefined

  const p = split ? focus : 0 // the focused pane; new tabs open there
  const active = actives[p]
  const setActive = (pn: number, id: number) => setActives((a) => (pn ? [a[0], id] : [id, a[1]]))
  const addTab = (b: Body) => {
    const id = seq.current++
    setSide(false)
    setTabs((t) => [...t, { ...b, id, pane: p } as Tab]); setActive(p, id)
    return id
  }
  const kick = (action: string) => setTimeout(() => window.dispatchEvent(new CustomEvent('webtuidb:action', { detail: { action }, cancelable: true })), 50)
  const openObj = (o: ObjRef, record = true, view?: 'structure') => {
    if (!['table', 'view', 'matview'].includes(o.Kind)) return
    if (record) { // cap 50, a new open drops the forward trail
      const t = trail.current
      if (t.list[t.i] === undefined || !sameRef(t.list[t.i], o)) { t.list = [...t.list.slice(0, t.i + 1), o].slice(-50); t.i = t.list.length - 1 }
    }
    const ex = tabs.find((t) => t.kind === 'table' && t.pane === p && sameRef(t.obj, o))
    if (ex) { setActive(p, ex.id); if (view) kick('show_structure'); return }
    addTab({ kind: 'table', obj: o, view })
    api('POST', '/state/touch', { Conn: i.name, Item: recentKey(o) }).then(() => qc.invalidateQueries({ queryKey: ['shortcuts', sid] })).catch(() => {})
  }
  const walk = (d: number) => {
    const t = trail.current, j = t.i + d
    if (j < 0 || j >= t.list.length) return notify(d < 0 ? 'Nothing to go back to' : 'Nothing to go forward to')
    t.i = j; openObj(t.list[j], false)
  }
  // a search hit opens its table in a fresh tab with the hit's filters, so a tab with edits is never replaced
  const openHit = (o: ObjRef, filters: FilterSet) => { addTab({ kind: 'table', obj: o, filters }) }
  const newQuery = (text = '', autorun = false) => { texts.current[addTab({ kind: 'query', n: ++queries.current, text, autorun })] = text }
  const openPage = (kind: 'diagram' | 'metrics' | 'assistant', focusOn?: ObjRef) => {
    const ex = tabs.find((t) => t.kind === kind)
    if (ex) { setFocus(ex.pane); setActive(ex.pane, ex.id); if (kind === 'diagram') setTabs((l) => l.map((x) => (x.id === ex.id ? { ...x, focus: focusOn } as Tab : x))) } else addTab(kind === 'diagram' ? { kind, focus: focusOn } : { kind })
  }
  const withFile = (f: { name: string; text: string } | null) => { if (f) newQuery(f.text) }
  // Close other connections: every other open workspace of this browser tab is disconnected; this one stays
  const closeOtherConns = async () => {
    const others = Object.entries(openSessions()).filter(([, id]) => id !== sid)
    if (!others.length) return notify('No other connection is open')
    if (!(await confirm(`Close ${others.length} other connection${others.length === 1 ? '' : 's'}?`))) return
    await Promise.all(others.map(([, id]) => api('DELETE', `/sessions/${id}`).catch(() => {})))
    others.forEach(([, id]) => forget(id))
    notify(`Closed ${others.length} other connection${others.length === 1 ? '' : 's'}`)
  }
  const close = async (id: number) => {
    const t = tabs.find((x) => x.id === id); if (!t) return
    if (!(await okToDrop([t]))) return
    const rest = tabs.filter((x) => x.id !== id)
    setTabs(rest)
    if (actives[t.pane] === id) setActive(t.pane, rest.filter((x) => x.pane === t.pane).at(-1)?.id ?? 0)
  }
  const closeWhere = async (keep: (t: Tab) => boolean) => {
    if (!(await okToDrop(tabs.filter((t) => !keep(t))))) return
    const rest = tabs.filter(keep)
    setTabs(rest)
    setActives(([a, b]) => [rest.some((x) => x.id === a) ? a : rest.filter((x) => x.pane === 0).at(-1)?.id ?? 0, rest.some((x) => x.id === b) ? b : rest.filter((x) => x.pane === 1).at(-1)?.id ?? 0])
    if (!rest.some((x) => x.pane === 1)) { setSplit(false); setFocus(0) }
  }
  const closeOthers = (id: number) => { const t = tabs.find((x) => x.id === id); if (t) closeWhere((x) => x.pane !== t.pane || x.id === id) }
  const closeRight = (id: number) => { const t = tabs.find((x) => x.id === id); if (!t) return; const at = tabs.filter((x) => x.pane === t.pane).findIndex((x) => x.id === id); const gone = new Set(tabs.filter((x) => x.pane === t.pane).slice(at + 1).map((x) => x.id)); closeWhere((x) => !gone.has(x.id)) }
  const rename = async (id: number) => {
    const t = tabs.find((x) => x.id === id); if (!t) return
    const n = await ask('Rename tab', t.name ?? (t.kind === 'table' ? t.obj.Name : t.kind === 'query' ? `Query ${t.n}` : PAGES[t.kind]))
    if (n !== null) setTabs((l) => l.map((x) => (x.id === id ? { ...x, name: n.trim() || undefined } : x)))
  }
  const reconnect = async () => { // a fresh session for the same connection; the old one is closed once the new one is up
    try {
      const r = await openSession(i.name)
      await api('DELETE', `/sessions/${sid}`).catch(() => {})
      forget(sid); qc.clear(); nav({ to: '/s/$sessionId', params: { sessionId: r.id } })
    } catch (e) { notify(`Could not reconnect: ${e instanceof Error ? e.message : e}`) }
  }
  const cycle = (d: number) => {
    const ids = Object.values(openSessions())
    if (ids.length < 2) return notify('Only one workspace is open')
    nav({ to: '/s/$sessionId', params: { sessionId: ids[(Math.max(0, ids.indexOf(sid)) + d + ids.length) % ids.length] } })
  }
  const splitRight = () => {
    if (split && !tabs.some((t) => t.pane === 1)) { setSplit(false); setFocus(0); return }
    const t = tabs.find((x) => x.id === active)
    if (!t) return notify('Open a tab first')
    if (t.pane === 1) return notify('This tab is already in the right pane')
    setSplit(true); setFocus(1)
    setTabs(tabs.map((x) => (x.id === t.id ? { ...x, pane: 1 } : x)))
    setActives([tabs.filter((x) => x.pane === 0 && x.id !== t.id).at(-1)?.id ?? 0, t.id])
  }
  const onPlugin = () => {
    const c = contextOf(), cur = tabs.find((t) => t.id === active)
    setPluginCtx({ context: c === 'editor' ? 'editor' : c === 'sidebar' ? 'sidebar' : c === 'global' ? '' : 'grid', object: cur?.kind === 'table' ? cur.obj : undefined, selection: window.getSelection()?.toString() ?? '' })
    setTool('plugins')
  }
  const insertSql = (text: string) => {
    const ev = new CustomEvent('webtuidb:insert', { detail: { text }, cancelable: true })
    window.dispatchEvent(ev)
    if (!ev.defaultPrevented) newQuery(text)
  }
  const switchDb = async (name: string) => {
    if (tabs.length && !(await okToDrop(tabs) && await confirm('Switching closes all open tabs. Continue?'))) return
    try { await api('POST', `/sessions/${sid}/database`, { Name: name }); setTabs([]); setActives([0, 0]); setSplit(false); setFocus(0); trail.current = { list: [], i: -1 }; qc.invalidateQueries() } catch (e) { notify(String(e)) }
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
    try { const r = await openSession(name); nav({ to: '/s/$sessionId', params: { sessionId: r.id } }) } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  // cancelling on PostgreSQL / MySQL also opens the process list, to see what is still running on the server
  const afterCancel = () => { if ((i.caps.Dialect === 'postgres' || i.caps.Dialect === 'mysql') && i.caps.ProcessList) setTool('procs') }
  const cancelQueries = () => { const n = cancelInflight(); if (n) afterCancel(); return notify(n ? `Cancelled ${n} running quer${n === 1 ? 'y' : 'ies'}` : 'No queries are running') }
  const bindings = meta.data.keymap.bindings
  act.current = (action: string) => {
    const mine = tabs.filter((t) => t.pane === p) // tab keys cycle inside the focused pane
    const idx = mine.findIndex((t) => t.id === active)
    const go = (j: number) => mine.length && setActive(p, mine[(j + mine.length) % mine.length].id)
    const n = /^tab_(\d)$/.exec(action)
    switch (action) {
      case 'help': return setHelp(true)
      case 'reconnect': return reconnect()
      case 'safe_mode': return setSafeDlg(true)
      case 'toggle_console': return setConsoleOn((c) => !c)
      case 'open_anything': return setPalette(true)
      case 'open_database': return (dbs.data?.databases.length ?? 0) > 1 ? setDbMenu(true) : notify('Only one database here')
      case 'next_workspace': return cycle(1)
      case 'prev_workspace': return cycle(-1)
      case 'connection_details': return setInfoDlg(true)
      case 'settings': return setSettingsDlg(true)
      case 'open_file': return void openSqlFile().then(withFile)
      case 'open_recent': return setRecentDlg(true)
      case 'close_other_connections': return closeOtherConns()
      case 'cancel_queries': return cancelQueries()
      case 'close_other_tabs': return active && closeOthers(active)
      case 'close_tabs_right': return active && closeRight(active)
      case 'close_all_tabs': return closeWhere(() => false)
      case 'rename_tab': return active && rename(active)
      case 'focus_left': case 'focus_right': case 'focus_up': case 'focus_down': return void moveFocus(action.slice(6) as 'left')
      case 'new_query_tab': case 'new_tab': return newQuery()
      case 'close_tab': return active && close(active)
      case 'next_tab': return go(idx + 1)
      case 'prev_tab': return go(idx - 1)
      case 'toggle_sidebar': setSide((s) => !s); return setCollapsed((c) => !c)
      case 'disconnect': case 'quit': return disconnect()
      case 'reload': return qc.invalidateQueries()
      case 'open_connection': case 'switch_session': case 'workspaces': return setSwitcher(true)
      case 'new_session': setAskDb(sid); return setSwitcher(true)
      case 'search_in_database': return setTool('search')
      case 'process_list': return inflightCount() > 0 ? cancelQueries() : setTool('procs') // the same key: cancel while something runs, the process list otherwise
      case 'schema_diff': return setTool('schemadiff')
      case 'data_diff': return setTool('datadiff')
      case 'fake_rows': { const t = tabs.find((x) => x.id === active); return t?.kind === 'table' ? setFake(t.obj) : notify('Open a table first: fake data fills the table of the current tab') }
      case 'mcp_log': return setTool('mcp')
      case 'clear_console': return void api('DELETE', '/console').then(() => { qc.invalidateQueries({ queryKey: ['console'] }); notify('Console cleared') }, (e) => notify(String(e.message ?? e)))
      case 'users': return setTool('users')
      case 'backup': return setTool('backup')
      case 'restore': return setTool('restore')
      case 'plugins': return onPlugin()
      case 'diagram': case 'metrics': case 'assistant': return openPage(action)
      case 'go_back': return walk(-1)
      case 'go_forward': return walk(1)
      case 'split_right': return splitRight()
    }
    if (action === 'next_pane' || action === 'prev_pane') { // a query tab with stacked editors answers first; otherwise the pane split does
      const ev = new CustomEvent('webtuidb:action', { detail: { action }, cancelable: true })
      window.dispatchEvent(ev)
      if (ev.defaultPrevented) return
      return split ? setFocus(1 - focus) : notify('No split: use Split pane right or Split pane down first')
    }
    if (n) return mine[Number(n[1]) - 1] && setActive(p, mine[Number(n[1]) - 1].id)
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
                // [general] show_connection_name / show_database_name pick the parts of an open workspace's label; with neither set both show
                const g = meta.data.config.general, showConn = !!g.ShowConnectionName || !g.ShowDatabaseName, showDb = !!g.ShowDatabaseName || !g.ShowConnectionName
                const dbName = c.Name === i.name ? i.database : opens.data?.[c.Name] || 'open'
                return (
                  <button key={c.Name} role="menuitem" className={`${SW_ITEM} ${c.Name === i.name ? 'btn-on' : ''}`} onClick={() => switchTo(c.Name)} style={{ '--c': c.Color || undefined } as CSSProperties}>
                    <span className="w-1 h-[18px] rounded-[1px] bg-[var(--c,var(--rule-2))]" aria-hidden /><Logo driver={c.Driver} size={14} /><span className="flex-1 min-w-0 truncate">{open && !showConn ? dbName : c.Name}</span>
                    {open && showConn && showDb && <span className="text-ink-2">{dbName}</span>}
                  </button>
                )
              })}
              <hr className="border-t border-rule my-1.5" />
              <button role="menuitem" className={SW_ITEM} onClick={() => { setSwitcher(false); closeOtherConns() }}>Close other connections</button>
              <button role="menuitem" className={SW_ITEM} onClick={() => { setSwitcher(false); setTool('mcp') }}>MCP activity</button>
              <button role="menuitem" className={SW_ITEM} onClick={() => nav({ to: '/' })}>All connections</button>
            </div>
          </>
        )}
        <span className="font-mono text-[11.5px] text-ink-2 truncate max-[760px]:hidden">{i.serverInfo}{i.encoding ? ` · ${i.encoding}` : ''}</span>
        <span className="px-2 border border-rule-2 rounded-[2px] text-[11.5px] text-ink-2 whitespace-nowrap" title="Safe mode decides what needs confirmation before it is sent">{i.levelLabel}</span>
        {i.readOnly && <span className="px-2 border border-del rounded-[2px] text-[11.5px] text-del whitespace-nowrap" title="This connection refuses every statement that changes data">read only</span>}
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
        <Sidebar key={i.database} open={side} collapsed={collapsed} sid={sid} info={i} meta={meta.data} onOpen={openObj} onQueryText={newQuery} onPlan={setPlan} notify={notify} onDiagram={(o) => openPage('diagram', o)} />
        <div className={`flex-1 min-w-0 grid bg-canvas grid-rows-[auto_minmax(0,1fr)] ${split ? 'grid-cols-2 max-[760px]:grid-cols-1 max-[760px]:grid-rows-[auto_minmax(0,1fr)_auto_minmax(0,1fr)]' : 'grid-cols-1'}`}>
          {(split ? [0, 1] : [0]).map((pn) => (
            <div key={pn} className={`flex h-8 min-w-0 text-[12px] border-b border-rule overflow-x-auto [scrollbar-width:none] bg-canvas font-mono border-t-2 ${split && pn === focus ? 'border-t-accent' : 'border-t-transparent'} ${pn ? 'col-start-2 row-start-1 border-l border-l-rule max-[760px]:col-start-1 max-[760px]:row-start-3' : 'col-start-1 row-start-1'}`} onMouseDownCapture={() => setFocus(pn)}>
              {tabs.filter((t) => t.pane === pn).map((t) => (
                <div key={t.id} className={`group flex items-center gap-2 pl-3.5 pr-2.5 cursor-pointer whitespace-nowrap border-r border-rule hover:text-ink hover:bg-panel ${t.id === actives[pn] ? 'text-ink bg-panel shadow-[inset_0_-2px_0_var(--accent)]' : 'text-ink-2'}`} onClick={() => setActive(pn, t.id)} onContextMenu={(e) => { e.preventDefault(); setTmenu({ x: e.clientX, y: e.clientY, id: t.id }) }}>
                  {tabLabel(t)}
                  <button className={`btn-ghost px-1 ${t.id === actives[pn] ? '' : 'opacity-0 group-hover:opacity-100'}`} aria-label="Tab menu" aria-haspopup="menu" onClick={(e) => { e.stopPropagation(); const r = e.currentTarget.getBoundingClientRect(); setTmenu({ x: r.left, y: r.bottom, id: t.id }) }}>▾</button>
                  <button className={`btn-ghost ${t.id === actives[pn] ? '' : 'opacity-0 group-hover:opacity-100'}`} onClick={(e) => { e.stopPropagation(); close(t.id) }}>✕</button>
                </div>
              ))}
            </div>
          ))}
          {!tabs.some((t) => t.pane === 0) && <div className={`${EMPTY} col-start-1 row-start-2`}>Pick a table from the list, or start a new query.</div>}
          {split && !tabs.some((t) => t.pane === 1) && <div className={`${EMPTY} col-start-2 row-start-2 max-[760px]:col-start-1 max-[760px]:row-start-4`} onMouseDownCapture={() => setFocus(1)}>This pane is empty. Open a tab here, or <button className="btn-ghost" onClick={splitRight}>close the split</button>.</div>}
          {tabs.map((t) => {
            const shownHere = t.id === actives[t.pane] && (split || t.pane === 0), live = shownHere && t.pane === p // only the focused pane answers keys
            return (
              <div key={t.id} className={`min-h-0 min-w-0 flex flex-col row-start-2 ${t.pane ? 'col-start-2 border-l border-rule max-[760px]:col-start-1 max-[760px]:row-start-4' : 'col-start-1'}`} hidden={!shownHere} onMouseDownCapture={() => setFocus(t.pane)}>
                {t.kind === 'table' ? <DataTab sid={sid} info={i} meta={meta.data!} obj={t.obj} initFilters={t.filters} initView={t.view} notify={notify} active={live} onFollow={openHit} onDirty={(d) => { dirty.current[t.id] = d }} />
                  : t.kind === 'query' ? <QueryTab sid={sid} info={i} meta={meta.data!} tabKey={`${i.name}/tab${t.n}`} initial={t.text} autorun={t.autorun} notify={notify} shown={live} onCancel={afterCancel} onText={(x) => { texts.current[t.id] = x; schedule() }} />
                  : t.kind === 'diagram' ? <DiagramTab sid={sid} info={i} focus={t.focus} onStructure={(r) => openObj(r, true, 'structure')} onPlan={setPlan} />
                  : t.kind === 'metrics' ? <MetricsTab sid={sid} info={i} shown={shownHere} others={openSessions()} notify={notify} />
                  : <AssistantTab sid={sid} notify={notify} onRun={(q) => newQuery(q, true)} />}
              </div>
            )
          })}
        </div>
        {drawer && <Drawer kind={drawer} conn={i.name} dir={meta.data.queriesDir ?? ''} notify={notify} onUse={(t) => newQuery(t)} onClose={() => setDrawer('')} />}
      </div>
      {consoleOn && <ConsolePane conn={i.name} onClose={() => setConsoleOn(false)} />}
      {tmenu && <TabMenu x={tmenu.x} y={tmenu.y} onClose={() => setTmenu(null)} items={[['Close', () => close(tmenu.id)], ['Close other tabs', () => closeOthers(tmenu.id)], ['Close tabs to the right', () => closeRight(tmenu.id)], ['Close all tabs', () => closeWhere(() => false)], ['Rename tab…', () => rename(tmenu.id)]]} />}
      {safeDlg && <SafeModeDialog sid={sid} info={i} onClose={() => setSafeDlg(false)} onDone={() => { setSafeDlg(false); qc.invalidateQueries({ queryKey: ['session', sid] }) }} />}
      {infoDlg && <DetailsDialog sid={sid} info={i} onClose={() => setInfoDlg(false)} onSaved={() => { setInfoDlg(false); qc.invalidateQueries({ queryKey: ['session', sid] }) }} />}
      {settingsDlg && <SettingsDialog notify={notify} onClose={() => setSettingsDlg(false)} />}
      {recentDlg && <RecentDialog onClose={() => setRecentDlg(false)} onPick={(e) => openRecentFile(e).then(withFile)} />}
      {palette && <PaletteHost sid={sid} info={i} dbs={dbs.data?.databases ?? []} bindings={bindings} onTable={openObj} onSql={newQuery} onDb={switchDb} onAction={(a) => act.current(a)} onClose={() => setPalette(false)} />}
      {plan && <PlanDialog sid={sid} plan={plan} onClose={() => setPlan(null)} onApplied={() => { setPlan(null); qc.invalidateQueries(); notify('Changes sent') }} />}
      {pending && machine.current && (
        <div className="popover fixed right-3 bottom-3 z-[45] w-[min(760px,calc(100vw-24px))] grid grid-cols-[repeat(auto-fill,minmax(220px,1fr))] gap-x-[18px] gap-y-[3px] px-3.5 py-2.5 max-h-[60vh] overflow-auto text-[12px] font-mono" role="status" aria-label="Keys after the prefix">
          <div className={`${WK_ROW} col-span-full mb-1 text-ink-2`}>{display(km!.prefix)} then</div>
          {machine.current.bindings(pending).map((b) => (
            <div key={b.Context + b.Key} className={`${WK_ROW} ${SUPPORTED.has(b.Action) ? '' : 'text-ink-2'}`}><kbd>{display(b.Key)}</kbd>{b.Desc}</div>
          ))}
        </div>
      )}
      {tool === 'search' && <SearchDialog sid={sid} info={i} onClose={() => setTool('')} onOpen={openHit} />}
      {tool === 'users' && <UsersDialog sid={sid} info={i} onPlan={setPlan} onClose={() => setTool('')} />}
      {(tool === 'backup' || tool === 'restore') && <BackupDialog sid={sid} info={i} restore={tool === 'restore'} notify={notify} onClose={() => setTool('')} />}
      {tool === 'plugins' && <PluginsDialog sid={sid} ctx={pluginCtx} notify={notify} onSql={insertSql} onClose={() => setTool('')} />}
      {approval && <ApprovalDialog key={approval.id} approval={approval} answer={answerApproval} />}
      {tool === 'mcp' && <McpDialog onClose={() => setTool('')} />}
      {tool === 'schemadiff' && <SchemaDiffDialog sid={sid} info={i} onPlan={setPlan} onClose={() => setTool('')} />}
      {tool === 'datadiff' && <DataDiffDialog sid={sid} info={i} onPlan={setPlan} onClose={() => setTool('')} />}
      {fake && <FakeDataDialog sid={sid} obj={fake} onPlan={setPlan} notify={notify} onClose={() => setFake(null)} />}
      {tool === 'procs' && <ProcessDialog sid={sid} info={i} notify={notify} onClose={() => setTool('')} />}
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

// ApprovalDialog: Deny has focus, and clicks and keys are ignored for 700 ms so a keystroke meant for the editor cannot approve a write.
function ApprovalDialog({ approval, answer }: { approval: { token: string; connection: string; sql: string }; answer: (ok: boolean) => void }) {
  const [armed, setArmed] = useState(false)
  useEffect(() => { const t = setTimeout(() => setArmed(true), 700); return () => clearTimeout(t) }, [])
  const go = (ok: boolean) => { if (armed || !ok) answer(ok) }
  return (
    <Dialog title="MCP client wants to run a statement" onClose={() => answer(false)}>
      <div className="text-ink-2 mb-2">Token <b>{approval.token}</b> on connection <b>{approval.connection}</b></div>
      <pre>{approval.sql}</pre>
      <div className={ACTIONS}><button autoFocus onClick={() => go(false)}>Deny</button><button className="btn-primary" onClick={() => go(true)}>Approve</button></div>
    </Dialog>
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

// Open Anything: tables and other objects, saved queries, databases and the actions the web UI answers (Cancel running queries among them).
// Open Anything entries that have no key binding; the labels are the terminal's.
const FEATURES: [string, string][] = [['Pin connection to this tab', 'pin_connection'], ['Schema diff', 'schema_diff'], ['Data diff', 'data_diff'], ['Fake data', 'fake_rows'], ['MCP activity', 'mcp_log'], ['Clear console', 'clear_console'], ['Item overview', 'show_structure']]
// URL operation= spellings and the filter operator they mean
const OPEN_OPS: Record<string, string> = { eq: '=', equal: '=', equals: '=', ne: '<>', neq: '<>', '!=': '<>', gt: '>', lt: '<', gte: '>=', lte: '<=', contain: 'Contains', contains: 'Contains', like: 'LIKE', in: 'IN', isnull: 'IS NULL', isnotnull: 'IS NOT NULL', startswith: 'Has prefix', endswith: 'Has suffix' }

function PaletteHost({ sid, info, dbs, bindings, onTable, onSql, onDb, onAction, onClose }: { sid: string; info: SessionInfo; dbs: string[]; bindings: Meta['keymap']['bindings']; onTable: (o: ObjRef) => void; onSql: (t: string) => void; onDb: (n: string) => void; onAction: (a: string) => void; onClose: () => void }) {
  const objs = useQuery({ queryKey: ['palette-objs', sid, info.database, info.schema], queryFn: () => api<(ObjRef & { Detail: string })[]>('GET', `/sessions/${sid}/objects` + qs({ schema: info.schema })), gcTime: 0 })
  const saved = useQuery({ queryKey: ['palette-queries'], queryFn: () => api<{ Name: string; Folder: string; SQL: string }[]>('GET', '/queries'), gcTime: 0 })
  const seen = new Set<string>()
  const items: PaletteItem[] = [
    ...(objs.data ?? []).map((o) => ({ group: o.Kind, label: o.Name, detail: o.Schema, run: () => onTable(o) })),
    ...(saved.data ?? []).map((q) => ({ group: 'query', label: q.Name, detail: q.Folder, run: () => onSql(q.SQL) })),
    ...dbs.filter((d) => d !== info.database).map((d) => ({ group: info.caps.DatabaseNoun || 'database', label: d, run: () => onDb(d) })),
    ...FEATURES.map(([label, action]) => ({ group: 'feature', label, detail: action, run: () => onAction(action) })),
    ...bindings.filter((b) => SUPPORTED.has(b.Action) && !seen.has(b.Action) && seen.add(b.Action)).map((b) => ({ group: 'action', label: b.Desc, detail: b.Action, run: () => onAction(b.Action) })),
  ]
  return <Palette items={items} onClose={onClose} />
}

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

function Drawer({ kind, conn, dir, notify, onUse, onClose }: { kind: 'history' | 'queries'; conn: string; dir: string; notify: (m: string) => void; onUse: (t: string) => void; onClose: () => void }) {
  const [q, setQ] = useState('')
  const [sort, setSort] = useState<'name' | 'created' | 'modified'>('name')
  const hist = useQuery({ queryKey: ['history', conn, q], queryFn: () => api<{ id: string; SQL?: string; sql: string; time: string }[]>('GET', '/history' + qs({ conn, q, limit: 200 })), enabled: kind === 'history', gcTime: 0 })
  const saved = useQuery({ queryKey: ['queries'], queryFn: () => api<{ Path: string; Name: string; Folder: string; SQL: string; Shared?: boolean; Modified?: string }[]>('GET', '/queries'), enabled: kind === 'queries', gcTime: 0 })
  // by name (the server's order) or newest first inside each folder; Created is not tracked, so it uses the modified time too
  const list = [...(saved.data ?? [])].sort((a, b) => a.Folder.localeCompare(b.Folder) || (sort === 'name' ? a.Name.localeCompare(b.Name, undefined, { sensitivity: 'base' }) : (b.Modified ?? '').localeCompare(a.Modified ?? '')))
  const fail = (m: string) => (e: unknown) => notify(`${m}: ${e instanceof Error ? e.message : e}`)
  const readFile = (e: React.ChangeEvent<HTMLInputElement>, then: (name: string, text: string) => Promise<void> | void) => {
    const f = e.target.files?.[0]; e.target.value = ''
    if (f) f.text().then((t) => then(f.name, t))
  }
  return (
    <div className="w-[340px] flex-none border-l border-rule flex flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-0 max-[760px]:w-full">
      <div className="toolbar"><strong className="flex-1 min-w-0 font-medium">{kind === 'history' ? 'History' : 'Saved queries'}</strong>
        {kind === 'history' && <>
          <button className="btn-ghost" onClick={async () => { try { const f = await download('/history/export' + qs({ conn, format: 'csv' })); saveBlob(f.blob, `${conn}-history.csv`) } catch (e) { fail('Could not save history')(e) } }}>Save As…</button>
          <label className="btn-ghost cursor-pointer border border-transparent px-1.5 hover:bg-raised">Import…<input type="file" accept=".csv,.json" hidden onChange={(e) => readFile(e, async (name, text) => { try { await api('POST', '/history/import', { Conn: conn, Format: name.endsWith('.csv') ? 'csv' : 'json', Content: text }); hist.refetch(); notify('Imported history') } catch (er) { fail('Could not import')(er) } })} /></label>
          <button className="btn-ghost" onClick={async () => { if (!(await confirm('Clear history for this connection?'))) return; try { await api('DELETE', '/history' + qs({ conn, all: 1 })) } catch (e) { notify(`Could not clear history: ${e instanceof Error ? e.message : e}`) } finally { hist.refetch() } }}>Clear</button>
        </>}
        {kind === 'queries' && <>
          <label className="btn-ghost cursor-pointer border border-transparent px-1.5 hover:bg-raised" title="Save a .sql file as a saved query">Import…<input type="file" accept=".sql,text/plain" hidden onChange={(e) => readFile(e, async (name, text) => { try { await postQuery('/queries/import-file', { Name: name, SQL: text }); saved.refetch(); notify('Imported ' + name) } catch (er) { fail('Could not import')(er) } })} /></label>
          <label className="btn-ghost cursor-pointer border border-transparent px-1.5 hover:bg-raised" title="Open a .sql file in a new tab">Open file…<input type="file" accept=".sql,text/plain" hidden onChange={(e) => readFile(e, (_, text) => onUse(text))} /></label>
        </>}
        <button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      {kind === 'history' && <input className="mx-2.5 mt-2 mb-1 bg-panel border-transparent" placeholder="Search history" value={q} onChange={(e) => setQ(e.target.value)} />}
      {kind === 'queries' && <div className="px-2.5 pt-2 pb-1"><Select aria-label="Sort queries" className="w-full bg-panel border-transparent" value={sort} onChange={(v) => setSort(v as typeof sort)} options={[{ value: 'name', label: 'Sort by name' }, { value: 'modified', label: 'Sort by modified' }, { value: 'created', label: 'Sort by created' }]} /></div>}
      <div className="overflow-auto flex-1">
        {(kind === 'history' ? hist : saved).error && <div className="text-del p-2.5 text-[12.5px]">{(kind === 'history' ? hist : saved).error!.message} <button className="btn-ghost" onClick={() => (kind === 'history' ? hist : saved).refetch()}>Retry</button></div>}
        {kind === 'history' && (hist.data ?? []).map((h) => (
          <div key={h.id} className={HIST} onClick={() => onUse(h.sql)}><pre className={HIST_PRE}>{h.sql}</pre><div className="text-ink-2 text-[11.5px]">{new Date(h.time).toLocaleString()}</div></div>
        ))}
        {kind === 'queries' && list.map((s) => (
          <div key={s.Path} className={HIST} onClick={() => onUse(s.SQL)}><strong className="flex items-center gap-1 mb-1 font-medium"><span className="flex-1 min-w-0 truncate">{s.Folder ? s.Folder + ' / ' : ''}{s.Name}{s.Shared && <span className="ml-2 text-ink-3 text-[11px] font-normal">shared</span>}</span>
            {/* a browser cannot open a file manager, so the full path is shown and copied instead */}
            <button className="btn-ghost text-[11px]" title="Copy the full path of this file" onClick={(e) => { e.stopPropagation(); const full = s.Path.startsWith('/') || /^[A-Za-z]:/.test(s.Path) ? s.Path : `${dir.replace(/\/$/, '')}/${s.Path}`; copyText(full).then(() => notify(`Path copied: ${full}. A browser cannot reveal it in the file manager.`), (er) => notify(String(er))) }}>Path</button></strong><pre className={HIST_PRE}>{s.SQL}</pre></div>
        ))}
      </div>
    </div>
  )
}
