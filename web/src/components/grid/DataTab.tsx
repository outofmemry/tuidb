import { useEffect, useImperativeHandle, useMemo, useRef, useState, type CSSProperties } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { tableStateBody } from '../../utils/tablestate'
import { api, ApiError, clip, qs, recentKey, type Column, type Filter, type FilterSet, type Meta, type ObjRef, type Plan, type ResultData, type SessionInfo, type Sort, type Val } from '../../utils/api'
import { PlanDialog, confirm } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { FilterBar } from './FilterBar'
import { Grid, show, type GridRow, type Range } from './Grid'
import { RowDetail } from './RowDetail'
import { FindBar, newFind, type FindState } from './GridFind'
import { StructureEditor, type StructureCtl } from './StructureEditor'
import { cellMatches, findNext, replaceIn } from '../../utils/gridfind'
import { CellEditor, type CellTarget } from './CellEditor'
import { CopyAsDialog } from '../query/CopyAsDialog'
import { ExportDialog } from '../tools/ExportDialog'
import { ImportDialog } from '../tools/ImportDialog'
import { PageDialog } from './PageDialog'
import { SetValueDialog } from './SetValueDialog'
import { SortDialog } from './SortDialog'
import { TabMenu } from '../shell/TabMenu'
import { Tree } from './Tree'
import { match } from '../shell/Palette'
import { Layer, MENU, at as place } from '../shell/Layer'
import { BulkUpdateDialog } from '../tools/BulkUpdateDialog'
import { ColumnDialog, planMoveColumn } from '../tools/ColumnDialogs'
import { copyText, formatRows, type CopyFormat } from '../../utils/copy'
import { cellsAs, columnText, rectOf, rectSize } from '../../utils/selection'
import { columnDef, columnTip, listOf, menuItems } from '../../utils/contextmenu'
import { cfg } from '../../utils/config'
import { ItemMenu } from './ItemMenu'

type Edits = Record<string, Record<string, Val>>
type Insert = { id: number; vals: Record<string, Val> }
type View = 'data' | 'structure' | 'json' | 'tree' | 'ddl'
type Pend = { edits: Edits; deletes: Set<string>; inserts: Insert[] }

