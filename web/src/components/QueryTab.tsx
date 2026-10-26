import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import CodeMirror from '@uiw/react-codemirror'
import { MySQL, PostgreSQL, StandardSQL } from '@codemirror/lang-sql'
import { EditorSelection, EditorState, Prec } from '@codemirror/state'
import { LanguageSupport, foldEffect, foldedRanges, unfoldEffect } from '@codemirror/language'
import { EditorView as View, crosshairCursor, highlightWhitespace, keymap, rectangularSelection, type EditorView } from '@codemirror/view'
import { SearchQuery, findNext, findPrevious, getSearchQuery, openSearchPanel, selectNextOccurrence, selectSelectionMatches, setSearchQuery } from '@codemirror/search'
import { autocompletion, startCompletion } from '@codemirror/autocomplete'
import { selectAll, toggleBlockComment, toggleComment } from '@codemirror/commands'
import { Vim, vim } from '@replit/codemirror-vim'
import { api, ApiError, beacon, download, postQuery, qs, saveBlob, stream, type Column, type DbObject, type ExecItem, type Gate, type Meta, type Plan, type SessionInfo, type Sort, type Val } from '../utils/api'
import { contextOf } from '../utils/keys'
import { copyText, formatRows, type CopyFormat } from '../utils/copy'
import { ChartView, CopyAsDialog, ExportDialog, VariablesDialog } from './Tools'
import { Dialog, GateFields, PlanDialog, ask, confirm } from './Dialogs'
import { Select, type SelectHandle } from './Select'
import { Grid, isNumeric, show, type GridRow, type Range } from './Grid'
import { FindBar, newFind, type FindState } from './GridFind'
import { SetValueDialog, SortDialog } from './Extras'
import { cellsAs, columnText, rectOf, rectSize } from '../utils/m15_copy'
import { listOf, menuItems } from '../utils/m15_menus'
import { cfg } from '../utils/m15_cfg'
import { foldedIndexes, loadHashes, saveHashes, stmtHash } from '../utils/m15_folds'
import { ItemMenu } from './m15_Menu'
import { cellMatches, findNext as findCell, replaceIn } from '../utils/gridfind'
import { dupVals, editedMap, emptyPend, pendCount, planBody, setEdit, toggleDelete, type Pend } from '../utils/pending'
import { CellEditor, type CellTarget } from './CellEditor'
import { ExplainDialog } from './Wave'
import { completionKeys, completionSource, type Env } from './Completion'
import { cmTheme } from './cmtheme'

// parameter values are remembered per session, so running a statement again pre-fills the dialog
const savedVars: Record<string, Record<string, string>> = {}

type Pending = { body: Record<string, unknown>; gate: Gate; statements: string[]; label: string; error?: string }

// editor toggles are remembered for the browser, not per tab
// M13.8 vim ex commands: the engine ships :<n> (go to line), :noh and visual mode / counts; :w :wq :q :q! are ours and reach the visible query tab through this event.
// shortcut: the ex commands are global to the vim engine, so they are registered once and the tab that is shown answers.
let vimEx = false
function defineVimEx() {
  if (vimEx) return
  vimEx = true
  const go = (name: string) => window.dispatchEvent(new CustomEvent('webtuidb:vim-ex', { detail: { name } }))
  Vim.defineEx('write', 'w', () => go('w'))
  Vim.defineEx('wq', undefined, () => go('wq'))
  Vim.defineEx('xit', 'x', () => go('wq'))
  Vim.defineEx('quit', 'q', (_cm, p) => go(String(p?.argString ?? '').trim().startsWith('!') ? 'q!' : 'q'))
}

const flag = (k: string) => { try { return localStorage.getItem(k) === '1' } catch { return false } }
const setFlag = (k: string, v: boolean) => { try { localStorage.setItem(k, v ? '1' : '0') } catch { /* private mode */ } }

