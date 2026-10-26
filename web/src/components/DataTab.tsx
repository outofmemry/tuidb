import { useEffect, useMemo, useRef, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { tableStateBody } from '../utils/tablestate'
import { api, ApiError, clip, qs, recentKey, type Column, type Filter, type FilterSet, type Meta, type ObjRef, type Plan, type ResultData, type SessionInfo, type Sort, type Val } from '../utils/api'
import { PlanDialog, confirm } from './Dialogs'
import { Select } from './Select'
import { FilterBar } from './FilterBar'
import { Grid, show, type GridRow, type Range } from './Grid'
import { RowDetail } from './RowDetail'
import { FindBar, newFind, type FindState } from './GridFind'
import { StructureEditor, type StructureCtl } from './StructureEditor'
import { cellMatches, findNext, replaceIn } from '../utils/gridfind'
import { CellEditor, type CellTarget } from './CellEditor'
import { CopyAsDialog, ExportDialog, ImportDialog } from './Tools'
import { PageDialog, SetValueDialog, SortDialog, TabMenu, Tree, match } from './Extras'
import { BulkUpdateDialog, ColumnDialog, planMoveColumn } from './Wave'
import { copyText, formatRows, type CopyFormat } from '../utils/copy'
import { cellsAs, columnText, rectOf, rectSize } from '../utils/m15_copy'
import { columnDef, columnTip, listOf, menuItems } from '../utils/m15_menus'
import { cfg } from '../utils/m15_cfg'
import { ItemMenu } from './m15_Menu'

type Edits = Record<string, Record<string, Val>>
type Insert = { id: number; vals: Record<string, Val> }
type View = 'data' | 'structure' | 'json' | 'tree' | 'ddl'
type Pend = { edits: Edits; deletes: Set<string>; inserts: Insert[] }

export function DataTab({ sid, info, meta, obj, notify, active, initFilters, initView, onFollow, onDirty }: { sid: string; info: SessionInfo; meta: Meta; obj: ObjRef; notify: (m: string) => void; active?: boolean; initFilters?: FilterSet; initView?: View; onFollow?: (ref: ObjRef, f: FilterSet) => void; onDirty?: (dirty: boolean) => void }) {
  const base = `/sessions/${sid}`
  const refQ = qs({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })
  const stateQ = qs({ conn: info.name, db: info.database, table: recentKey(obj) })

  const [view, setView] = useState<View>(initView ?? 'data')
  const [filters, setFilters] = useState<FilterSet>({ Filters: [], Or: false })
  const [sort, setSort] = useState<Sort[]>([])
  const [hidden, setHidden] = useState<string[]>([])
  const [showBar, setShowBar] = useState(false)
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(info.rowLimit)
  const [nonce, setNonce] = useState(0)
  const [exact, setExact] = useState(false)
  const [ready, setReady] = useState(false)

  const [edits, setEdits] = useState<Edits>({})
  const [deletes, setDeletes] = useState<Set<string>>(new Set())
  const [inserts, setInserts] = useState<Insert[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [detail, setDetail] = useState(false)
  const [cell, setCell] = useState<CellTarget | null>(null)
  const [trayOpen, setTrayOpen] = useState(true)
  const [plan, setPlan] = useState<Plan | null>(null)
  const [planOwn, setPlanOwn] = useState(false) // the open plan was built from the staged cell edits (not from a tool dialog)
  const toolPlan = (p: Plan) => { setPlanOwn(false); setPlan(p) }
  const ownPlan = (p: Plan) => { setPlanOwn(true); setPlan(p) }
  const [dlg, setDlg] = useState<'' | 'export' | 'import' | 'copyas' | 'setvalue' | 'sort' | 'page' | 'bulk'>('')
  const [fieldQ, setFieldQ] = useState('') // row detail: Search for field…
  const [section, setSection] = useState('') // structure section to scroll to (show_index, triggers)
  const [hmenu, setHmenu] = useState<{ x: number; y: number; col: string } | null>(null)
  const [structCol, setStructCol] = useState('') // the structure row the cursor is on; Row detail (space) edits it as a form
  const [colForm, setColForm] = useState<Column | null>(null)
  const redo = useRef<Pend[]>([])
  const colRef = useRef(0)
  const cc = () => Math.max(0, Math.min(colRef.current, (rowsQ.data?.columns.length ?? 1) - 1)) // the cursor column, clamped: hiding it leaves colRef past the end
  const nextId = useRef(1)
  const colsMenu = useRef<HTMLDetailsElement>(null)

  // restore what the TUI (or an earlier visit) remembered for this table
  const saved = useQuery({ queryKey: ['tstate', sid, obj], queryFn: () => api<any>('GET', '/state/table' + stateQ), staleTime: Infinity, gcTime: 0 })
  useEffect(() => {
    if (saved.isSuccess && !ready) {
      const s = saved.data
      setFilters(initFilters ?? { Filters: s.filters?.Filters ?? [], Or: !!s.filters?.Or }) // a search hit brings its own filters
      setSort(s.sort ?? []); setHidden(s.hidden ?? []); setShowBar(!!initFilters || !!s.show_filters); setReady(true)
    }
    if (saved.isError && !ready) setReady(true)
  }, [saved.isSuccess, saved.isError, saved.data, ready])
  useEffect(() => {
    if (!ready) return
    const t = setTimeout(() => api('PUT', '/state/table' + stateQ, { hidden, filters, show_filters: showBar, sort }).catch(() => {}), 500)
    return () => clearTimeout(t)
  }, [ready, hidden, filters, showBar, sort, stateQ])

  const cols = useQuery({ queryKey: ['cols', sid, obj], queryFn: () => api<Column[]>('GET', `${base}/object/columns${refQ}`), gcTime: 0 })
  const colNames = useMemo(() => (cols.data ?? []).map((c) => c.Name), [cols.data])
  const visible = useMemo(() => colNames.filter((n) => !hidden.includes(n)), [colNames, hidden])
  const hasActive = filters.Filters.some((f) => f.Enabled)

  const rowsQ = useQuery({
    queryKey: ['rows', sid, obj, filters, sort, hidden, offset, limit, nonce],
    queryFn: ({ signal }) => api<ResultData>('POST', `${base}/rows`, { ref: obj, filters, sort, columns: hidden.length ? visible : [], limit, offset }, signal),
    enabled: ready && cols.isSuccess, placeholderData: keepPreviousData, gcTime: 0,
  })
  const countQ = useQuery({
    queryKey: ['count', sid, obj, filters, nonce, exact],
    queryFn: () => api<{ n: number; exact: boolean }>('POST', `${base}/count`, { ref: obj, filters, estimate: !exact && !hasActive }),
    enabled: ready && cols.isSuccess, gcTime: 0,
  })

  const res = rowsQ.data
  const stale = rowsQ.isPlaceholderData // the previous page stays on screen while reloading, but its server snapshot may be gone
  const pending = Object.keys(edits).length + deletes.size + inserts.length
  useEffect(() => { onDirty?.(pending > 0) }, [pending]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onDirty?.(false), []) // eslint-disable-line react-hooks/exhaustive-deps
  const clearPending = () => { setEdits({}); setDeletes(new Set()); setInserts([]); setSelected(null) }
  const guard = async () => pending === 0 || (await confirm('Discard pending changes?'))
  const reload = async () => { if (await guard()) { clearPending(); setNonce((n) => n + 1) } }
  const change = <T,>(set: (v: T) => void) => async (v: T) => { if (await guard()) { clearPending(); setOffset(0); set(v) } }

  // Row keys follow the primary-key value, so a pending edit stays on its row when a reload reorders or shifts the page.
  // A table without a usable key falls back to the row index, and then every reload drops the pending changes (see below).
  const keyed = useMemo(() => {
    const byKey = new Map<string, number>()
    if (!res) return { byKey, hasKey: false, keys: [] as string[] }
    const pk = res.columns.map((c, i) => (c.pk ? i : -1)).filter((i) => i >= 0)
    if (pk.length) {
      const keys = res.cells.map((cells) => 'r' + JSON.stringify(pk.map((i) => cells[i] ?? null)))
      keys.forEach((k, i) => byKey.set(k, i))
      if (byKey.size === keys.length) return { byKey, hasKey: true, keys }
      byKey.clear()
    }
    const keys = res.cells.map((_, i) => 'r' + i)
    keys.forEach((k, i) => byKey.set(k, i))
    return { byKey, hasKey: false, keys }
  }, [res])
  const rowIdx = (k: string) => keyed.byKey.get(k) ?? -1
  // a reload that changed the data under the pending changes: keep what still points at a row with the same key, drop the rest
  const seen = useRef<ResultData | undefined>(undefined)
  useEffect(() => {
    if (!res || stale || seen.current === res) return
    const first = seen.current === undefined
    seen.current = res
    if (first || (!Object.keys(edits).length && !deletes.size)) return
    const gone = [...Object.keys(edits), ...deletes].filter((k) => !keyed.hasKey || !keyed.byKey.has(k)).length
    if (!keyed.hasKey) { setEdits({}); setDeletes(new Set()) }
    else { setEdits((e) => Object.fromEntries(Object.entries(e).filter(([k]) => keyed.byKey.has(k)))); setDeletes((d) => new Set([...d].filter((k) => keyed.byKey.has(k)))) }
    if (gone) notify('The table was reloaded: pending changes to rows that moved or are gone were dropped')
  }, [res, stale]) // eslint-disable-line react-hooks/exhaustive-deps

  const rows: GridRow[] = useMemo(() => {
    if (!res) return []
    const server = res.cells.map((cells, i): GridRow => ({ key: keyed.keys[i], cells, state: deletes.has(keyed.keys[i]) ? 'deleted' : 'clean' }))
    const fresh = inserts.map((n): GridRow => ({ key: 'n' + n.id, cells: res.columns.map(() => ''), state: 'new' }))
    return [...server, ...fresh]
  }, [res, keyed, deletes, inserts])
  const editedAll: Edits = useMemo(() => {
    const m: Edits = { ...edits }
    for (const n of inserts) m['n' + n.id] = n.vals
    return m
  }, [edits, inserts])

  // every pending change as a diff line, so nothing is sent unseen
  type Mark = { id: string; kind: 'update' | 'delete' | 'insert'; where: string; col?: string; from?: string | null; to?: string | null; detail?: string; revert: () => void }
  const marks: Mark[] = useMemo(() => {
    if (!res) return []
    const pkIdx = res.columns.map((c, i) => (c.pk ? i : -1)).filter((i) => i >= 0)
    const at = (key: string) => {
      const n = rowIdx(key)
      const cells = res.cells[n] ?? []
      return pkIdx.length ? pkIdx.map((i) => `${res.columns[i].name} ${cells[i] ?? 'NULL'}`).join(', ') : `row ${n + 1}`
    }
    const out: Mark[] = []
    for (const key of deletes) out.push({ id: 'd' + key, kind: 'delete', where: at(key), revert: () => setDeletes((s) => { const x = new Set(s); x.delete(key); return x }) })
    for (const [key, set] of Object.entries(edits)) {
      if (deletes.has(key)) continue
      const cells = res.cells[rowIdx(key)] ?? []
      for (const [col, v] of Object.entries(set)) {
        const ci = res.columns.findIndex((c) => c.name === col)
        out.push({
          id: `u${key}.${col}`, kind: 'update', where: at(key), col, from: cells[ci] ?? null, to: show(v, null),
          revert: () => setEdits((e) => { const { [col]: _gone, ...rest } = e[key]; const n = { ...e }; if (Object.keys(rest).length) n[key] = rest; else delete n[key]; return n }),
        })
      }
    }
    for (const n of inserts) {
      const vals = Object.entries(n.vals).map(([c, v]) => `${c} = ${show(v, null) ?? 'NULL'}`)
      out.push({ id: 'n' + n.id, kind: 'insert', where: 'new row', detail: vals.length ? vals.join(', ') : 'column defaults', revert: () => setInserts((l) => l.filter((x) => x.id !== n.id)) })
    }
    return out
  }, [res, keyed, edits, deletes, inserts])

  const onEdit = (key: string, col: string, v: Val) => {
    redo.current = []
    if (key[0] === 'n') setInserts((l) => l.map((n) => ('n' + n.id === key ? { ...n, vals: { ...n.vals, [col]: v } } : n)))
    else setEdits((e) => ({ ...e, [key]: { ...e[key], [col]: v } }))
  }
  const onSort = (col: string, additive: boolean) => change<Sort[]>(setSort)((() => {
    const cur = sort.find((s) => s.Column === col)
    const rest = additive ? sort.filter((s) => s.Column !== col) : []
    if (!cur) return [...rest, { Column: col, Desc: false }]
    return cur.Desc ? rest : [...rest, { Column: col, Desc: true }]
  })())

  const addRow = () => { redo.current = []; const id = nextId.current++; setInserts((l) => [...l, { id, vals: {} }]); setSelected('n' + id) }
  const duplicate = () => {
    if (!selected || !res) return
    const src = rows.find((r) => r.key === selected); if (!src) return
    const pk = new Set(res.columns.filter((c) => c.pk).map((c) => c.name))
    const vals: Record<string, Val> = {}
    res.columns.forEach((c, i) => {
      if (pk.has(c.name)) return
      const v = show(editedAll[src.key]?.[c.name], src.cells[i])
      vals[c.name] = v === null ? { $: 'null' } : v
    })
    const id = nextId.current++; setInserts((l) => [...l, { id, vals }]); setSelected('n' + id)
  }
  const del = () => {
    if (!selected) return
    redo.current = []
    if (selected[0] === 'n') { setInserts((l) => l.filter((n) => 'n' + n.id !== selected)); setSelected(null); return }
    setDeletes((d) => { const x = new Set(d); x.has(selected) ? x.delete(selected) : x.add(selected); return x })
  }

  // the PK values the user saw, so the server rejects a stale snapshot instead of touching another row
  const keyOfRow = (k: string) => {
    const cells = res?.cells[rowIdx(k)] ?? []
    return Object.fromEntries((res?.columns ?? []).flatMap((c, i) => (c.pk ? [[c.name, cells[i] ?? '']] : [])))
  }
  const body = () => ({
    resultId: res?.resultId,
    updates: Object.entries(edits).filter(([k]) => !deletes.has(k)).map(([k, set]) => ({ row: rowIdx(k), set, key: keyOfRow(k) })),
    deletes: [...deletes].map(rowIdx),
    deleteKeys: [...deletes].map(keyOfRow),
    inserts: inserts.map((n) => n.vals),
  })
  const makePlan = async (): Promise<Plan | null> => {
    try { return await api<Plan>('POST', `${base}/plan`, body()) } catch (e) { notify(e instanceof Error ? e.message : String(e)); return null }
  }
  // only the grid's own commit sends the staged edits; a plan from a tool (bulk update, column edit, move column) leaves them staged
  const applied = (own: boolean) => { setPlan(null); if (own) clearPending(); setNonce((n) => n + 1); cols.refetch(); notify(own ? 'Changes sent' : pending > 0 ? 'Change applied. Your staged changes were kept; review them, they were made against the earlier data' : 'Change applied') }
  const previewSql = async () => { const p = await makePlan(); if (p) ownPlan(p) }
  const busy = useRef(false) // a second click while the first is still planning or sending would duplicate inserts
  const commit = async () => {
    if (busy.current) return
    busy.current = true
    try { await send() } finally { busy.current = false }
  }
  const send = async () => {
    const p = await makePlan(); if (!p) return
    if (p.gate.confirm || p.gate.password) { ownPlan(p); return }
    try { await api('POST', `${base}/apply`, { planId: p.planId, confirmed: true }); applied(true) } catch (e) { notify(e instanceof ApiError ? e.message : String(e)) }
  }

  const total = countQ.data
  const from = res && res.cells.length ? offset + 1 : 0
  const to = offset + (res?.cells.length ?? 0)
  const ops = meta.operators[info.caps.Dialect] ?? meta.operators.postgres

  // prefix-key actions, answered only while this tab is the visible one
  const selRow = rows.find((r) => r.key === selected)
  const shown = (r: GridRow) => res!.columns.map((_, i) => show(editedAll[r.key]?.[res!.columns[i].name], r.cells[i]))
  const copyRows = (f: CopyFormat) => { // the selected row, else every row on the page
    if (!res) return
    const list = selRow ? [selRow] : rows.filter((r) => r.state !== 'new')
    copyText(formatRows(f, res.columns, list.map(shown), meta.config.csv, info.caps.Dialect, obj.Name)).then(() => notify(`Copied ${list.length} row${list.length === 1 ? '' : 's'}`), (e) => notify(String(e)))
  }
  const copyCell = () => {
    if (!res || !selRow) return notify('Select a row first')
    const v = shown(selRow)[cc()]
    copyText(v ?? 'NULL').then(() => notify('Copied the cell'), (e) => notify(String(e)))
  }
  const cellValue = (r: GridRow, ci: number) => show(editedAll[r.key]?.[res!.columns[ci].name], r.cells[ci])
  const clipRows = async () => { // clipboard text -> rows of cells, split on tabs only
    try {
      const t = (await navigator.clipboard.readText()).replace(/\r\n/g, '\n').replace(/\n$/, '')
      if (!t) return []
      // tab separated only (what Copy rows produces and spreadsheets paste): a comma in a name must not split it
      return t.split('\n').map((l) => l.split('\t').map((v) => (v === 'NULL' ? null : v)))
    } catch { notify('The browser did not allow reading the clipboard'); return [] }
  }
  const asVal = (v: string | null): Val => (v === null ? { $: 'null' } : v)
  async function pasteRows() {
    if (!res?.editable) return notify('This table is not editable')
    const lines = await clipRows(); if (!lines.length) return
    redo.current = []
    const made = lines.map((cells) => { const id = nextId.current++; return { id, vals: Object.fromEntries(cells.slice(0, res.columns.length).map((v, i) => [res.columns[i].name, asVal(v)])) } })
    setInserts((l) => [...l, ...made]); setSelected('n' + made[made.length - 1].id)
    notify(`Pasted ${made.length} row${made.length === 1 ? '' : 's'}`)
  }
  async function pasteCells() {
    if (!res?.editable || !selRow) return notify(res?.editable ? 'Select a cell first' : 'This table is not editable')
    const lines = await clipRows(); if (!lines.length) return
    const at = rows.findIndex((r) => r.key === selRow.key)
    lines.forEach((cells, dy) => { const r = rows[at + dy]; if (r && r.state !== 'deleted') cells.forEach((v, dx) => { const c = res.columns[cc() + dx]; if (c) onEdit(r.key, c.name, asVal(v)) }) })
  }
  function quickFilter() {
    if (!selRow || !res) return notify('Select a cell first')
    const col = res.columns[cc()].name, v = cellValue(selRow, cc())
    const f: Filter = v === null ? { Column: col, Op: 'IS NULL', Value: '', Enabled: true } : { Column: col, Op: '=', Value: v, Enabled: true }
    setShowBar(true); change<FilterSet>(setFilters)({ Filters: [...filters.Filters.filter((x) => x.Column !== col), f], Or: false })
  }
  function followFk() {
    const fk = cols.data?.find((c) => c.Name === res?.columns[cc()]?.name)?.FK, v = selRow && cellValue(selRow, cc())
    if (!fk || !onFollow) return notify('This column has no foreign key')
    if (v === null || v === undefined) return notify('The value is NULL')
    onFollow({ Schema: fk.RefSchema, Name: fk.RefTable, Kind: 'table' }, { Filters: [{ Column: fk.RefColumns[0], Op: '=', Value: v, Enabled: true }], Or: false })
  }
  // [sidebar] matching picks how the field search matches, like the sidebar search: fuzzy (the best subsequence match) or contains
  const best = (() => {
    const q = fieldQ.trim(); if (!q || !res) return ''
    const contains = meta.config.sidebar.Matching === 'contains'
    let name = '', score = Infinity
    for (const c of res.columns) { const sc = match(q, c.name); if (sc >= 0 && (!contains || sc < 1000) && sc < score) { name = c.name; score = sc } }
    return name
  })()
  const keyActions: Record<string, () => void> = {
    reload_tab: reload, filters: () => setShowBar(!showBar), columns: () => { if (colsMenu.current) colsMenu.current.open = !colsMenu.current.open },
    add_row: addRow, duplicate_rows: duplicate, delete_rows: del, commit, preview: previewSql, discard: clearPending,
    undo: () => { const m = marks[marks.length - 1]; if (m) { redo.current.push({ edits, deletes, inserts }); m.revert() } },
    redo: () => { const s = redo.current.pop(); if (!s) return notify('Nothing to redo'); setEdits(s.edits); setDeletes(s.deletes); setInserts(s.inserts) },
    set_value: () => { if (!res?.editable || !selRow || selRow.state === 'deleted') return notify(res?.editable ? 'Select a row first' : 'This result is not editable'); setDlg('setvalue') },
    paste_rows: pasteRows, paste_cells: pasteCells, quick_filter: quickFilter, follow_fk: followFk,
    page_settings: () => setDlg('page'), sort_columns: () => setDlg('sort'),
    hide_column: () => { const c = res?.columns[cc()]?.name ?? ''; if (visible.length > 1 && c) change<string[]>(setHidden)([...hidden, c]); },
    show_all_columns: () => change<string[]>(setHidden)([]),
    show_ddl: () => setView('ddl'), show_index: () => { setView('structure'); setSection('indexes') }, triggers: () => { setView('structure'); setSection('triggers') },
    show_tree: () => (info.caps.Document ? setView('tree') : notify('The Tree view is for document databases (MongoDB, Redis)')),
    next_page: () => { if (res && res.cells.length >= limit) change<number>(setOffset)(offset + limit) },
    prev_page: () => { if (offset > 0) change<number>(setOffset)(Math.max(0, offset - limit)) },
    first_row: () => rows[0] && setSelected(rows[0].key), last_row: () => rows.length && setSelected(rows[rows.length - 1].key),
    count_exact: () => setExact(true), show_data: () => setView('data'), show_structure: () => setView('structure'),
    show_json: () => info.caps.Document && setView('json'),
    bulk_update: () => { if (!res?.editable) return notify('This table is not editable'); if (!hasActive) return notify('Add a filter first: a bulk update needs one'); setDlg('bulk') },
    toggle_row_detail: () => {
      if (view !== 'structure') return setDetail(!detail)
      const c = cols.data?.find((x) => x.Name === structCol)
      if (!c) return notify('Click a column in the structure first')
      if (obj.Kind !== 'table') return notify('Only a table can be altered')
      setColForm(c)
    },
    export: () => setDlg('export'), import: () => (obj.Kind === 'table' ? setDlg('import') : notify('Only tables can be imported into')),
    copy_rows: () => copyRows('Plain'), copy_rows_as: () => setDlg('copyas'), copy_cells: copyCell,
    quick_look: () => { if (selRow) setCell({ row: selRow.key, col: res!.columns[0].name, readOnly: true }) },
  }
  useEffect(() => {
    if (!active) return
    const h = (e: Event) => { if (e.defaultPrevented) return; const f = keyActions[(e as CustomEvent).detail.action]; if (f) { e.preventDefault(); f() } } // the sidebar answers first when it holds the marked tables
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  })

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="toolbar bg-canvas">
        <button onClick={reload}>Reload</button>
        <button onClick={() => setShowBar(!showBar)}>{showBar ? 'Hide filters' : 'Filters'}{hasActive ? ' (on)' : ''}</button>
        <ColumnsMenu all={colNames} hidden={hidden} onChange={change<string[]>(setHidden)} menuRef={colsMenu} />
        <Select className="bg-raised border-transparent" aria-label="View" value={view} onChange={(v) => setView(v as View)} options={[{ value: 'data', label: 'Data' }, { value: 'structure', label: 'Structure' }, { value: 'ddl', label: 'DDL' }, ...(info.caps.Document ? [{ value: 'json', label: 'JSON' }, { value: 'tree', label: 'Tree' }] : [])]} />
        <button onClick={() => setDlg('export')}>Export</button>
        {view === 'data' && res?.editable && <button onClick={keyActions.bulk_update} title="Set columns on every row the filter matches">Bulk update</button>}
        {obj.Kind === 'table' && <button onClick={() => setDlg('import')}>Import</button>}
        <span className="flex-1 min-w-0" />
        {view === 'data' && res?.editable && !stale && <>
          <button onClick={addRow}>Add row</button>
          <button onClick={duplicate} disabled={!selected}>Duplicate row</button>
          <button onClick={del} disabled={!selected}>{selected && deletes.has(selected) ? 'Keep row' : 'Delete row'}</button>
        </>}
      </div>
      {showBar && view === 'data' && <FilterBar key={JSON.stringify(filters)} sid={sid} obj={obj} value={filters} columns={meta.config.table.FilterColumnSort === 'name' ? [...colNames].sort((a, b) => a.localeCompare(b)) : colNames} operators={ops} onApply={change<FilterSet>(setFilters)} active={active} />}
      {(rowsQ.isFetching || cols.isLoading) && <div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-[color-mix(in_srgb,var(--accent)_20%,transparent)] after:absolute after:inset-0 after:w-[40%] after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Loading rows" />}
      {!res && !rowsQ.error && <div className="p-2.5 text-[12.5px] text-ink-2">Loading rows…</div>}
      {rowsQ.error && <div className="p-2.5 text-[12.5px] text-del">{rowsQ.error.message}</div>}
      {view === 'data' && res && res.cells.length === 0 && inserts.length === 0 && (
        <div className="py-14 px-8 max-w-[46ch] text-ink-2 text-[15px] leading-normal">{hasActive ? 'No rows match these filters.' : res.editable ? 'This table has no rows yet. Add one to start.' : 'This table has no rows yet.'}</div>
      )}
      {view === 'data' && res && (res.cells.length > 0 || inserts.length > 0) && (
        <div className="flex-1 min-h-0 flex">
          <Grid colRef={colRef} columns={res.columns} rows={rows} sort={sort} onSort={onSort} edited={editedAll} editable={res.editable && !stale} onOpen={setCell} pretty={!!meta.config.table.AutoPrettyJSON}
            selected={selected} onSelect={setSelected} onSpace={() => setDetail(!detail)} onHeaderMenu={(col, x, y) => setHmenu({ col, x, y })} />
          {detail && (
            <div className="w-[340px] flex-none border-l border-rule flex flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:inset-0 max-[760px]:z-30 max-[760px]:w-full" role="region" aria-label="Row details" data-ctx="detail" tabIndex={0}>
              <div className="toolbar bg-canvas"><strong className="flex-1 min-w-0 font-medium">Row details</strong><button className="btn-ghost" onClick={() => setDetail(false)} aria-label="Close">✕</button></div>
              <input className="mx-2.5 mt-2 mb-1 bg-panel border-transparent" aria-label="Search for field" placeholder="Search for field…" value={fieldQ} onChange={(e) => setFieldQ(e.target.value)} />
              <div className="overflow-auto flex-1">
                {!selRow && <div className="p-2.5 text-[12.5px] text-ink-2">Select a row to see its fields.</div>}
                {selRow && res.columns.map((c, i) => {
                  const ed = editedAll[selRow.key]?.[c.name], hit = best === c.name
                  const val = show(ed, selRow.cells[i])
                  return (
                    <div key={c.name} ref={hit ? (el) => el?.scrollIntoView({ block: 'nearest' }) : undefined} className={`px-3 py-2 border-b border-rule cursor-pointer hover:bg-panel ${ed !== undefined ? 'shadow-[inset_3px_0_0_var(--edit)]' : ''} ${hit ? 'bg-raised' : ''}`} onClick={() => setCell({ row: selRow.key, col: c.name, readOnly: !res.editable || selRow.state === 'deleted' })}
                      title={res.editable ? 'Click to edit' : undefined}>
                      <div className="text-[12px] text-ink-2 mb-[3px]">{c.pk && <span className="text-accent mr-1">◆</span>}{c.name}<span className="text-ink-3 text-[11px]"> {c.type}</span></div>
                      <div className={`font-mono text-[12px] whitespace-pre-wrap wrap-anywhere max-h-[9em] overflow-hidden ${val === null ? 'text-ink-3' : ''}`}>{val === null ? 'NULL' : meta.config.table.AutoPrettyJSON ? prettyJson(val) : val}</div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      )}
      {cell && res && (() => {
        const r = rows.find((x) => x.key === cell.row); const ci = res.columns.findIndex((c) => c.name === cell.col)
        return r && ci >= 0 ? <CellEditor col={cell.col} readOnly={cell.readOnly} value={show(editedAll[r.key]?.[cell.col], r.cells[ci])}
          onDone={(v) => { setCell(null); if (v !== undefined) onEdit(cell.row, cell.col, v) }} /> : null
      })()}
      {view === 'json' && <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch]">{(res?.docs ?? []).map((d) => { try { return JSON.stringify(JSON.parse(d), null, 2) } catch { return d } }).join('\n')}</pre>}
      {view === 'tree' && <Tree docs={res?.docs ?? []} />}
      {view === 'ddl' && <Ddl sid={sid} refQ={refQ} />}
      {view === 'structure' && <Structure sid={sid} refQ={refQ} caps={info.caps} columns={cols.data ?? []} section={section} obj={obj} total={total} notify={notify} onPlan={toolPlan} selected={structCol} onSelect={setStructCol} onEdit={(c) => setColForm(c)} />}
      {colForm && <ColumnDialog sid={sid} obj={obj} col={colForm} onPlan={toolPlan} onClose={() => setColForm(null)} />}
      {view === 'data' && marks.length > 0 && (
        <div className="border-t border-rule-2 bg-panel flex flex-col max-h-[42%] font-mono motion-safe:animate-rise" role="region" aria-label="Staged changes">
          <div className="flex items-center gap-2 px-2.5 py-2">
            <strong className="font-medium text-[13px]"><b className="inline-block min-w-[22px] px-1.5 mr-1.5 rounded-[2px] bg-edit text-on-accent text-center font-semibold">{marks.length}</b>staged change{marks.length === 1 ? '' : 's'}</strong>
            <span className="flex-1 min-w-0 text-ink-2">Not sent to the database yet.</span>
            <button onClick={() => setTrayOpen(!trayOpen)} aria-expanded={trayOpen}>{trayOpen ? 'Hide list' : 'Show list'}</button>
            <button onClick={previewSql}>Preview SQL</button>
            <button onClick={clearPending}>Discard all</button>
            <button className="btn-primary" onClick={commit}>Send changes</button>
          </div>
          {trayOpen && (
            <div className="overflow-auto border-t border-rule px-2.5 pt-1.5 pb-2.5 text-[11.5px] font-mono">
              {marks.map((m) => (
                <div className="flex gap-2.5 items-baseline py-[3px]" key={m.id}>
                  <span className="flex-1 min-w-0 wrap-anywhere whitespace-nowrap overflow-hidden text-ellipsis">
                    {m.kind === 'insert' && <><span className="text-new font-bold">+ </span>{clip(m.detail ?? '', 160)}</>}
                    {m.kind === 'delete' && <del className="text-del decoration-1">remove {m.where}</del>}
                    {m.kind === 'update' && <><span className="text-ink-2">{m.col} on {m.where}:  </span><del className="text-del decoration-1">{clip(m.from ?? 'NULL', 80)}</del> <ins className="text-edit no-underline">{clip(m.to ?? 'NULL', 80)}</ins></>}
                  </span>
                  <button className="btn-ghost" onClick={m.revert} aria-label={`Undo change to ${m.where}`}>Undo</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {view === 'data' && (
        <div className="toolbar bg-canvas border-b-0! border-t border-rule text-[12px] text-ink-2">
          <button disabled={offset === 0} onClick={() => change<number>(setOffset)(Math.max(0, offset - limit))} aria-label="Previous page">Prev</button>
          <span><span className="font-mono text-ink">{from}–{to}</span>{total ? <> of <span className="font-mono text-ink">{total.exact ? '' : '~'}{total.n}</span></> : ''}</span>
          <button disabled={!res || res.cells.length < limit} onClick={() => change<number>(setOffset)(offset + limit)} aria-label="Next page">Next</button>
          {total && !total.exact && <button className="btn-ghost" onClick={() => setExact(true)}>Count exactly</button>}
          <label className="text-ink-2">Rows per page <input type="number" min={1} style={{ width: 70 }} defaultValue={limit}
            onBlur={(e) => { const n = Number(e.target.value); if (n > 0 && n !== limit) change<number>(setLimit)(n) }} /></label>
          {res?.truncated && <span className="text-ink-2">Stopped at the row limit.</span>}
          {rowsQ.isFetching && <span className="text-ink-2">Loading…</span>}
        </div>
      )}
      {plan && <PlanDialog sid={sid} plan={plan} onClose={() => setPlan(null)} onApplied={() => applied(planOwn)} />}
      {dlg === 'export' && <ExportDialog sid={sid} notify={notify} onClose={() => setDlg('')} source={{ mode: 'table', ref: obj, filters, sort, columns: hidden.length ? visible : [], limit, offset }} />}
      {dlg === 'import' && <ImportDialog sid={sid} table={obj} notify={notify} onClose={() => setDlg('')} onDone={() => setNonce((n) => n + 1)} />}
      {hmenu && <TabMenu x={hmenu.x} y={hmenu.y} onClose={() => setHmenu(null)} items={[
        ...(visible.length > 1 ? [['Hide this column', () => change<string[]>(setHidden)([...hidden, hmenu.col])] as [string, () => void]] : []),
        ...(hidden.length ? [['Show all columns', () => change<string[]>(setHidden)([])] as [string, () => void]] : []),
        ['Sort by several columns…', () => setDlg('sort')]]} />}
      {dlg === 'setvalue' && selRow && res && <SetValueDialog sid={sid} schema={obj.Schema} col={res.columns[cc()].name} dialect={info.caps.Dialect} onClose={() => setDlg('')} onPick={(v) => { setDlg(''); onEdit(selRow.key, res.columns[cc()].name, v) }} />}
      {dlg === 'bulk' && res && <BulkUpdateDialog sid={sid} obj={obj} filters={filters} columns={colNames} onPlan={toolPlan} onClose={() => setDlg('')} />}
      {dlg === 'sort' && <SortDialog columns={colNames} sort={sort} onClose={() => setDlg('')} onApply={(v) => { setDlg(''); change<Sort[]>(setSort)(v) }} />}
      {dlg === 'page' && <PageDialog limit={limit} offset={offset} onClose={() => setDlg('')} onApply={async (l, o) => { setDlg(''); if (await guard()) { clearPending(); setLimit(l); setOffset(o) } }} />}
      {dlg === 'copyas' && <CopyAsDialog onClose={() => setDlg('')} onPick={(f) => { setDlg(''); copyRows(f) }} />}
    </div>
  )
}

function ColumnsMenu({ all, hidden, onChange, menuRef }: { all: string[]; hidden: string[]; onChange: (h: string[]) => void; menuRef?: React.Ref<HTMLDetailsElement> }) {
  return (
    <details ref={menuRef} style={{ position: 'relative' }}>
      <summary className="list-none border border-rule-2 rounded-[2px] font-mono px-[11px] py-1 leading-5 cursor-pointer hover:bg-raised [&::-webkit-details-marker]:hidden">Columns{hidden.length ? ` (${all.length - hidden.length}/${all.length})` : ''}</summary>
      <div className="popover absolute z-20 mt-1 w-60 max-h-[300px] overflow-auto p-2">
        {all.map((c) => (
          <label key={c} className="flex items-center gap-1.5"><input type="checkbox" checked={!hidden.includes(c)} onChange={(e) => onChange(e.target.checked ? hidden.filter((h) => h !== c) : [...hidden, c])} />{c}</label>
        ))}
      </div>
    </details>
  )
}

function Structure({ sid, refQ, caps, columns, section, obj, total, notify, onPlan, selected, onSelect, onEdit }: { sid: string; refQ: string; caps: SessionInfo['caps']; columns: Column[]; section: string; obj: ObjRef; total?: { n: number; exact: boolean }; notify: (m: string) => void; onPlan: (p: Plan) => void; selected: string; onSelect: (n: string) => void; onEdit: (c: Column) => void }) {
  const get = (what: string, on: boolean) => useQuery({ queryKey: ['obj', sid, what, refQ], queryFn: () => api<any[]>('GET', `/sessions/${sid}/object/${what}${refQ}`), enabled: on, gcTime: 0 })
  const idx = get('indexes', caps.Indexes), trg = get('triggers', caps.Triggers)
  // engines with ReorderColumns (MySQL, ClickHouse) can move a column: drag its row or use the arrows; the ALTER is previewed first
  const reorder = !!caps.ReorderColumns && obj.Kind === 'table'
  const dragCol = useRef<number | null>(null)
  const move = async (name: string, to: number) => { try { onPlan(await planMoveColumn(sid, obj, name, to)) } catch (e) { notify(e instanceof Error ? e.message : String(e)) } }
  useEffect(() => { if (section) document.getElementById('sec-' + section)?.scrollIntoView({ block: 'start' }) }, [section, idx.data, trg.data])
  return (
    <div className="px-4 py-3 overflow-auto [&_h4]:mt-4 [&_h4]:mb-1.5 [&_h4:first-child]:mt-0 [&_h4]:text-[13px] [&_h4]:text-ink-2 [&_table]:border-collapse [&_table]:mb-2.5 [&_table]:text-[12px] [&_table]:font-mono [&_:is(th,td)]:text-left [&_:is(th,td)]:py-1 [&_:is(th,td)]:pr-[18px] [&_:is(th,td)]:pl-0 [&_:is(th,td)]:border-b [&_td]:border-rule [&_th]:border-rule-2 [&_th]:font-sans [&_th]:font-medium [&_th]:text-ink-2" data-ctx="structure">
      <h4>Overview</h4>
      <table><tbody>
        <tr><td>Name</td><td>{obj.Schema ? `${obj.Schema}.` : ''}{obj.Name}</td></tr>
        <tr><td>Kind</td><td>{obj.Kind}</td></tr>
        <tr><td>Columns</td><td>{columns.length}</td></tr>
        <tr><td>Primary key</td><td>{columns.filter((c) => c.PK).map((c) => c.Name).join(', ') || 'none'}</td></tr>
        <tr><td>Foreign keys</td><td>{columns.filter((c) => c.FK).length}</td></tr>
        {caps.Indexes && <tr><td>Indexes</td><td>{idx.data?.length ?? '…'}</td></tr>}
        {total && <tr><td>Rows</td><td>{total.exact ? '' : '~'}{total.n}</td></tr>}
      </tbody></table>
      <h4>Columns</h4>
      <table><thead><tr>{reorder && <th aria-label="Order" />}<th>Name</th><th>Type</th><th>Null</th><th>Default</th><th>Key</th><th>Extra</th><th>References</th></tr></thead><tbody>
        {columns.map((c, i) => <tr key={c.Name} aria-selected={selected === c.Name} className={`cursor-pointer ${selected === c.Name ? 'bg-raised' : 'hover:bg-panel'}`} onClick={() => onSelect(c.Name)} onDoubleClick={() => obj.Kind === 'table' && onEdit(c)} title={obj.Kind === 'table' ? 'Click to select, Row detail or a double click edits the column' : undefined} draggable={reorder} onDragStart={() => { dragCol.current = i }} onDragOver={(e) => { if (dragCol.current !== null) e.preventDefault() }}
          onDrop={() => { const from = dragCol.current; dragCol.current = null; if (from !== null && from !== i) move(columns[from].Name, i) }}>
          {reorder && <td className="whitespace-nowrap"><span className="cursor-grab text-ink-3" aria-hidden title="Drag to move this column">⠿</span>
            <button className="btn-ghost px-1" disabled={i === 0} aria-label={`Move ${c.Name} up`} onClick={() => move(c.Name, i - 1)}>▲</button>
            <button className="btn-ghost px-1" disabled={i === columns.length - 1} aria-label={`Move ${c.Name} down`} onClick={() => move(c.Name, i + 1)}>▼</button></td>}<td>{c.Name}</td><td>{c.Type}</td><td>{c.Nullable ? 'yes' : 'no'}</td><td>{c.HasDef ? c.Default : ''}</td><td>{c.PK ? 'PK' : ''}</td><td>{c.Extra}</td>
          <td>{c.FK ? `${c.FK.RefTable}(${c.FK.RefColumns.join(', ')})` : ''}</td></tr>)}
      </tbody></table>
      {caps.Indexes && <><h4 id="sec-indexes">Indexes</h4><table><tbody>{(idx.data ?? []).map((i) => <tr key={i.Name}><td>{i.Name}</td><td>{(i.Columns ?? []).join(', ')}</td><td>{i.Primary ? 'primary' : i.Unique ? 'unique' : ''}</td><td>{i.Algorithm}</td></tr>)}</tbody></table></>}
      {caps.Triggers && <><h4 id="sec-triggers">Triggers</h4><table><tbody>{(trg.data ?? []).map((t) => <tr key={t.Name}><td>{t.Name}</td><td>{t.Timing} {t.Event}</td></tr>)}</tbody></table></>}
    </div>
  )
}

function Ddl({ sid, refQ }: { sid: string; refQ: string }) {
  const q = useQuery({ queryKey: ['ddl', sid, refQ], queryFn: () => api<{ text: string }>('GET', `/sessions/${sid}/object/ddl${refQ}`), gcTime: 0 })
  return <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] whitespace-pre-wrap" data-ctx="structure">{q.error ? q.error.message : q.data?.text ?? 'Loading…'}</pre>
}