export function DataTab({ sid, info, meta, obj, notify, active, initFilters, initView, onFollow, onDirty, onFilters }: { sid: string; info: SessionInfo; meta: Meta; obj: ObjRef; notify: (m: string) => void; active?: boolean; initFilters?: FilterSet; initView?: View; onFollow?: (ref: ObjRef, f: FilterSet) => void; onDirty?: (dirty: boolean) => void; onFilters?: (f: FilterSet) => void }) {
  const base = `/sessions/${sid}`
  const refQ = qs({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })
  const stateQ = qs({ conn: info.name, db: info.database, table: recentKey(obj) })

  const [view, setViewNow] = useState<View>(initView ?? 'data')
  const [filters, setFilters] = useState<FilterSet>({ Filters: [], Or: false })
  const [sort, setSort] = useState<Sort[]>([])
  const [hidden, setHidden] = useState<string[]>([])
  const [showBar, setShowBar] = useState(false)
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(info.rowLimit)
  const page = info.rowLimit > 0 ? Math.min(limit, info.rowLimit) : limit // the server never returns more than [general] row_limit rows at once
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
  const [dlg, setDlg] = useState<'' | 'export' | 'import' | 'copyas' | 'cellsas' | 'setvalue' | 'sort' | 'page' | 'bulk'>('')
  const [fieldQ, setFieldQ] = useState('') // row detail: Search for field…
  const [section, setSection] = useState('') // structure section to scroll to (show_index, triggers)
  const [hmenu, setHmenu] = useState<{ x: number; y: number; col: string } | null>(null)
  const [cmenu, setCmenu] = useState<{ x: number; y: number } | null>(null) // the cell / row context menu
  const root = useRef<HTMLDivElement>(null)
  const [more, setMore] = useState<{ x: number; y: number } | null>(null) // the toolbar's ⋯ menu: the less frequent actions
  const [structCol, setStructCol] = useState('') // the structure row the cursor is on; Row detail (space) edits it as a form
  const [colForm, setColForm] = useState<Column | null>(null)
  const [find, setFind] = useState<FindState | null>(null) // the find / replace bar; null = closed
  const [focusCell, setFocusCell] = useState<{ row: string; col: number; n: number } | null>(null)
  const focusN = useRef(0)
  const [cursor, setCursor] = useState<{ col: number; range: Range | null }>({ col: 0, range: null }) // from the grid: the cursor column and the shift-selected rectangle
  const [widths, setWidths] = useState<Record<string, number>>({}) // column name -> width in terminal cells, the unit the TUI saves
  const resized = useRef(false)
  const sortChosen = useRef(false) // [table] default_sort fills `sort` on open; only a sort the user picked is saved
  const setSortChosen = (v: Sort[]) => { sortChosen.current = true; setSort(v) }
  const [expFilters, setExpFilters] = useState<FilterSet | null>(null) // "Export filtered rows": the bar's filters, all rows
  const structCtl: StructureCtl = useRef(null)
  const [structDirty, setStructDirty] = useState(false)
  // leaving the structure view drops the editor's draft, so unsent structure changes are asked about first
  const setView = async (v: View) => { if (v !== view && view === 'structure' && structDirty && !(await confirm('Discard the unsent structure changes?', undefined, 'Discard'))) return; setViewNow(v) }
  useEffect(() => { onFilters?.(filters) }, [filters]) // eslint-disable-line react-hooks/exhaustive-deps
  const noPkTold = useRef(false)
  const redo = useRef<Pend[]>([])
  const colRef = useRef(0)
  const cc = () => Math.max(0, Math.min(colRef.current, (rowsQ.data?.columns.length ?? 1) - 1)) // the cursor column, clamped: hiding it leaves colRef past the end
  const nextId = useRef(1)
  const colsMenu = useRef<{ toggle: () => void }>(null)

  // restore what the TUI (or an earlier visit) remembered for this table
  const cols = useQuery({ queryKey: ['cols', sid, obj], queryFn: () => api<Column[]>('GET', `${base}/object/columns${refQ}`), gcTime: 0 })
  const saved = useQuery({ queryKey: ['tstate', sid, obj], queryFn: () => api<any>('GET', '/state/table' + stateQ), staleTime: Infinity, gcTime: 0 })
  useEffect(() => {
    if (saved.isSuccess && !ready) {
      const s = saved.data
      // [table] default_sort: with nothing saved, the first load sorts by the primary key.
      const dsort = String(cfg(meta, 'table', 'defaultSort', 'none'))
      if (dsort !== 'none' && !(s.sort?.length) && !s.sort_none && !cols.isSuccess && !cols.isError) return // wait for the columns, one fetch instead of two
      const pk = (cols.data ?? []).filter((c) => c.PK).map((c) => ({ Column: c.Name, Desc: dsort === 'pk_desc' }))
      setFilters(initFilters ?? { Filters: s.filters?.Filters ?? [], Or: !!s.filters?.Or }) // a search hit brings its own filters
      setSort(s.sort?.length ? s.sort : dsort !== 'none' && !s.sort_none ? pk : []); setHidden(s.hidden ?? []); setWidths(s.widths ?? {})
      // [table] filter_state: remember (what the table had), show or hide the filter bar on open; a search hit always shows it
      const fs = String(cfg(meta, 'table', 'filterState', 'remember'))
      setShowBar(!!initFilters || (fs === 'show' ? true : fs === 'hide' ? false : !!s.show_filters)); setReady(true)
    }
    if (saved.isError && !ready) setReady(true)
  }, [saved.isSuccess, saved.isError, saved.data, ready, cols.isSuccess, cols.isError])
  useEffect(() => {
    if (!ready) return
    // widths are sent only after a drag: absent, the saved ones (the TUI's too) are kept
    const t = setTimeout(() => api('PUT', '/state/table' + stateQ, tableStateBody({ hidden, filters, showBar, sort, sortChosen: sortChosen.current, widths, widthsChosen: resized.current })).catch(() => {}), 500)
    return () => clearTimeout(t)
  }, [ready, hidden, filters, showBar, sort, widths, stateQ])
  const CELL_PX = 8 // shortcut: a terminal cell is taken as 8px, so widths round-trip with the TUI without a font measurement
  const pxWidths = useMemo(() => Object.fromEntries(Object.entries(widths).map(([k, v]) => [k, v * CELL_PX])), [widths])
  const onResize = (col: string, px: number | null) => { resized.current = true; setWidths((w) => { const n = { ...w }; if (px === null) delete n[col]; else n[col] = Math.max(4, Math.min(120, Math.round(px / CELL_PX))); return n }) }

  const colNames = useMemo(() => (cols.data ?? []).map((c) => c.Name), [cols.data])
  const colTips = useMemo(() => Object.fromEntries((cols.data ?? []).map((c) => [c.Name, columnTip(c)])), [cols.data]) // header tooltips: type, nullable, default, key
  const visible = useMemo(() => colNames.filter((n) => !hidden.includes(n)), [colNames, hidden])
  const hasActive = filters.Filters.some((f) => f.Enabled)
  const nActive = filters.Filters.filter((f) => f.Enabled).length

  const rowsQ = useQuery({
    queryKey: ['rows', sid, obj, filters, sort, hidden, offset, limit, nonce],
    queryFn: ({ signal }) => api<ResultData>('POST', `${base}/rows`, { ref: obj, filters, sort, columns: hidden.length ? visible : [], limit, offset }, signal),
    enabled: ready && cols.isSuccess, placeholderData: keepPreviousData, gcTime: 0,
  })
  // [general] estimate_count_over: above that many rows (by the table's estimate) a filtered count is estimated too, shown with ≈
  const over = Number(cfg(meta, 'general', 'estimateCountOver', 0)) || 0
  const probe = over > 0 && hasActive && !exact
  const baseEst = useQuery({
    queryKey: ['count0', sid, obj, nonce],
    queryFn: () => api<{ n: number; exact: boolean }>('POST', `${base}/count`, { ref: obj, filters: { Filters: [], Or: false }, estimate: true }),
    enabled: ready && cols.isSuccess && probe, gcTime: 0,
  })
  const skipExact = probe && (baseEst.data?.n ?? 0) > over
  const countQ = useQuery({
    queryKey: ['count', sid, obj, filters, nonce, exact, skipExact],
    queryFn: () => api<{ n: number; exact: boolean }>('POST', `${base}/count`, { ref: obj, filters, estimate: !exact && (!hasActive || skipExact) }),
    enabled: ready && cols.isSuccess && (!probe || baseEst.isFetched), gcTime: 0,
  })

  const res = rowsQ.data
  const stale = rowsQ.isPlaceholderData // the previous page stays on screen while reloading, but its server snapshot may be gone
  const pending = Object.keys(edits).length + deletes.size + inserts.length
  useEffect(() => { onDirty?.(pending > 0 || structDirty) }, [pending, structDirty]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => () => onDirty?.(false), []) // eslint-disable-line react-hooks/exhaustive-deps
  const clearPending = () => { setEdits({}); setDeletes(new Set()); setInserts([]); setSelected(null) }
  const guard = async () => pending === 0 || (await confirm('Discard pending changes?', undefined, 'Discard'))
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
  const openCell = (t: CellTarget) => { // a table without a primary key opens read only: say why, once
    if (res && !res.editable && obj.Kind === 'table' && !res.columns.some((c) => c.pk) && !noPkTold.current) { noPkTold.current = true; notify('This table has no primary key, so rows cannot be edited here.') }
    setCell(t)
  }
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
  const onSort = (col: string, additive: boolean) => change<Sort[]>(setSortChosen)((() => {
    const cur = sort.find((s) => s.Column === col)
    const rest = additive ? sort.filter((s) => s.Column !== col) : []
    if (!cur) return [...rest, { Column: col, Desc: false }]
    return cur.Desc ? rest : [...rest, { Column: col, Desc: true }]
  })())

  const addRow = () => { redo.current = []; const id = nextId.current++; setInserts((l) => [...l, { id, vals: {} }]); setSelected('n' + id); setFocusCell({ row: 'n' + id, col: Math.max(0, res?.columns.findIndex((c) => !c.pk) ?? 0), n: ++focusN.current }) } // the grid scrolls to the new row
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
  const range = cursor.range
  // the rows the selection covers (the shift-selected rectangle, else the cursor row): Row detail edits and Set Value apply to all of them
  const selRows = range ? rows.slice(range.r0, range.r1 + 1).filter((r) => r.state !== 'deleted') : selRow && selRow.state !== 'deleted' ? [selRow] : []
  const copyCell = () => {
    if (!res || !selRow) return notify('Select a row first')
    if (range) { // the rectangle, tab separated like Copy rows
      const text = rows.slice(range.r0, range.r1 + 1).map((r) => shown(r).slice(range.c0, range.c1 + 1).map((v) => v ?? 'NULL').join('\t')).join('\n')
      return copyText(text).then(() => notify(`Copied ${(range.r1 - range.r0 + 1) * (range.c1 - range.c0 + 1)} cells`), (e) => notify(String(e)))
    }
    const v = shown(selRow)[cc()]
    copyText(v ?? 'NULL').then(() => notify('Copied the cell'), (e) => notify(String(e)))
  }
  const cellValue = (r: GridRow, ci: number) => show(editedAll[r.key]?.[res!.columns[ci].name], r.cells[ci])
  // copy_cells_as: the selected rectangle (else the cursor cell) in one of the seven formats; copy_column: every value of a column, one per line
  const copyCellsAs = (f: CopyFormat) => {
    if (!res || !selRow) return notify('Select a cell first')
    const rect = rectOf(range, rows.findIndex((r) => r.key === selRow.key), cc())
    copyText(cellsAs(f, rect, res.columns, (r, c) => cellValue(rows[r], c), meta.config.csv, info.caps.Dialect, obj.Name)).then(() => notify(`Copied ${rectSize(rect)} cell${rectSize(rect) === 1 ? '' : 's'} as ${f}`), (e) => notify(String(e)))
  }
  const copyColumn = (name?: string) => {
    if (!res) return
    const ci = name ? res.columns.findIndex((c) => c.name === name) : cc()
    if (ci < 0) return
    copyText(columnText(rows.filter((r) => r.state !== 'new').length, ci, (r, c) => cellValue(rows[r], c))).then(() => notify(`Copied the values of ${res.columns[ci].name}`), (e) => notify(String(e)))
  }
  // column header menu: the entries come from meta.menus.header; col is the header it was opened on
  const filterColumn = (col: string) => {
    setView('data'); setShowBar(true)
    setTimeout(() => window.dispatchEvent(new CustomEvent('webtuidb:filter-column', { detail: { column: col, sid, key: recentKey(obj) } })), 80) // FilterBar presets its blank filter to this column
  }
  const colAct = (action: string, col: string) => {
    const hide = () => { if (visible.length > 1) change<string[]>(setHidden)([...hidden, col]); else notify('At least one column must stay visible') }
    switch (action) {
      case 'sort_asc': return change<Sort[]>(setSortChosen)([{ Column: col, Desc: false }])
      case 'sort_desc': return change<Sort[]>(setSortChosen)([{ Column: col, Desc: true }])
      case 'sort_none': return change<Sort[]>(setSortChosen)(sort.filter((x) => x.Column !== col))
      case 'sort_columns': return setDlg('sort')
      case 'filter_column': return filterColumn(col)
      case 'hide_column': return hide()
      case 'show_all_columns': return change<string[]>(setHidden)([])
      case 'copy_name': return void copyText(col).then(() => notify('Copied the column name'), (e) => notify(String(e)))
      case 'copy_column': return copyColumn(col)
      case 'copy_definition': { const c = cols.data?.find((x) => x.Name === col); return void copyText(c ? columnDef(c) : col).then(() => notify('Copied the column definition'), (e) => notify(String(e))) }
    }
    notify(`${action}: not available here`)
  }
  const headerAt = (col: number) => { // column_menu: open the header menu under the cursor column's header
    const el = root.current?.querySelector<HTMLElement>(`[data-h="${col}"]`)
    const r = el?.getBoundingClientRect()
    if (res?.columns[col]) setHmenu({ col: res.columns[col].name, x: r ? r.left : 40, y: r ? r.bottom : 80 })
  }
  const addColumn = () => {
    if (obj.Kind !== 'table' || info.caps.Editor !== 'sql') return notify('Only an SQL table can get a new column')
    setView('structure'); setTimeout(() => structCtl.current?.add(), 80) // opens the structure editor on a new column
  }
  const nextCell = (from: CellTarget, move: 1 | -1) => { // Tab while editing: save and edit the neighbouring cell
    if (!res) return
    const ri = rows.findIndex((r) => r.key === from.row), ci = res.columns.findIndex((c) => c.name === from.col)
    if (ri < 0 || ci < 0) return
    let r = ri, c = ci + move
    if (c >= res.columns.length) { c = 0; r++ } else if (c < 0) { c = res.columns.length - 1; r-- }
    const row = rows[r]
    if (!row) return
    setSelected(row.key); setFocusCell({ row: row.key, col: c, n: ++focusN.current })
    setCell({ row: row.key, col: res.columns[c].name, readOnly: !res.editable || stale || row.state === 'deleted' })
  }
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
    const at = range ? range.r0 : rows.findIndex((r) => r.key === selRow.key), c0 = range ? range.c0 : cc()
    lines.forEach((cells, dy) => { const r = rows[at + dy]; if (r && r.state !== 'deleted') cells.forEach((v, dx) => { const c = res.columns[c0 + dx]; if (c) onEdit(r.key, c.name, asVal(v)) }) })
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
  // find / replace over the loaded rows. The cursor moves to the match; replace edits through the same pending-change path as typing.
  const colNames2 = res?.columns.map((c) => c.name) ?? []
  const stepFind = (dir: 1 | -1, f = find) => {
    if (!f || !res) return
    if (!f.text) return notify('Type what to find')
    const at = Math.max(0, rows.findIndex((r) => r.key === selected))
    const hit = findNext((r, c) => cellValue(rows[r], c), rows.length, colNames2, f, { r: at, c: cc() }, dir)
    if (!hit) return notify(`"${f.text}" is not in the loaded rows`)
    setSelected(rows[hit.r].key); setFocusCell({ row: rows[hit.r].key, col: hit.c, n: ++focusN.current })
  }
  const replaceCell = (r: GridRow, ci: number, f: FindState) => {
    if (r.state === 'deleted' || !res) return false
    const v = cellValue(r, ci)
    const out = v === null ? null : replaceIn(v, f, f.replace)
    if (out === null) return false
    onEdit(r.key, res.columns[ci].name, out); return true
  }
  const replaceOne = () => {
    if (!find || !res?.editable || !selRow) return
    if (!(find.col === '' || find.col === res.columns[cc()]?.name) || !replaceCell(selRow, cc(), find)) return stepFind(1) // not on a match: move to the next one
    stepFind(1)
  }
  const replaceAll = () => {
    if (!find || !res?.editable) return
    let n = 0, skipped = 0
    for (const r of rows) res.columns.forEach((c, ci) => {
      if (!(find.col === '' || find.col === c.name)) return
      if (find.col === '' && c.pk) { if (r.state !== 'deleted' && cellMatches(cellValue(r, ci), find)) skipped++; return } // Any column leaves primary keys alone
      if (replaceCell(r, ci, find)) n++
    })
    const sk = skipped ? ` (skipped ${skipped} primary-key cell${skipped === 1 ? '' : 's'})` : ''
    notify(n ? `Replaced in ${n} cell${n === 1 ? '' : 's'}${sk}` : skipped ? `Nothing replaced${sk}` : `"${find.text}" is not in the loaded rows`)
  }
  const openFind = () => setFind((f) => f ?? newFind())
  const gridFind = (dir: 0 | 1 | -1) => (dir === 0 || !find ? openFind() : stepFind(dir))
  // [sidebar] matching picks how the field search matches, like the sidebar search: fuzzy (the best subsequence match) or contains
  const best = (() => {
    const q = fieldQ.trim(); if (!q || !res) return ''
    const contains = meta.config.sidebar.Matching === 'contains'
    let name = '', score = Infinity
    for (const c of res.columns) { const sc = match(q, c.name); if (sc >= 0 && (!contains || sc < 1000) && sc < score) { name = c.name; score = sc } }
    return name
  })()
  const keyActions: Record<string, () => void> = {
    reload_tab: reload, filters: () => setShowBar(!showBar), columns: () => colsMenu.current?.toggle(),
    add_row: () => (view === 'structure' ? structCtl.current?.add() : addRow()), duplicate_rows: duplicate, delete_rows: () => (view === 'structure' ? structCtl.current?.del() : del()), commit, preview: previewSql, discard: clearPending,
    undo: () => { const m = marks[marks.length - 1]; if (m) { redo.current.push({ edits, deletes, inserts }); m.revert() } },
    redo: () => { const s = redo.current.pop(); if (!s) return notify('Nothing to redo'); setEdits(s.edits); setDeletes(s.deletes); setInserts(s.inserts) },
    set_value: () => { if (view === 'structure') return structCtl.current?.toggle(); if (!res?.editable || !selRow || selRow.state === 'deleted') return notify(res?.editable ? 'Select a row first' : 'This result is not editable'); setDlg('setvalue') },
    paste_rows: pasteRows, paste_cells: pasteCells, quick_filter: quickFilter, follow_fk: followFk,
    page_settings: () => setDlg('page'), sort_columns: () => setDlg('sort'),
    hide_column: () => { const c = res?.columns[cc()]?.name ?? ''; if (c) colAct('hide_column', c) },
    show_all_columns: () => change<string[]>(setHidden)([]),
    show_ddl: () => setView('ddl'), show_index: () => { setView('structure'); setSection('indexes') }, triggers: () => { setView('structure'); setSection('triggers') },
    show_tree: () => (info.caps.Document ? setView('tree') : notify('The Tree view is for document databases (MongoDB, Redis, DynamoDB)')),
    next_page: () => { if (res && res.cells.length >= page) change<number>(setOffset)(offset + page) },
    prev_page: () => { if (offset > 0) change<number>(setOffset)(Math.max(0, offset - page)) },
    find: () => { if (view === 'data') openFind() }, find_next: () => stepFind(1), find_prev: () => stepFind(-1),
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
    copy_rows: () => copyRows('Plain'), copy_rows_as: () => setDlg('copyas'), copy_cells: copyCell, copy_cells_as: () => (selRow ? setDlg('cellsas') : notify('Select a cell first')), copy_column: () => copyColumn(),
    column_menu: () => { if (view === 'data' && res) headerAt(cc()) },
    quick_look: () => { if (selRow) setCell({ row: selRow.key, col: res!.columns[cc()]?.name ?? res!.columns[0].name, readOnly: true }) },
  }
  useEffect(() => { // webtuidb:cell: a plugin asks for the grid cursor's value (the TUI's Cell)
    if (!active) return
    const h = (e: Event) => { if (selRow && res && view === 'data') (e as CustomEvent).detail.v = shown(selRow)[cc()] ?? '' }
    window.addEventListener('webtuidb:cell', h)
    return () => window.removeEventListener('webtuidb:cell', h)
  })
  useEffect(() => {
    if (!active) return
    const h = (e: Event) => { if (e.defaultPrevented) return; const f = keyActions[(e as CustomEvent).detail.action]; if (f) { e.preventDefault(); f() } } // the sidebar answers first when it holds the marked tables
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  })

  return (
    <div className="flex-1 min-h-0 flex flex-col" ref={root}>
      <div className="toolbar bg-canvas" role="toolbar" aria-label="Table toolbar">
        <button onClick={reload} title="Reload the rows">Reload</button>
        {view === 'data' && <button className={showBar ? 'btn-on' : ''} aria-pressed={showBar} onClick={() => setShowBar(!showBar)} title={showBar ? 'Hide the filter bar' : 'Show the filter bar'}>Filters{nActive > 0 && <span className="ml-1.5 inline-block min-w-[16px] px-1 rounded-[2px] bg-accent text-on-accent text-[10.5px] leading-[15px] text-center font-semibold align-[1px]">{nActive}</span>}</button>}
        {view === 'data' && <ColumnsMenu all={colNames} hidden={hidden} onChange={change<string[]>(setHidden)} ref={colsMenu} />}
        <Select className="bg-raised border-transparent" aria-label="View" value={view} onChange={(v) => setView(v as View)} options={[{ value: 'data', label: 'Data' }, { value: 'structure', label: 'Structure' }, { value: 'ddl', label: 'DDL' }, ...(info.caps.Document ? [{ value: 'json', label: 'JSON' }, { value: 'tree', label: 'Tree' }] : [])]} />
        {view === 'data' && <button onClick={openFind} title="Find in the loaded rows (/)">Find</button>}
        <button onClick={() => { setExpFilters(null); setDlg('export') }} title="Export this table">Export</button>
        <button className="btn-icon text-[15px] leading-none" aria-label="More actions" aria-haspopup="menu" title="Import, bulk update, sorting, paging, copying" onClick={(e) => { const r = e.currentTarget.getBoundingClientRect(); setMore({ x: r.left, y: r.bottom + 2 }) }}>⋯</button>
        <span className="flex-1 min-w-0" />
        {view === 'data' && res?.editable && !stale && <>
          <button onClick={addRow} title="Add a row (saved on Send changes)">Add row</button>
          <button onClick={duplicate} disabled={!selected} title="Duplicate the selected row">Duplicate</button>
          <button onClick={del} disabled={!selected} className={selected && deletes.has(selected) ? '' : 'hover:text-del'} title={selected && deletes.has(selected) ? 'Keep the selected row' : 'Mark the selected row for deletion'}>{selected && deletes.has(selected) ? 'Keep row' : 'Delete row'}</button>
        </>}
        {view === 'data' && res && !res.editable && <span className="chip" title="This result has no usable primary key, or the connection is read only">read only</span>}
      </div>
      {more && <TabMenu x={more.x} y={more.y} onClose={() => setMore(null)} items={([
        view === 'data' && res?.editable ? ['Bulk update…', keyActions.bulk_update] : null,
        obj.Kind === 'table' ? ['Import…', () => setDlg('import')] : null,
        ['-', () => {}],
        ['Sort by several columns…', () => setDlg('sort')],
        ['Row limit and offset…', () => setDlg('page')],
        hidden.length ? ['Show all columns', () => change<string[]>(setHidden)([])] : null,
        ['-', () => {}],
        ['Copy rows', () => copyRows('Plain')],
        ['Copy rows as…', () => setDlg('copyas')],
      ] as ([string, () => void] | null)[]).filter((x): x is [string, () => void] => !!x)} />}
      {showBar && view === 'data' && <FilterBar menu={meta.menus?.filter_bar} key={JSON.stringify(filters)} sid={sid} obj={obj} value={filters} columns={meta.config.table.FilterColumnSort === 'name' ? [...colNames].sort((a, b) => a.localeCompare(b)) : colNames} operators={ops} onApply={change<FilterSet>(setFilters)} onExport={(f) => { setExpFilters(f); setDlg('export') }} active={active} defaults={{ column: cfg<string>(meta, 'table', 'filterDefaultColumn', ''), operator: cfg<string>(meta, 'table', 'filterDefaultOperator', '') }} pk={cols.data?.find((c) => c.PK)?.Name} />}
      {find && view === 'data' && res && <FindBar value={find} columns={colNames2} editable={!!res.editable && !stale} onChange={setFind} onStep={stepFind} onReplace={replaceOne} onReplaceAll={replaceAll} onClose={() => setFind(null)} />}
      {(rowsQ.isFetching || cols.isLoading) && <div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-[color-mix(in_srgb,var(--accent)_20%,transparent)] after:absolute after:inset-0 after:w-[40%] after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Loading rows" />}
      {!res && !rowsQ.error && <div className="p-2.5 text-[12.5px] text-ink-2">Loading rows…</div>}
      {rowsQ.error && <div className="p-2.5 text-[12.5px] text-del">{rowsQ.error.message}</div>}
      {view === 'data' && res && (
        <div className="flex-1 min-h-0 flex">
          <Grid colRef={colRef} columns={res.columns} rows={rows} sort={sort} onSort={onSort} edited={editedAll} editable={res.editable && !stale} onOpen={openCell} pretty={!!meta.config.table.AutoPrettyJSON}
            empty={<div className="sticky left-0 py-14 px-8 max-w-[46ch] text-ink-2 text-[15px] leading-normal font-sans">
              <p className="m-0">{hasActive ? 'No rows match these filters.' : res.editable ? 'This table has no rows yet.' : 'This table has no rows.'}</p>
              <p className="mt-3 mb-0 flex gap-2">{hasActive ? <button onClick={() => change<FilterSet>(setFilters)({ Filters: [], Or: false })}>Clear the filters</button> : res.editable ? <button className="btn-primary" onClick={addRow}>Add a row</button> : null}</p>
            </div>}
            selected={selected} onSelect={setSelected} onSpace={() => setDetail(!detail)} onHeaderMenu={(col, x, y) => setHmenu({ col, x, y })}
            zebra={!!cfg(meta, 'table', 'alternatingRows', false)} spatial={cfg<boolean>(meta, 'table', 'decodeSpatial', true) !== false} colTips={colTips} onCellMenu={(x, y) => setCmenu({ x, y })} onAddRow={() => res.editable && !stale && addRow()} onAddColumn={addColumn}
            widths={pxWidths} onResize={onResize} focusCell={focusCell} onCursor={(col, r) => setCursor((c) => (c.col === col && JSON.stringify(c.range) === JSON.stringify(r) ? c : { col, range: r }))} onFind={gridFind} />
          {detail && <RowDetail columns={res.columns} cells={selRow?.cells ?? []} edited={selRow ? editedAll[selRow.key] : undefined} rows={selRows.length} editable={!!res.editable && !stale && !!selRow && selRow.state !== 'deleted'}
            cursorCol={cc()} fieldQ={fieldQ} onFieldQ={setFieldQ} best={best} pretty={!!meta.config.table.AutoPrettyJSON} hasRow={!!selRow}
            onSet={(col, v) => selRows.forEach((r) => onEdit(r.key, col, v))} onOpen={(col) => selRow && setCell({ row: selRow.key, col, readOnly: !res.editable || selRow.state === 'deleted' })} onClose={() => setDetail(false)} />}
        </div>
      )}
      {cell && res && (() => {
        const r = rows.find((x) => x.key === cell.row); const ci = res.columns.findIndex((c) => c.name === cell.col)
        return r && ci >= 0 ? <CellEditor key={`${cell.row}:${cell.col}`} col={cell.col} readOnly={cell.readOnly} value={show(editedAll[r.key]?.[cell.col], r.cells[ci])}
          imagePreview={cfg<boolean>(meta, 'table', 'imagePreview', true) !== false} decodeSpatial={cfg<boolean>(meta, 'table', 'decodeSpatial', true) !== false}
          onDone={(v, move) => { setCell(null); if (v !== undefined) onEdit(cell.row, cell.col, v); if (move) nextCell(cell, move) }} /> : null
      })()}
      {view === 'json' && <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch]">{(res?.docs ?? []).map((d) => { try { return JSON.stringify(JSON.parse(d), null, 2) } catch { return d } }).join('\n')}</pre>}
      {view === 'tree' && <Tree docs={res?.docs ?? []} />}
      {view === 'ddl' && <Ddl sid={sid} refQ={refQ} />}
      {view === 'structure' && obj.Kind === 'table' && info.caps.Editor === 'sql' && <StructureEditor sid={sid} caps={info.caps} obj={obj} notify={notify} onPlan={toolPlan} columns={cols.data ?? []} total={total} section={section} selected={structCol} onSelect={setStructCol} nonce={nonce} ctl={structCtl} onDirty={setStructDirty} />}
      {view === 'structure' && !(obj.Kind === 'table' && info.caps.Editor === 'sql') && <Structure sid={sid} refQ={refQ} caps={info.caps} columns={cols.data ?? []} section={section} obj={obj} total={total} notify={notify} onPlan={toolPlan} selected={structCol} onSelect={setStructCol} onEdit={(c) => setColForm(c)} />}
      {colForm && <ColumnDialog sid={sid} obj={obj} col={colForm} onPlan={toolPlan} onClose={() => setColForm(null)} />}
      {view === 'data' && marks.length > 0 && (
        <div className="border-t border-rule-2 bg-panel flex flex-col max-h-[42%] font-mono motion-safe:animate-rise" role="region" aria-label="Staged changes">
          <div className="flex items-center gap-2 px-2.5 py-2">
            <strong className="font-medium text-[13px]"><b className="inline-block min-w-[22px] px-1.5 mr-1.5 rounded-[2px] bg-edit text-on-accent text-center font-semibold">{marks.length}</b>staged change{marks.length === 1 ? '' : 's'}</strong>
            <span className="flex-1 min-w-0 text-ink-2 truncate">Nothing is sent until you choose Send changes.</span>
            <button onClick={() => setTrayOpen(!trayOpen)} aria-expanded={trayOpen}>{trayOpen ? 'Hide list' : 'Show list'}</button>
            <button onClick={previewSql}>Preview SQL</button>
            <button onClick={clearPending}>Discard all</button>
            <button className="btn-primary" onClick={commit}>Send changes</button>
          </div>
          {trayOpen && (
            <div className="overflow-auto border-t border-rule px-2.5 pt-1.5 pb-2.5 text-[11.5px] font-mono">
              {marks.map((m) => (
                <div className="flex gap-2.5 items-baseline py-[3px] px-1 -mx-1 rounded-[2px] hover:bg-raised" key={m.id}>
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
          <button disabled={offset === 0} onClick={() => change<number>(setOffset)(Math.max(0, offset - page))} aria-label="Previous page" title="Previous page">‹ Prev</button>
          <span className="tabular-nums"><span className="text-ink">{from.toLocaleString()}–{to.toLocaleString()}</span>{total ? <> of <span className="text-ink" title={total.exact ? 'Exact count' : 'Estimated from the table statistics'}>{total.exact ? '' : '≈'}{total.n.toLocaleString()}</span></> : ''}</span>
          <button disabled={!res || res.cells.length < page} onClick={() => change<number>(setOffset)(offset + page)} aria-label="Next page" title="Next page">Next ›</button>
          {total && !total.exact && <button className="btn-ghost" onClick={() => setExact(true)} title="Run a COUNT(*) instead of the estimate">Count exactly</button>}
          {total && page > 0 && total.n > page && <span className="text-ink-3">page {Math.floor(offset / page) + 1}{total.exact ? ` of ${Math.max(1, Math.ceil(total.n / page)).toLocaleString()}` : ''}</span>}
          {limit > page && <span className="text-ink-3" title="Set by [general] row_limit in the config">capped at {page.toLocaleString()} per page by row_limit</span>}
          <span className="flex-1 min-w-0" />
          {res?.editable && !stale && marks.length === 0 && !!res.cells.length && <span className="text-ink-3 max-[900px]:hidden" title="Double-click a cell, or select it and press Enter, to change its value. Changes are staged until you send them.">Double-click a cell to edit it</span>}
          {res?.truncated && <span className="chip border-edit text-edit">stopped at the row limit</span>}
          {rowsQ.isFetching && <span className="text-ink-3">Loading…</span>}
          <label className="flex items-center gap-1.5 text-ink-2">Rows per page <input className="w-[72px] text-right" type="number" min={1} aria-label="Rows per page" defaultValue={limit} key={limit}
            onKeyDown={(e) => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur() }} onBlur={(e) => {
              const n = Number(e.target.value)
              if (!Number.isInteger(n) || n < 1) { notify('Rows per page must be a whole number of 1 or more.'); e.target.value = String(limit); return }
              if (n !== limit) change<number>(setLimit)(n)
            }} /></label>
        </div>
      )}
      {plan && <PlanDialog sid={sid} plan={plan} onClose={() => setPlan(null)} onApplied={() => applied(planOwn)} />}
      {dlg === 'export' && <ExportDialog sid={sid} notify={notify} onClose={() => setDlg('')} source={{ mode: 'table', ref: obj, filters: expFilters ?? filters, sort, columns: hidden.length ? visible : [], limit: expFilters ? 0 : limit, offset: expFilters ? 0 : offset }} />}
      {dlg === 'import' && <ImportDialog sid={sid} table={obj} notify={notify} onClose={() => setDlg('')} onDone={() => setNonce((n) => n + 1)} />}
      {hmenu && <ItemMenu x={hmenu.x} y={hmenu.y} label={`Column ${hmenu.col}`} onClose={() => setHmenu(null)} items={menuItems(listOf(meta.menus, 'header'), {}).filter((it) => it.action !== 'show_all_columns' || hidden.length > 0)} onPick={(it) => colAct(it.action, hmenu.col)} />}
      {cmenu && <ItemMenu x={cmenu.x} y={cmenu.y} label="Cell menu" onClose={() => setCmenu(null)} items={menuItems(listOf(meta.menus, 'grid'), { editable: !!res?.editable && !stale })} onPick={(it) => { const f = keyActions[it.action]; if (f) f(); else notify(`${it.label}: not available here`) }} />}
      {dlg === 'setvalue' && selRow && res && <SetValueDialog sid={sid} schema={obj.Schema} col={res.columns[cc()].name} dialect={info.caps.Dialect} onClose={() => setDlg('')} onPick={(v) => { setDlg(''); onEdit(selRow.key, res.columns[cc()].name, v) }} />}
      {dlg === 'bulk' && res && <BulkUpdateDialog sid={sid} obj={obj} filters={filters} columns={colNames} onPlan={toolPlan} onClose={() => setDlg('')} />}
      {dlg === 'sort' && <SortDialog columns={colNames} sort={sort} onClose={() => setDlg('')} onApply={(v) => { setDlg(''); change<Sort[]>(setSortChosen)(v) }} />}
      {dlg === 'page' && <PageDialog limit={limit} offset={offset} onClose={() => setDlg('')} onApply={async (l, o) => { setDlg(''); if (await guard()) { clearPending(); setLimit(l); setOffset(o) } }} />}
      {dlg === 'copyas' && <CopyAsDialog onClose={() => setDlg('')} onPick={(f) => { setDlg(''); copyRows(f) }} />}
      {dlg === 'cellsas' && <CopyAsDialog onClose={() => setDlg('')} onPick={(f) => { setDlg(''); copyCellsAs(f) }} />}
    </div>
  )
}

// ColumnsMenu picks the columns to show. It is a popover in the shared Layer, so it closes on an outside click and Escape like every menu.
function ColumnsMenu({ all, hidden, onChange, ref }: { all: string[]; hidden: string[]; onChange: (h: string[]) => void; ref?: React.Ref<{ toggle: () => void }> }) {
  const [q, setQ] = useState('')
  const [open, setOpen] = useState<CSSProperties | null>(null)
  const btn = useRef<HTMLButtonElement>(null)
  const toggle = () => { const r = btn.current?.getBoundingClientRect(); setOpen((o) => (o || !r ? null : place(r, { width: 256, height: 380 }))) }
  useImperativeHandle(ref, () => ({ toggle }))
  const shown = q ? all.filter((c) => c.toLowerCase().includes(q.toLowerCase())) : all
  return (
    <>
      <button ref={btn} className={hidden.length ? 'border-accent/60' : ''} aria-haspopup="dialog" aria-expanded={!!open} title="Choose the columns to show" onClick={toggle}>Columns{hidden.length ? <span className="text-ink-2"> {all.length - hidden.length}/{all.length}</span> : ''}</button>
      {open && <Layer onClose={() => setOpen(null)}>
      <div className={`${MENU} w-64 p-2 flex flex-col gap-1`} style={open} role="dialog" aria-label="Columns to show" onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); setOpen(null); btn.current?.focus() } }}>
        {all.length > 8 ? <input autoFocus className="w-full bg-canvas" placeholder="Find a column" aria-label="Find a column" value={q} onChange={(e) => setQ(e.target.value)} /> : null}
        <div className="flex gap-1 text-[11.5px]"><button className="btn-ghost" onClick={() => onChange([])} disabled={!hidden.length}>Show all</button><button className="btn-ghost" onClick={() => onChange(all.slice(1))} disabled={all.length < 2} title="Keep only the first column">Hide all</button></div>
        <div className="max-h-[300px] overflow-auto">
          {shown.map((c) => (
            <label key={c} className="flex items-center gap-2 px-1 py-0.5 rounded-[2px] hover:bg-raised cursor-pointer"><input type="checkbox" checked={!hidden.includes(c)} disabled={!hidden.includes(c) && all.length - hidden.length <= 1} onChange={(e) => onChange(e.target.checked ? hidden.filter((h) => h !== c) : [...hidden, c])} /><span className="truncate">{c}</span></label>
          ))}
          {!shown.length && <div className="px-1 py-1 text-ink-2">No column matches.</div>}
        </div>
        <div className="text-[11.5px] text-ink-3 px-1">Hidden columns stay hidden next time you open this table.</div>
      </div>
      </Layer>}
    </>
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
  return <pre className={`flex-1 overflow-auto m-0 px-4 py-3 text-[12px] leading-[1.5] whitespace-pre-wrap ${q.error ? 'text-del' : ''}`} data-ctx="structure">{q.error ? q.error.message : q.data?.text ?? 'Loading…'}</pre>
}