export function QueryTab({ sid, info, meta, initial, notify, shown, autorun, tabKey, savedName, onText, onCancel, onDirty, onClose }: { sid: string; info: SessionInfo; meta: Meta; initial: string; tabKey: string; notify: (m: string) => void; shown?: boolean; autorun?: boolean; savedName?: string; onText?: (t: string) => void; onCancel?: () => void; onDirty?: (dirty: boolean) => void; onClose?: () => void }) {
  const [vimOn, setVimOn] = useState(() => flag('webtuidb-vim'))
  const [invis, setInvis] = useState(() => flag('webtuidb-invisibles'))
  const ed = meta.config.editor
  const [edH, setEdH] = useState(() => { try { return Math.max(80, Math.min(900, Number(localStorage.getItem('webtuidb-editor-h')) || 220)) } catch { return 220 } })
  const dragH = (e: React.MouseEvent) => {
    e.preventDefault()
    const y0 = e.clientY, h0 = edH
    let h = h0
    const move = (m: MouseEvent) => { h = Math.max(80, Math.min(900, h0 + m.clientY - y0)); setEdH(h) }
    const up = () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); try { localStorage.setItem('webtuidb-editor-h', String(h)) } catch { /* private mode */ } }
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up)
  }
  const [wrap, setWrap] = useState(() => { try { const v = localStorage.getItem('webtuidb-wrap'); return v === null ? !!ed.Wrap : v === '1' } catch { return !!ed.Wrap } })
  const [text, setTextState] = useState(initial)
  const setText = (t: string) => { setTextState(t); onText?.(t) }
  const [showRes, setShowRes] = useState(true)
  const [msgView, setMsgView] = useState(false)
  const [results, setResults] = useState<ExecItem[]>([])
  const [active, setActive] = useState(0)
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)
  const [limit, setLimit] = useState(0)
  const [gate, setGate] = useState<Pending | null>(null)
  const [password, setPassword] = useState('')
  const [json, setJson] = useState(false)
  const [sel, setSel] = useState<string | null>(null)
  const [cell, setCell] = useState<CellTarget | null>(null)
  const [vars, setVars] = useState<{ names: string[]; body: Record<string, unknown> } | null>(null)
  const [dlg, setDlg] = useState<'' | 'export' | 'copyas' | 'cellsas' | 'setvalue' | 'sort'>('')
  const [cmenu, setCmenu] = useState<{ x: number; y: number } | null>(null) // the result cell menu
  const [hmenu, setHmenu] = useState<{ x: number; y: number; col: string } | null>(null) // the column header menu
  const [cursor, setCursor] = useState<{ col: number; range: Range | null }>({ col: 0, range: null })
  const [rsort, setRsort] = useState<Sort[]>([]) // client-side sort of the loaded rows
  const root = useRef<HTMLDivElement>(null)
  const savedAs = useRef(savedName ?? '') // the saved query this tab writes to: [editor] auto_save_queries saves on every change
  const lastSaved = useRef(initial)
  const [chart, setChart] = useState(false)
  const [syntax, setSyntaxState] = useState(() => { try { return localStorage.getItem('webtuidb-mongo-syntax') === 'command' ? 'command' : 'shell' } catch { return 'shell' } }) // Mongo: shell JavaScript or a command document
  const setSyntax = (v: string) => { setSyntaxState(v); try { localStorage.setItem('webtuidb-mongo-syntax', v) } catch { /* private mode */ } }
  const [slow, setSlow] = useState(false) // the query ran past [general] query_timeout_secs: keep waiting or cancel
  const [slowTick, setSlowTick] = useState(0)
  const timeoutSecs = Number(meta.config.general.QueryTimeoutSecs) || 0
  const [find, setFind] = useState<FindState | null>(null) // the results find / replace bar; null = closed
  const [focusCell, setFocusCell] = useState<{ row: string; col: number; n: number } | null>(null)
  const focusN = useRef(0)
  // M12.2: pending changes of the editable results, by resultId (a re-run gets a new id). hist is the undo stack: the state before each change.
  const [pends, setPends] = useState<Record<string, Pend>>({})
  const hist = useRef<{ id: string; p: Pend }[]>([])
  const nextId = useRef(1)
  const [plan, setPlan] = useState<Plan | null>(null)
  const busy = useRef(false)
  const pendingTotal = Object.values(pends).reduce((n, p) => n + pendCount(p), 0)
  useEffect(() => { onDirty?.(pendingTotal > 0) }, [pendingTotal]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onDirty?.(false), []) // eslint-disable-line react-hooks/exhaustive-deps
  const [explain, setExplain] = useState<{ Text: string; Current?: boolean; Offset?: number } | null>(null)
  const pinId = tabKey.replace(/[^A-Za-z0-9_-]/g, '_')
  const [pinned, setPinned] = useState(false) // this tab keeps one dedicated connection (BEGIN, SET and temp tables carry over)
  const pinnedRef = useRef(false); pinnedRef.current = pinned
  useEffect(() => {
    const gone = () => { if (pinnedRef.current) beacon(`/sessions/${sid}/pin/${pinId}`) } // the server also sweeps pins idle for 10 minutes
    window.addEventListener('pagehide', gone)
    return () => { window.removeEventListener('pagehide', gone); if (pinnedRef.current) api('DELETE', `/sessions/${sid}/pin/${pinId}`).catch(() => {}) }
  }, [])
  const colCache = useRef(new Map<string, Promise<string[]>>())
  const objs = useQuery({ queryKey: ['acobjects', sid, info.database, info.schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + (info.schema ? `?schema=${encodeURIComponent(info.schema)}` : '')), gcTime: 0 })
  const saved = useQuery({ queryKey: ['acqueries'], queryFn: () => api<{ Name: string; SQL: string; Keywords: string[] | null }[]>('GET', '/queries'), gcTime: 0 })
  const colRef = useRef(0)
  const view = useRef<EditorView | null>(null) // the editor that has the focus
  // split_down: a second editor stacked under the first. shortcut: its text is not saved with the tab, and a run sends the focused editor's text
  const [second, setSecond] = useState<string | null>(null)
  const views = useRef<(EditorView | null)[]>([null, null])
  const [pane, setPane] = useState(0)
  // block_select: a rectangle grown from the cursor with the arrows (or Shift-Alt-arrows at any time)
  const [blockOn, setBlockOn] = useState(false)
  const blockOnRef = useRef(false); blockOnRef.current = blockOn
  const blk = useRef<{ a: { line: number; col: number }; h: { line: number; col: number }; head: number } | null>(null)
  const ctrl = useRef<AbortController | null>(null)

  const send = async (body: Record<string, unknown>) => {
    if (running) return
    if (pendingTotal > 0 && !(await confirm('Discard pending changes in the result tabs?'))) return
    setPends({}); hist.current = []
    const mine = ctrl.current = new AbortController()
    setRunning(true); setError(''); setGate(null)
    const t0 = performance.now()
    try {
      const keep = meta.config.editor.ResultMode === 'append' ? results : results.filter((r) => r.pinned) // append: earlier result tabs stay; a pinned one always does
      const req = { ...body, limit, pin: pinned ? pinId : undefined, syntax: info.caps.Editor === 'mongo' ? syntax : undefined }
      // a big result streams in, so the first rows show while the rest arrives; small ones keep the one-shot path
      const items = (limit < 0 || (limit || info.rowLimit || -1) > 1000) ? await runStream(req, mine.signal, keep) : (await api<{ results: ExecItem[] }>('POST', `/sessions/${sid}/exec`, req, mine.signal)).results
      const at = items.findIndex((x) => x.error)
      setResults([...keep, ...items]); setJson(false); setChart(false); setFind(null); setFocusCell(null); setActive(keep.length + Math.max(0, at)); setPassword('')
      if (!items.length) setError('Nothing ran. Type a statement first.')
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') setError(`Cancelled after ${Math.round(performance.now() - t0)} ms.`)
      else if (e instanceof ApiError && e.status === 422 && e.body.params) {
        setVars({ names: e.body.params, body })
      } else if (e instanceof ApiError && e.status === 409 && e.body.gate) {
        setGate({ body, gate: e.body.gate, statements: e.body.statements ?? [], label: e.body.level ?? '', error: password ? e.message : undefined })
      } else setError(e instanceof Error ? e.message : String(e))
    } finally { setRunning(false) }
  }
  // POST .../exec/stream: columns, then rows in batches, then done per statement. A cancel keeps the rows that arrived.
  const runStream = async (req: Record<string, unknown>, signal: AbortSignal, keep: ExecItem[]) => {
    const items: ExecItem[] = []
    let last = 0
    const show = (force = false) => { if (force || performance.now() - last > 120) { last = performance.now(); setResults([...keep, ...items.map((x) => ({ ...x }))]) } }
    try {
      await stream('POST', `/sessions/${sid}/exec/stream`, req, (ev, d) => {
        if (ev === 'columns') { items[d.i] = { stmt: d.stmt, label: d.label || undefined, columns: d.columns, cells: [], affected: 0, hasRows: true, durationMs: 0, truncated: false, editable: false }; if (d.i === 0) setActive(keep.length); show(true) }
        else if (ev === 'rows') { items[d.i].cells.push(...d.cells); show() }
        else if (ev === 'done') {
          const it = items[d.i] ?? { stmt: d.stmt, columns: [], cells: [], affected: 0, hasRows: false, durationMs: 0, truncated: false, editable: false }
          items[d.i] = { ...it, label: d.label || it.label, error: d.error || undefined, affected: d.affected, message: d.message, noticeId: d.noticeId || undefined, durationMs: d.durationMs, truncated: d.truncated, hasRows: d.hasRows, docs: d.docs ?? undefined, editable: !!d.editable, table: d.table ?? undefined, resultId: d.resultId || undefined }
          show(true)
        }
      }, signal)
    } catch (e) { show(true); throw e }
    return items
  }
  useEffect(() => {
    if (!running) return setSlow(false)
    if (!timeoutSecs) return
    const t = setTimeout(() => setSlow(true), timeoutSecs * 1000)
    return () => clearTimeout(t)
  }, [running, timeoutSecs, slowTick])
  const cancel = () => { if (running) { ctrl.current?.abort(); onCancel?.() } }
  const togglePin = async () => {
    try {
      if (pinned) await api('DELETE', `/sessions/${sid}/pin/${pinId}`)
      else await api('POST', `/sessions/${sid}/pin/${pinId}`)
      setPinned(!pinned); notify(pinned ? 'Connection released' : 'This tab now keeps one connection: BEGIN and SET carry over. Pin again to release it.')
    } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  useEffect(() => () => ctrl.current?.abort(), [])
  useEffect(() => { if (autorun) setTimeout(() => runRef.current(true)) }, []) // the assistant's Run: through the normal path, so safe mode applies

  const run = (all: boolean) => {
    const v = view.current; if (!v) return
    const doc = v.state.doc.toString(), sel = v.state.selection.main
    if (!sel.empty) return send({ text: v.state.sliceDoc(sel.from, sel.to) })
    if (all) return send({ text: doc })
    // the server cuts statements at byte offsets, so convert the cursor from UTF-16 units
    send({ text: doc, current: true, offset: new TextEncoder().encode(doc.slice(0, sel.head)).length })
  }
  const runRef = useRef(run); runRef.current = run
  const limitBox = useRef<SelectHandle>(null)
  // Fold by statement: statement offsets come from the server's db.Split so indexes match the terminal's. The hidden part starts at the end of the statement's first line.
  // The server keeps the folded indexes ([]int, /api/state/folds); the client also keeps the FNV-1a hash of each folded statement's trimmed text (m15_folds.ts),
  // so after an edit above, or when the tab opens again, the fold goes back on the same statement. Identical statements share a hash, like the terminal's.
  // shortcut: a gutter click is saved by the next edit or fold command, not at once.
  const stmtRanges = async (v: EditorView) => {
    const doc = v.state.doc
    return (await api<[number, number][]>('POST', '/split', { text: doc.toString(), dialect: info.caps.Dialect })).map(([a, b]) => ({ from: doc.lineAt(a).to, to: b, text: doc.sliceString(a, b) })) // one-line statements get an empty range (from >= to) to keep indexes aligned
  }
  const foldKeyQ = `?key=${encodeURIComponent(tabKey)}`
  const saveFolds = (v: EditorView, rs: { from: number; to: number; text: string }[]) => {
    const idx: number[] = [], hashes: string[] = []
    rs.forEach((x, k) => foldedRanges(v.state).between(x.from, x.from, (a) => { if (a === x.from) { idx.push(k); hashes.push(stmtHash(x.text)) } }))
    saveHashes(tabKey, hashes)
    api('PUT', '/state/folds' + foldKeyQ, idx).catch(() => {})
  }
  const foldStatement = async () => {
    const v = view.current; if (!v) return
    const rs = await stmtRanges(v), head = v.state.selection.main.head
    const r = rs.find((x) => head >= v.state.doc.lineAt(x.from).from && head <= x.to)
    if (!r || r.from >= r.to) { notify('No multi-line statement here to fold'); return }
    const was = (() => { let f = false; foldedRanges(v.state).between(r.from, r.from, (a) => { f = a === r.from }); return f })()
    v.dispatch({ effects: (was ? unfoldEffect : foldEffect).of({ from: r.from, to: r.to }) })
    saveFolds(v, rs)
  }
  const restoreFolds = async (v: EditorView) => {
    try {
      const [idx, rs] = await Promise.all([api<number[]>('GET', '/state/folds' + foldKeyQ), stmtRanges(v)])
      const hashes = loadHashes(tabKey)
      const want = hashes ? foldedIndexes(hashes, rs.map((x) => x.text)) : idx
      const effects = want.flatMap((k) => rs[k] && rs[k].from < rs[k].to ? [foldEffect.of({ from: rs[k].from, to: rs[k].to })] : [])
      if (effects.length) v.dispatch({ effects })
      if (hashes && JSON.stringify(want) !== JSON.stringify(idx)) saveFolds(v, rs) // the statements moved: the server's indexes follow
    } catch { /* folds are a convenience */ }
  }
  const foldTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const syncFolds = () => { // after an edit: the folded statements may have moved, so the saved indexes are derived again from what is folded now
    clearTimeout(foldTimer.current)
    foldTimer.current = setTimeout(async () => {
      const v = views.current[0]
      if (!v || (!foldedRanges(v.state).size && !loadHashes(tabKey)?.length)) return
      try { saveFolds(v, await stmtRanges(v)) } catch { /* a convenience */ }
    }, 1200)
  }
  useEffect(() => () => clearTimeout(foldTimer.current), [])
  useEffect(() => {
    if (!shown) return
    // answered only while the focus is in an editable result grid (the editor keeps undo, add_row etc. otherwise)
    const resActs: Record<string, () => void> = { add_row: addRow, duplicate_rows: duplicate, delete_rows: delRows, commit, preview, discard, undo, set_value: setValue }
    const acts: Record<string, () => void> = {
      run_current: () => run(false), run_all: () => run(true), cancel_query: cancel, explain: explainNow, pin_connection: () => (info.pinnable ? togglePin() : notify(`${info.driver} cannot pin a connection to a tab`)),
      toggle_results: () => setShowRes((v) => !v),
      wrap_lines: () => { setFlag('webtuidb-wrap', !wrap); setWrap(!wrap) },
      cleanup_variables: () => { delete savedVars[sid]; notify('Forgot the remembered variables') },
      toggle_comment: () => view.current && toggleComment(view.current), block_comment: () => view.current && toggleBlockComment(view.current),
      upper_case: () => mapSel((t) => t.toUpperCase()), lower_case: () => mapSel((t) => t.toLowerCase()),
      capitalize: () => mapSel((t) => t.toLowerCase().replace(/(^|[^\p{L}\p{N}_])(\p{L})/gu, (_, a, b) => a + b.toUpperCase())),
      use_selection_find: useSelectionFind, jump_to_selection: () => { const v = view.current; if (v) { v.focus(); v.dispatch({ effects: View.scrollIntoView(v.state.selection.main.head, { y: 'center' }) }) } },
      complete: () => { if (view.current) { view.current.focus(); startCompletion(view.current) } },
      save_sql_as: saveSqlAs,
      add_to_queries: () => { if (text.trim()) void save() }, set_limit: () => limitBox.current?.open(),
      beautify: () => reformat('beautify'), uglify: () => reformat('uglify'), variables: askVars,
      find: () => { if (contextOf() === 'results' && cur?.hasRows) setFind((f) => f ?? newFind()); else findInEditor() }, find_replace: findInEditor,
      export: () => { if (cur?.hasRows && !cur.error) setDlg('export'); else notify('Run a query first, then export its result') },
      copy_rows: () => copyRows('Plain'), copy_rows_as: () => { if (cur?.hasRows) setDlg('copyas') }, copy_cells: copyCell,
      copy_cells_as: () => { if (cur?.hasRows) setDlg('cellsas') }, copy_column: () => copyColumn(), column_menu: () => { if (cur?.hasRows && !chart) headerAt(Math.min(colRef.current, cur.columns.length - 1)) },
      chart: () => { if (cur?.hasRows) setChart(!chart) }, load_more: loadMore,
      // select_next_occurrence ADDS a caret (CodeMirror keeps the earlier ranges; allowMultipleSelections is on in ext below), unlike the terminal's single jumping cursor
      select_next_occurrence: () => view.current && selectNextOccurrence(view.current), select_all_occurrences: () => view.current && selectSelectionMatches(view.current),
      select_word: selectWord, select_all: () => { const v = view.current; if (v) { v.focus(); selectAll(v) } },
      vim_mode: () => { setFlag('webtuidb-vim', !vimOn); setVimOn(!vimOn); notify(`Vim keys ${vimOn ? 'off' : 'on'}`) },
      fold: foldStatement,
      split_down: toggleSecond, block_select: toggleBlock,
      find_next: () => (contextOf() === 'results' && find ? stepRes(1) : stepFind(findNext)), find_prev: () => (contextOf() === 'results' && find ? stepRes(-1) : stepFind(findPrevious)),
      ...(second !== null ? { next_pane: () => focusPane(1 - pane), prev_pane: () => focusPane(1 - pane) } : {}),
      invisibles: () => { setFlag('webtuidb-invisibles', !invis); setInvis(!invis) },
    }
    const h = (e: Event) => { if (e.defaultPrevented) return; const a = (e as CustomEvent).detail.action; const f = acts[a] ?? (canEdit && contextOf() === 'results' ? resActs[a] : undefined); if (f) { e.preventDefault(); f() } }
    // plugin output of type sql lands in the editor of the visible query tab
    const ins = (e: Event) => { const v = view.current; if (v) { e.preventDefault(); v.dispatch(v.state.replaceSelection((e as CustomEvent).detail.text)) } }
    // :w saves the query (asks for a name the first time), :q closes the tab unless the text changed since the last write, :q! and :wq as in vim
    const vx = async (e: Event) => {
      const n = (e as CustomEvent).detail.name as string
      if (n === 'w' || n === 'wq') { if (!(await writeQuery())) return }
      if (n === 'q' && text !== lastSaved.current) return notify('No write since the last change (add ! to override)')
      if (n === 'q' || n === 'q!' || n === 'wq') { if (onClose) onClose(); else window.dispatchEvent(new CustomEvent('webtuidb:global', { detail: { action: 'close_tab' } })) }
    }
    window.addEventListener('webtuidb:action', h)
    window.addEventListener('webtuidb:insert', ins)
    window.addEventListener('webtuidb:vim-ex', vx)
    return () => { window.removeEventListener('webtuidb:action', h); window.removeEventListener('webtuidb:insert', ins); window.removeEventListener('webtuidb:vim-ex', vx) }
  })

  const selectWord = () => { // alt+D: the word under each caret
    const v = view.current; if (!v) return
    v.focus()
    v.dispatch({ selection: EditorSelection.create(v.state.selection.ranges.map((r) => v.state.wordAt(r.head) ?? r), v.state.selection.mainIndex) })
  }
  const focusPane = (k: number) => { const v = views.current[k]; if (v) { view.current = v; setPane(k); v.focus() } }
  const toggleSecond = () => {
    if (second === null) { setSecond(''); setTimeout(() => focusPane(1), 50) } else { setSecond(null); views.current[1] = null; view.current = views.current[0]; setPane(0); views.current[0]?.focus() }
  }
  const stepFind = (f: (v: EditorView) => boolean) => {
    const v = view.current; if (!v) return
    if (!getSearchQuery(v.state).search) { openSearchPanel(v); return notify('Type what to find first, then find next / find previous') }
    f(v)
  }
  const toggleBlock = () => { blk.current = null; setBlockOn(!blockOn); if (view.current) view.current.focus(); notify(blockOn ? 'Block selection off' : 'Block selection on: the arrows grow the rectangle, Esc leaves') }
  // blockStep moves the rectangle's moving corner by (dl lines, dc columns). shortcut: columns count characters, so tabs and wide characters shift the rectangle
  const blockStep = (dl: number, dc: number) => {
    const v = view.current; if (!v) return false
    const doc = v.state.doc, sel = v.state.selection.main
    if (!blk.current || blk.current.head !== sel.head) { const l = doc.lineAt(sel.head), c = sel.head - l.from; blk.current = { a: { line: l.number, col: c }, h: { line: l.number, col: c }, head: sel.head } }
    const b = blk.current
    b.h = { line: Math.max(1, Math.min(doc.lines, b.h.line + dl)), col: Math.max(0, b.h.col + dc) }
    const lo = Math.min(b.a.col, b.h.col), hi = Math.max(b.a.col, b.h.col), l0 = Math.min(b.a.line, b.h.line), l1 = Math.max(b.a.line, b.h.line)
    const ranges = []
    for (let n = l0; n <= l1; n++) { const L = doc.line(n); ranges.push(EditorSelection.range(L.from + Math.min(lo, L.length), L.from + Math.min(hi, L.length))) }
    const main = b.h.line - l0
    b.head = ranges[main].head
    v.dispatch({ selection: EditorSelection.create(ranges, main), scrollIntoView: true })
    return true
  }
  const mapSel = (f: (t: string) => string) => {
    const v = view.current; if (!v) return
    if (v.state.selection.main.empty) return notify('Select some text first')
    v.dispatch(v.state.changeByRange((r) => { const n = f(v.state.sliceDoc(r.from, r.to)); return { changes: { from: r.from, to: r.to, insert: n }, range: EditorSelection.range(r.from, r.from + n.length) } }))
  }
  const useSelectionFind = () => {
    const v = view.current; if (!v) return
    const t = v.state.sliceDoc(v.state.selection.main.from, v.state.selection.main.to)
    if (!t) return notify('Select some text first')
    v.dispatch({ effects: setSearchQuery.of(new SearchQuery({ search: t })) }); findNext(v)
  }
  const saveSqlAs = async () => {
    const name = await ask('Save SQL as', 'query.sql'); if (!name) return
    saveBlob(new Blob([text], { type: 'text/plain;charset=utf-8' }), /\.\w+$/.test(name) ? name : name + '.sql')
  }
  // the selection if there is one, else the whole text
  const target = () => {
    const v = view.current!, sel = v.state.selection.main
    return sel.empty ? { from: 0, to: v.state.doc.length } : { from: sel.from, to: sel.to }
  }
  const reformat = async (mode: 'beautify' | 'uglify') => {
    const v = view.current; if (!v) return
    const { from, to } = target()
    try {
      const r = await api<{ text: string }>('POST', '/format', { text: v.state.sliceDoc(from, to), mode, dialect: info.caps.Dialect, IndentType: ed.IndentType })
      v.dispatch({ changes: { from, to, insert: r.text } })
    } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  const explainNow = () => {
    const v = view.current; if (!v) return
    const sel = v.state.selection.main
    if (!sel.empty) return setExplain({ Text: v.state.sliceDoc(sel.from, sel.to) })
    const doc = v.state.doc.toString()
    setExplain({ Text: doc, Current: true, Offset: new TextEncoder().encode(doc.slice(0, sel.head)).length })
  }
  const findInEditor = () => { if (view.current) { view.current.focus(); openSearchPanel(view.current) } }
  const askVars = async () => {
    const v = view.current; if (!v) return
    const { from, to } = target(), t = v.state.sliceDoc(from, to)
    try {
      const r = await api<{ params: string[] }>('POST', '/params', { text: t, dialect: info.caps.Dialect })
      if (!r.params.length) return notify('No query parameters in the text')
      setVars({ names: r.params, body: { text: t } })
    } catch (e) { notify(e instanceof Error ? e.message : String(e)) }
  }
  // Load more asks for the next page of a plain SELECT (POST .../exec/page); anything else falls back to re-running the statement with a doubled limit.
  // shortcut: a page is a subquery that runs again (no server cursor), so a slow query is slow once per page.
  const loadingMore = useRef(false)
  const loadMore = async () => {
    if (!cur?.truncated) return notify('Nothing more to load')
    if (loadingMore.current) return
    if (cur.editable && pendCount(P) > 0 && !(await confirm('Loading more rows discards the pending changes in this result. Continue?'))) return
    loadingMore.current = true
    if (cur.resultId) setPends((m) => { const { [cur.resultId!]: _g, ...rest } = m; return rest })
    try {
      const size = cur.pageSize ?? cur.cells.length
      const p = await api<{ supported: boolean; result?: ExecItem; more?: boolean }>('POST', `/sessions/${sid}/exec/page`, { text: cur.stmt, limit: size, offset: cur.cells.length, pin: pinned ? pinId : undefined })
      if (p.supported && p.result) {
        const add = p.result
        // shortcut: the server snapshot behind resultId holds the first page only, so a result that grew by Load more is no longer editable
        return setResults((all) => all.map((x, i) => (i === active ? { ...x, editable: false, cells: [...x.cells, ...add.cells], truncated: !!p.more, pageSize: size, durationMs: x.durationMs + add.durationMs } : x)))
      }
      const r = await api<{ results: ExecItem[] }>('POST', `/sessions/${sid}/exec`, { text: cur.stmt, limit: cur.cells.length * 2, vars: {}, pin: pinned ? pinId : undefined })
      if (r.results[0]?.error) return notify(r.results[0].error)
      setResults((all) => all.map((x, i) => (i === active ? r.results[0] : x)))
    } catch (e) { notify(e instanceof ApiError && e.status === 409 ? 'This statement needs confirmation, so it cannot be re-run to load more' : e instanceof Error ? e.message : String(e)) } finally { loadingMore.current = false }
  }
  const copyRows = (f: CopyFormat) => {
    if (!cur?.hasRows) return
    const idx = sel && sel[0] === 'r' ? [Number(sel.slice(1))] : cur.cells.map((_, i) => i)
    copyText(formatRows(f, cur.columns, idx.map((i) => cur.cells[i]), meta.config.csv, info.caps.Dialect)).then(() => notify(`Copied ${idx.length} row${idx.length === 1 ? '' : 's'}`), (e) => notify(String(e)))
  }
  const copyCell = () => {
    if (!cur?.hasRows || !sel || sel[0] !== 'r') return notify('Select a row first')
    copyText(cur.cells[Number(sel.slice(1))]?.[colRef.current] ?? 'NULL').then(() => notify('Copied the cell'), (e) => notify(String(e)))
  }

  // copy_cells_as / copy_column (M15.1, M15.2) and the header menu (M15.5) for result grids
  const copyCellsAs = (f: CopyFormat) => {
    if (!cur?.hasRows) return
    const at = gridRows.findIndex((r) => r.key === sel)
    if (at < 0) return notify('Select a cell first')
    const rect = rectOf(cursor.range, at, Math.min(colRef.current, cur.columns.length - 1))
    copyText(cellsAs(f, rect, cur.columns, (r, c) => cellVal(gridRows[r], c), meta.config.csv, info.caps.Dialect, cur.table?.name)).then(() => notify(`Copied ${rectSize(rect)} cell${rectSize(rect) === 1 ? '' : 's'} as ${f}`), (e) => notify(String(e)))
  }
  const copyColumn = (name?: string) => {
    if (!cur?.hasRows) return
    const ci = name ? cur.columns.findIndex((c) => c.name === name) : Math.min(colRef.current, cur.columns.length - 1)
    if (ci < 0) return
    copyText(columnText(sortedRows.length, ci, (r, c) => cellVal(sortedRows[r], c))).then(() => notify(`Copied the values of ${cur.columns[ci].name}`), (e) => notify(String(e)))
  }
  const colAct = (action: string, col: string) => {
    switch (action) {
      case 'sort_asc': return setRsort([{ Column: col, Desc: false }])
      case 'sort_desc': return setRsort([{ Column: col, Desc: true }])
      case 'sort_none': return setRsort((l) => l.filter((x) => x.Column !== col))
      case 'sort_columns': return setDlg('sort')
      case 'copy_name': return void copyText(col).then(() => notify('Copied the column name'), (e) => notify(String(e)))
      case 'copy_column': return copyColumn(col)
      case 'copy_definition': { const c = cur?.columns.find((x) => x.name === col); return void copyText(c ? `${c.name} ${c.type}${c.pk ? ' PRIMARY KEY' : ''}` : col).then(() => notify('Copied the column definition'), (e) => notify(String(e))) }
    }
    notify(`${action}: not available here`)
  }
  const headerAt = (col: number) => {
    const r = root.current?.querySelector<HTMLElement>(`[data-h="${col}"]`)?.getBoundingClientRect()
    if (cur?.columns[col]) setHmenu({ col: cur.columns[col].name, x: r ? r.left : 40, y: r ? r.bottom : 120 })
  }
  // result grid cell menu actions: the grid's keymap actions, answered here
  const cellActs = (): Record<string, () => void> => ({
    quick_look: () => { const r = gridRows.find((x) => x.key === sel); if (r && cur) setCell({ row: r.key, col: cur.columns[Math.min(colRef.current, cur.columns.length - 1)].name, readOnly: true }) },
    add_row: addRow, duplicate_rows: duplicate, delete_rows: delRows, set_value: setValue,
    copy_rows: () => copyRows('Plain'), copy_rows_as: () => setDlg('copyas'), copy_cells: copyCell, copy_cells_as: () => setDlg('cellsas'), copy_column: () => copyColumn(),
    chart: () => setChart(!chart), load_more: loadMore, pin: () => cur && setResults((all) => all.map((x, i) => (i === active ? { ...x, pinned: !x.pinned } : x))), export: () => (cur?.hasRows && !cur.error ? setDlg('export') : notify('Run a query first, then export its result')),
  })
  const nextCell = (from: CellTarget, move: 1 | -1) => { // Tab while editing: save and edit the neighbouring cell
    if (!cur) return
    const ri = gridRows.findIndex((r) => r.key === from.row), ci = cur.columns.findIndex((c) => c.name === from.col)
    if (ri < 0 || ci < 0) return
    let r = ri, c = ci + move
    if (c >= cur.columns.length) { c = 0; r++ } else if (c < 0) { c = cur.columns.length - 1; r-- }
    const row = gridRows[r]
    if (!row) return
    setSel(row.key); setFocusCell({ row: row.key, col: c, n: ++focusN.current })
    setCell({ row: row.key, col: cur.columns[c].name, readOnly: !canEdit || row.state === 'deleted' })
  }

  const exportAll = async () => {
    const list = results.flatMap((r, i) => (r.hasRows && !r.error ? [[r, i] as const] : []))
    for (const [r, i] of list) {
      try { const f = await download(`/sessions/${sid}/export`, { mode: 'query', format: 'csv', gzip: false, columns: [], text: r.stmt, limit }); saveBlob(f.blob, `result-${i + 1}.csv`) } catch (e) { return notify(e instanceof Error ? e.message : String(e)) }
    }
    notify(`Exported ${list.length} result tab${list.length === 1 ? '' : 's'}`)
  }
  const save = async () => {
    const name = await ask('Save query as'); if (!name) return
    try { await postQuery('/queries', { Name: name, SQL: text }); notify('Saved to Queries') } catch (e) { notify(String(e)) }
  }

  const dialect = info.caps.Dialect
  // Autocomplete sources follow [editor] complete_tables / complete_functions / complete_keywords; add_space and prefix_schema shape the inserted text.
  const sqlSupport = () => {
    const d = dialect === 'postgres' ? PostgreSQL : dialect === 'mysql' ? MySQL : StandardSQL
    const comps: Completion[] = (objs.data ?? []).flatMap((o) => {
      const fn = o.Kind === 'function' || o.Kind === 'procedure'
      if (fn ? !ed.CompleteFunctions : !ed.CompleteTables || !['table', 'view', 'matview'].includes(o.Kind)) return []
      const label = ed.PrefixSchema && o.Schema ? `${o.Schema}.${o.Name}` : o.Name
      return [{ label, type: fn ? 'function' : o.Kind === 'table' ? 'class' : 'type', detail: o.Kind, apply: ed.AddSpace ? label + ' ' : label }]
    })
    const colsOf = (table: string): Promise<string[]> => {
      const o = (objs.data ?? []).find((x) => x.Name.toLowerCase() === table && ['table', 'view', 'matview'].includes(x.Kind))
      if (!o) return Promise.resolve([])
      const k = (o.Schema ?? '') + '.' + o.Name
      if (!colCache.current.has(k)) colCache.current.set(k, api<Column[]>('GET', `/sessions/${sid}/object/columns` + qs({ schema: o.Schema, name: o.Name, kind: o.Kind })).then((l) => l.map((c) => c.Name), () => { colCache.current.delete(k); return [] }))
      return colCache.current.get(k)!
    }
    // columns of the tables the statement reads (alias. narrows to one), and the saved queries whose keywords expand (snippet:<trigger> expands from <trigger>)
    const smart = async (ctx: CompletionContext): Promise<CompletionResult | null> => {
      const w = ctx.matchBefore(/(?:[\w"]+\.)?[\w"]*/)
      if (!w || (w.from === w.to && !ctx.explicit)) return null
      const dot = w.text.lastIndexOf('.'), qual = dot >= 0 ? w.text.slice(0, dot).replace(/"/g, '').toLowerCase() : ''
      const als = aliasesIn(ctx.state.doc.toString()), opts: Completion[] = []
      const tables = qual ? (als[qual] ? [als[qual]] : []) : [...new Set(Object.values(als))]
      for (const t of tables) for (const c of await colsOf(t)) opts.push({ label: c, type: 'property', detail: t })
      if (!qual) for (const q of saved.data ?? []) for (const k of q.Keywords ?? []) {
        const sn = k.startsWith('snippet:')
        opts.push({ label: sn ? k.slice(8) : k, type: 'text', detail: sn ? 'snippet' : 'saved query', apply: q.SQL, boost: -1 })
      }
      return opts.length ? { from: dot >= 0 ? w.from + dot + 1 : w.from, options: opts, validFor: /^[\w"]*$/ } : null
    }
    return new LanguageSupport(d.language, [
      d.language.data.of({ autocomplete: smart }),
      ...(comps.length ? [d.language.data.of({ autocomplete: schemaCompletionSource({ dialect: d, schema: {}, tables: comps }) })] : []),
      ...(ed.CompleteKeywords !== false ? [d.language.data.of({ autocomplete: keywordCompletionSource(d, !!ed.UppercaseKeywords) })] : []),
    ])
  }
  const ext = [
    ...(vimOn ? [Prec.high(vim())] : []),
    ...(invis ? [highlightWhitespace()] : []),
    Prec.highest(keymap.of([
      { key: 'Mod-Enter', run: () => { runRef.current(false); return true } },
      { key: 'Mod-Shift-Enter', run: () => { runRef.current(true); return true } },
      { key: 'Shift-Alt-ArrowUp', run: () => blockStep(-1, 0) }, { key: 'Shift-Alt-ArrowDown', run: () => blockStep(1, 0) },
      { key: 'Shift-Alt-ArrowLeft', run: () => blockStep(0, -1) }, { key: 'Shift-Alt-ArrowRight', run: () => blockStep(0, 1) },
      { key: 'ArrowUp', run: () => blockOnRef.current && blockStep(-1, 0) }, { key: 'ArrowDown', run: () => blockOnRef.current && blockStep(1, 0) },
      { key: 'ArrowLeft', run: () => blockOnRef.current && blockStep(0, -1) }, { key: 'ArrowRight', run: () => blockOnRef.current && blockStep(0, 1) },
      { key: 'Escape', run: (v) => { if (!blockOnRef.current) return false; setBlockOn(false); blk.current = null; v.dispatch({ selection: EditorSelection.cursor(v.state.selection.main.head) }); return true } },
    ])),
    rectangularSelection(), crosshairCursor(), // Alt-drag with the mouse
    ...(dialect === 'mongo' ? [EditorState.languageData.of(() => [{ autocomplete: mongoSource }])] : []),
    ...(wrap ? [View.lineWrapping] : []),
    ...(info.caps.Editor === 'sql' ? [sqlSupport()] : []),
  ]
  const cur = results[active]

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="toolbar bg-canvas">
        {/* [editor] run_default decides what the primary button runs */}
        <button className="btn-primary" disabled={running} onClick={() => run(ed.RunDefault === 'all')} title={ed.RunDefault === 'all' ? 'Run all (Ctrl/Cmd+Shift+Enter)' : 'Run the current statement (Ctrl/Cmd+Enter)'}>Run{ed.RunDefault === 'all' ? ' all' : ''}</button>
        <button disabled={running} onClick={() => run(ed.RunDefault !== 'all')} title={ed.RunDefault === 'all' ? 'Run the current statement (Ctrl/Cmd+Enter)' : 'Run all (Ctrl/Cmd+Shift+Enter)'}>{ed.RunDefault === 'all' ? 'Run current' : 'Run all'}</button>
        <button disabled={!running} onClick={cancel}>Cancel</button>
        {info.caps.Editor === 'sql' && <button onClick={explainNow} title="Explain the statement at the cursor (or the selection) as a tree">Explain</button>}
        {info.pinnable && <button className={pinned ? 'btn-on' : ''} aria-pressed={pinned} onClick={togglePin} title="Keep one connection for this tab, so BEGIN, SET and temp tables carry over">{pinned ? 'Pinned' : 'Pin'}</button>}
        <Select ref={limitBox} className="bg-raised border-transparent" title="row limit" aria-label="Row limit" value={String(limit)} onChange={(v) => setLimit(Number(v))}
          options={[{ value: '0', label: `Limit ${info.rowLimit}` }, '100', '1000', '10000', { value: '-1', label: 'No limit' }]} />
        <span className="flex-1 min-w-0" />
        <button onClick={() => reformat('beautify')} title="Beautify SQL (selection or all)">Beautify</button>
        <button onClick={() => reformat('uglify')} title="Uglify SQL (selection or all)">Uglify</button>
        {blockOn && <span className="px-2 border border-rule-2 rounded-[2px] text-[11.5px] text-ink-2" title="Block selection: the arrows grow the rectangle, Esc leaves">block</span>}
        <button onClick={toggleSecond} className={second !== null ? 'btn-on' : ''} aria-pressed={second !== null} title="Stack a second editor under this one">Split down</button>
        <button onClick={save} disabled={!text.trim()}>Save query</button>
      </div>
      <div className="border-b border-rule" data-ctx="editor" onFocusCapture={() => { if (views.current[0]) { view.current = views.current[0]; setPane(0) } }}>
        <CodeMirror value={text} height={second === null ? '220px' : '140px'} theme={isDark() ? 'dark' : 'light'} extensions={ext} onChange={setText} onCreateEditor={(v) => { views.current[0] = v; view.current = v; restoreFolds(v) }}
          basicSetup={{ lineNumbers: true, foldGutter: true }} />
      </div>
      {second !== null && (
        <div className="border-b border-rule" data-ctx="editor" onFocusCapture={() => { if (views.current[1]) { view.current = views.current[1]; setPane(1) } }}>
          <CodeMirror value={second} height="140px" theme={isDark() ? 'dark' : 'light'} extensions={ext} onChange={setSecond} onCreateEditor={(v) => { views.current[1] = v }} basicSetup={{ lineNumbers: true, foldGutter: true }} />
        </div>
      )}
      {showRes && <>
      <div className="flex h-8 text-[12px] border-b border-rule overflow-x-auto [scrollbar-width:none] bg-canvas font-mono">
        {results.map((r, i) => (
          <div key={i} className={`flex items-center gap-2 pl-3.5 pr-2.5 cursor-pointer whitespace-nowrap border-r border-rule ${i === active ? 'text-ink bg-panel shadow-[inset_0_-2px_0_var(--accent)]' : 'text-ink-2 hover:text-ink hover:bg-panel'}`} onClick={() => setActive(i)} title={r.stmt}>
            <span className={r.error ? 'text-del' : ''}>{r.error ? '✗' : '✓'}</span>{i + 1}: {r.label ? r.label.slice(0, 28) : r.stmt.replace(/\s+/g, ' ').slice(0, 28)}
          </div>
        ))}
        {cur?.docs?.length ? <button className="btn-ghost" onClick={() => setJson(!json)}>{json ? 'Rows' : 'JSON'}</button> : null}
        {cur && !cur.error && <button className={`btn-ghost ${msgView ? 'btn-on' : ''}`} onClick={() => setMsgView(!msgView)}>Message</button>}
        {cur?.hasRows && !cur.error && <button className={`btn-ghost ${chart ? 'btn-on' : ''}`} onClick={() => setChart(!chart)}>Chart</button>}
        {results.some((r) => r.hasRows && !r.error) && <button className="btn-ghost" onClick={exportAll} title="One CSV file per result tab">Export all tabs</button>}
      </div>
      {error && <div className="p-2.5 text-[12.5px] text-del">{error}</div>}
      {running && <><div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-[color-mix(in_srgb,var(--accent)_20%,transparent)] after:absolute after:inset-0 after:w-[40%] after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Running" /><div className="p-2.5 text-[12.5px] text-ink-2">Running…</div></>}
      {cur && (msgView && !cur.error ? <MessageView sid={sid} item={cur} notify={notify} /> : cur.error ? <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch] text-del">{cur.error}</pre>
        : cur.hasRows ? (
          json && cur.docs ? <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch]">{cur.docs.map(pretty).join('\n')}</pre> : <>
            {find !== null && <div className="toolbar bg-canvas"><input autoFocus className="flex-1 min-w-0" aria-label="Search results" placeholder="Search results" value={find} onChange={(e) => setFind(e.target.value)} onKeyDown={(e) => { if (e.key === 'Escape') setFind(null) }} /><label className="flex items-center gap-1.5 text-ink-2"><input type="checkbox" checked={matchCase} onChange={(e) => setMatchCase(e.target.checked)} />Match case</label><button className="btn-ghost" onClick={() => setFind(null)} aria-label="Close search">✕</button></div>}
            {chart ? <ChartView notify={notify} stmt={cur.stmt} columns={cur.columns.map((c) => c.name)} cells={cur.cells} />
              : <Grid colRef={colRef} ctx="results" columns={cur.columns} rows={cur.cells.flatMap((cells, i): GridRow[] => (find && !cells.some((c) => (matchCase ? c?.includes(find) : c?.toLowerCase().includes(find.toLowerCase()))) ? [] : [{ key: 'r' + i, cells, state: 'clean' }]))} selected={sel} onSelect={setSel} onOpen={setCell} />}
            {cell && <CellEditor col={cell.col} readOnly value={cur.cells[Number(cell.row.slice(1))]?.[cur.columns.findIndex((c) => c.name === cell.col)] ?? null} onDone={() => setCell(null)} />}
            <div className="toolbar bg-canvas text-ink-2">{cur.cells.length} rows in {cur.durationMs} ms{cur.truncated ? '. Stopped at the row limit.' : ''}{cur.truncated && <button className="btn-ghost" onClick={loadMore}>Load more</button>}</div>
          </>
        ) : <MessageView sid={sid} item={cur} notify={notify} />)}
      </>}
      {vars && <VariablesDialog names={vars.names} init={savedVars[sid] ?? {}} format={String(ed.ParamFormat || 'colon')} onClose={() => setVars(null)}
        onFormat={async (f) => (await api<{ params: string[] }>('POST', '/params', { Text: vars.body.text, Dialect: info.caps.Dialect, ParamFormat: f, Current: vars.body.current, Offset: vars.body.offset })).params}
        onRun={(v, f) => { savedVars[sid] = { ...savedVars[sid], ...v }; const b = vars.body; setVars(null); send({ ...b, vars: v, paramFormat: f }) }} />}
      {explain && <ExplainDialog sid={sid} body={explain} onClose={() => setExplain(null)} />}
      {dlg === 'export' && cur && <ExportDialog sid={sid} notify={notify} onClose={() => setDlg('')} source={{ mode: 'query', text: cur.stmt, columns: [], limit }} />}
      {dlg === 'copyas' && <CopyAsDialog onClose={() => setDlg('')} onPick={(f) => { setDlg(''); copyRows(f) }} />}
      {gate && (
        <Dialog title="Run these statements?" onClose={() => setGate(null)}>
          <pre>{gate.statements.join('\n')}</pre>
          <GateFields gate={gate.gate} label={gate.label} password={password} setPassword={setPassword} />
          {gate.error && <div className="text-del" style={{ marginTop: 8 }}>{gate.error}</div>}
          <div className="flex justify-end gap-2 mt-[18px]">
            <button onClick={() => setGate(null)}>Cancel</button>
            <button className="btn-primary" disabled={gate.gate.password && !password} onClick={() => send({ ...gate.body, confirmed: true, password })}>Run</button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

const isDark = () => {
  const t = document.documentElement.dataset.theme
  return t === 'dark' || (t !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches)
}
const pretty = (d: string) => { try { return JSON.stringify(JSON.parse(d), null, 2) } catch { return d } }

// The Message view: the command tag or row count, the server notices (PostgreSQL RAISE NOTICE) and the time; it can be saved as text.
function MessageView({ sid, item, notify }: { sid: string; item: ExecItem; notify: (m: string) => void }) {
  const q = useQuery({ queryKey: ['notices', sid, item.noticeId], queryFn: () => api<{ message: string; notices: { severity: string; message: string }[] }>('GET', `/sessions/${sid}/notices/${item.noticeId}`), enabled: !!item.noticeId, gcTime: 0 })
  const head = q.data?.message || (item.message && !item.noticeId ? item.message : item.hasRows ? `${item.cells.length} rows` : `${item.affected} rows affected`)
  const text = [head, ...(q.data?.notices ?? []).map((n) => `${n.severity}: ${n.message}`), `${item.durationMs} ms`].join('\n')
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] whitespace-pre-wrap" data-ctx="results">{text}</pre>
      <div className="toolbar bg-canvas"><button onClick={() => saveBlob(new Blob([text + '\n'], { type: 'text/plain;charset=utf-8' }), 'message.txt')}>Save as text</button><button onClick={() => navigator.clipboard.writeText(text).then(() => notify('Copied the message'), (e) => notify(String(e)))}>Copy</button></div>
    </div>
  )
}
