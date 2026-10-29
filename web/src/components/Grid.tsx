import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { clip, type ResultCol, type Sort, type Val } from '../utils/api'
import type { CellTarget } from './CellEditor'
import { cellWkt } from '../utils/m15_wkb'

export type GridRow = { key: string; cells: (string | null)[]; state: 'clean' | 'new' | 'deleted' }

type Props = {
  columns: ResultCol[]
  rows: GridRow[]
  sort?: Sort[]
  onSort?: (col: string, additive: boolean) => void
  // editing is on when editable is set; edited maps rowKey -> colName -> value shown instead of the server's
  edited?: Record<string, Record<string, Val>>
  editable?: boolean
  // Enter or a double-click opens the cell in the popover; the owner renders it
  onOpen?: (t: CellTarget) => void
  selected?: string | null
  onSelect?: (rowKey: string | null) => void // null: Esc deselected the row
  onSpace?: () => void
  ctx?: 'grid' | 'results'
  colRef?: { current: number } // the cursor column, for copy_cells
  onHeaderMenu?: (col: string, x: number, y: number) => void // right-click on a column header
  pretty?: boolean // auto_pretty_json
  widths?: Record<string, number> // column name -> px (the owner converts from the saved cells)
  onResize?: (col: string, px: number | null) => void // a header edge was dragged; null = double-click, back to the default
  focusCell?: { row: string; col: number; n: number } | null // move the cursor here (find); n changes on every request
  onCursor?: (col: number, range: Range | null) => void // the cursor column and the shift-selected rectangle changed
  onFind?: (dir: 0 | 1 | -1) => void // / opens find, n / N step through the matches
  zebra?: boolean // [table] alternating_rows
  spatial?: boolean // [table] decode_spatial: (E)WKB cells show as WKT
  colTips?: Record<string, string> // column name -> header tooltip (type, nullable, default, key)
  onCellMenu?: (x: number, y: number) => void // right-click on a cell, Shift+F10 or the ContextMenu key
  onAddRow?: () => void // double-click in the empty space below the last row
  onAddColumn?: () => void // double-click in the empty space beside the last column header
  empty?: ReactNode // shown under the headers when there are no rows
}

// A rectangle of cells, as indexes into rows and columns (inclusive).
export type Range = { r0: number; r1: number; c0: number; c1: number }

const ROW_H = 28
const COL_W = 160
const MIN_W = 40, MAX_W = 960
const AUTO_MIN = 64, AUTO_MAX = 360, SAMPLE = 200

// numeric columns sit flush right, like a ledger; the type names are the drivers' own
const NUMERIC = /^(small|big|tiny|medium|u)?int|^int\d|numeric|decimal|float|double|real|money|serial|^number|^dec\b|^fixed|^uint/i
export const isNumeric = (type: string) => NUMERIC.test(type)

// one character of the data face, measured once (Martian Mono at 80% width is narrower than a generic monospace guess)
let charW = 0
const charWidth = () => {
  if (charW) return charW
  if (typeof document === 'undefined') return 7
  const el = document.createElement('span')
  el.className = 'font-mono text-[12px]'; el.style.cssText = 'position:absolute;visibility:hidden;white-space:pre'; el.textContent = '0'.repeat(20)
  document.body.appendChild(el); charW = el.getBoundingClientRect().width / 20 || 7; el.remove()
  return charW
}
// a column's starting width: wide enough for its name and the longest of the first rows, within limits
const autoWidth = (c: ResultCol, i: number, rows: GridRow[]) => {
  let n = c.name.length + (c.pk ? 2 : 0) + 1
  for (const r of rows.slice(0, SAMPLE)) { const v = r.cells[i]; const l = v === null ? 4 : v.length; if (l > n) n = l; if (n * charWidth() + 20 >= AUTO_MAX) break }
  return Math.max(AUTO_MIN, Math.min(AUTO_MAX, Math.round(n * charWidth()) + 20))
}

// one ledger cell: hairline right and bottom rules, clipped to a single line
const CELL = 'px-[9px] h-[28px] leading-[27px] overflow-hidden text-ellipsis whitespace-nowrap flex-none border-r border-b border-[color:var(--grid-line,var(--rule))]'
// the sticky row-number column
const LN = 'sticky left-0 z-[2] w-[48px] text-right cursor-pointer pr-[8px] text-[11px]'
const SEL_BG = 'bg-[var(--selection,color-mix(in_srgb,var(--accent)_12%,var(--canvas)))]'
const MOD_BG = 'bg-[var(--modified-row,color-mix(in_srgb,var(--edit)_22%,var(--canvas)))]'
const NEW_BG = 'bg-[var(--new-row,transparent)]'
const DEL_BG = 'bg-[var(--deleted-row,transparent)]'
const ZEBRA = 'bg-[color-mix(in_srgb,var(--ink)_3%,var(--canvas))]'
const CUR = 'shadow-[inset_0_0_0_2px_var(--sel-cell,var(--cell-selection,var(--accent)))]'

// [table] auto_pretty_json: a JSON value shows with spaces after its separators; in a grid cell only when that still fits the column.
export function prettyJson(v: string, indent = 2): string {
  const t = v.trim()
  if (t.length < 2 || (t[0] !== '{' && t[0] !== '[')) return v
  try { return JSON.stringify(JSON.parse(t), null, indent) } catch { return v }
}
const prettyCell = (v: string) => { const one = prettyJson(v, 1).replace(/\n\s*/g, ' '); return one.length <= 20 ? one : v }

export const show = (v: Val | undefined, server: string | null): string | null => {
  if (v === undefined) return server
  if (v === null) return null
  if (typeof v === 'string') return v
  if ('$expr' in v) return v.$expr
  return v.$ === 'null' ? null : v.$ === 'default' ? 'DEFAULT' : ''
}

// what a cell shows besides its text: NULL, the empty string and a pending DEFAULT are dim italic marks (M15.26)
const specialOf = (v: Val | undefined, shown: string | null, isNew: boolean): '' | 'null' | 'empty' | 'default' =>
  shown === null ? 'null' : v !== undefined && typeof v === 'object' && v !== null && '$' in v ? (v.$ === 'default' ? 'default' : v.$ === 'empty' ? 'empty' : '') : isNew && v === undefined ? 'default' : shown === '' && !isNew ? 'empty' : ''

export function Grid({ columns, rows, sort, onSort, edited, editable, onOpen, selected, onSelect, onSpace, ctx = 'grid', colRef, onHeaderMenu, pretty, widths, onResize, focusCell, onCursor, onFind, zebra, spatial, colTips, onCellMenu, onAddRow, onAddColumn, empty }: Props) {
  const scroller = useRef<HTMLDivElement>(null)
  const [col, setColState] = useState(0) // the cursor column; the cursor row is `selected`
  const setCol = (c: number) => { setColState(c); if (colRef) colRef.current = c }
  // hiding the cursor column leaves the cursor past the end: pull it back so reads of columns[col] stay valid
  useEffect(() => {
    if (columns.length && col > columns.length - 1) setCol(columns.length - 1)
    else if (colRef && colRef.current > Math.max(0, columns.length - 1)) colRef.current = Math.max(0, columns.length - 1)
  }, [columns.length]) // eslint-disable-line react-hooks/exhaustive-deps
  const defs = useMemo<ColumnDef<GridRow>[]>(
    () => columns.map((c, i) => ({ id: c.name, header: c.name, accessorFn: (r) => r.cells[i] })),
    [columns],
  )
  const table = useReactTable({ data: rows, columns: defs, getCoreRowModel: getCoreRowModel(), manualSorting: true })
  const virt = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => ROW_H, overscan: 12 })
  const [drag, setDrag] = useState<{ name: string; px: number } | null>(null)
  // widths: a drag wins, then a saved width, then one sized from the first rows (computed once per column set, so paging does not shift the columns)
  const auto = useMemo(() => columns.map((c, i) => autoWidth(c, i, rows)), [columns]) // eslint-disable-line react-hooks/exhaustive-deps
  const colW = columns.map((c, i) => (drag?.name === c.name ? drag.px : widths?.[c.name] ?? auto[i] ?? COL_W))
  const numeric = useMemo(() => columns.map((c) => isNumeric(c.type)), [columns])
  const lefts = colW.reduce<number[]>((a, _w, i) => (a.push(i ? a[i - 1] + colW[i - 1] : 0), a), [])
  const width = 48 + colW.reduce((a, b) => a + b, 0)
  const startResize = (e: React.MouseEvent, name: string, from: number) => {
    e.preventDefault(); e.stopPropagation()
    const x0 = e.clientX
    let px = from
    const move = (m: MouseEvent) => { px = Math.max(MIN_W, Math.min(MAX_W, from + m.clientX - x0)); setDrag({ name, px }) }
    const up = () => { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); setDrag(null); if (px !== from) onResize?.(name, px) }
    window.addEventListener('mousemove', move); window.addEventListener('mouseup', up)
  }
  // shift-selected rectangle: the anchor is where the shift movement started, the other corner is the cursor
  const [anchor, setAnchor] = useState<{ r: number; c: number } | null>(null)
  const curRow = rows.findIndex((r) => r.key === selected)
  const range: Range | null = anchor && curRow >= 0 && (anchor.r !== curRow || anchor.c !== col)
    ? { r0: Math.min(anchor.r, curRow), r1: Math.max(anchor.r, curRow), c0: Math.min(anchor.c, col), c1: Math.max(anchor.c, col) } : null
  const rangeKey = range ? `${range.r0},${range.r1},${range.c0},${range.c1}` : ''
  useEffect(() => { onCursor?.(col, range) }, [col, rangeKey]) // eslint-disable-line react-hooks/exhaustive-deps
  // a sort, filter or refresh re-orders the rows under the anchor's index, so the range would cover other rows
  const order = rows.map((r) => r.key).join('\x00')
  useEffect(() => { setAnchor(null) }, [order, columns.length])
  const reveal = (cc: number) => { // keep column cc in view (the sticky row-number column covers the first 48px)
    const el = scroller.current
    if (!el) return
    const left = lefts[cc] ?? 0, w = colW[cc] ?? COL_W
    if (left < el.scrollLeft) el.scrollLeft = left
    else if (left + w > el.scrollLeft + el.clientWidth - 48) el.scrollLeft = left + w - el.clientWidth + 48
  }
  useEffect(() => {
    if (!focusCell) return
    const r = rows.findIndex((x) => x.key === focusCell.row)
    if (r < 0) return
    setAnchor(null); setCol(focusCell.col); virt.scrollToIndex(r); reveal(focusCell.col)
  }, [focusCell?.n]) // eslint-disable-line react-hooks/exhaustive-deps
  const inRange = (r: number, c: number) => !!range && r >= range.r0 && r <= range.r1 && c >= range.c0 && c <= range.c1
  const sortOf = (name: string) => sort?.findIndex((s) => s.Column === name) ?? -1

  // the cell menu opens at the cursor cell for Shift+F10 / the ContextMenu key
  const menuAt = () => {
    const el = scroller.current?.querySelector<HTMLElement>(`[data-rk="${CSS.escape(selected ?? '')}"] [data-c="${col}"]`) ?? scroller.current
    const r = el?.getBoundingClientRect()
    if (r) onCellMenu?.(r.left + 12, r.bottom - 4)
  }
  const emptyDbl = (e: React.MouseEvent) => { // the empty space under the rows adds a row, beside the headers a column
    if (e.target !== e.currentTarget && !(e.target as HTMLElement).dataset.fill) return
    const top = scroller.current?.getBoundingClientRect().top ?? 0
    if (e.clientY - top <= ROW_H + 2) onAddColumn?.(); else onAddRow?.()
  }
  const open = (row: GridRow, c: number, ro = false) => columns[c] && onOpen?.({ row: row.key, col: columns[c].name, readOnly: ro || !editable || row.state === 'deleted' })
  const onKey = (e: React.KeyboardEvent) => {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return // defaultPrevented: the keymap already dispatched it
    const idx = curRow
    const go = (r: number, c: number) => {
      const rr = Math.max(0, Math.min(rows.length - 1, r)), cc = Math.max(0, Math.min(columns.length - 1, c))
      if (e.shiftKey) { if (!anchor && idx >= 0) setAnchor({ r: idx, c: col }) } else setAnchor(null)
      if (rows[rr] && rows[rr].key !== selected) onSelect?.(rows[rr].key)
      setCol(cc); virt.scrollToIndex(rr); reveal(cc)
    }
    switch (e.key) {
      case 'ArrowDown': case 'j': go(idx < 0 ? 0 : idx + 1, col); break
      case 'ArrowUp': case 'k': go(idx < 0 ? 0 : idx - 1, col); break
      case 'ArrowRight': case 'l': go(idx < 0 ? 0 : idx, col + 1); break
      case 'ArrowLeft': case 'h': go(idx < 0 ? 0 : idx, col - 1); break
      case 'Home': go(idx < 0 ? 0 : idx, 0); break
      case 'End': go(idx < 0 ? 0 : idx, columns.length - 1); break
      case 'PageDown': go(idx + 20, col); break
      case 'PageUp': go(idx - 20, col); break
      case 'Enter': if (idx >= 0) open(rows[idx], col); break
      case ' ': onSpace?.(); break
      case 'Escape': if (anchor) setAnchor(null); else if (selected) onSelect?.(null); else return; break
      case 'ContextMenu': menuAt(); break
      case 'F10': if (!e.shiftKey) return; menuAt(); break
      case '/': if (!onFind) return; onFind(0); break
      case 'n': if (!onFind) return; onFind(1); break
      case 'N': if (!onFind) return; onFind(-1); break
      default: return
    }
    e.preventDefault()
  }

  return (
    <div className="flex-1 min-h-0 overflow-auto relative text-[12px] bg-canvas font-mono outline-none focus-visible:shadow-[inset_0_0_0_1px_var(--accent)]" ref={scroller} data-ctx={ctx} tabIndex={0} onKeyDown={onKey} onDoubleClick={emptyDbl} role="grid" aria-label={ctx === 'results' ? 'Query result' : 'Table data'} aria-rowcount={rows.length} aria-colcount={columns.length}>
      <div className="sticky top-0 z-[3] flex bg-panel border-b border-rule-2" style={{ width }}>
        <div className={`${CELL} ${LN} border-b-0 font-sans font-medium text-ink-3 bg-panel cursor-pointer select-none hover:text-ink hover:bg-raised`} aria-hidden>#</div>
        {table.getHeaderGroups()[0]?.headers.map((h) => {
          const i = sortOf(h.column.id)
          return (
            <div key={h.id} data-h={h.index} role="columnheader" aria-haspopup={onHeaderMenu ? 'menu' : undefined} className={`${CELL} relative border-b-0 font-sans font-medium text-[12px] text-ink-2 cursor-pointer select-none hover:text-ink hover:bg-raised ${numeric[h.index] ? 'text-right' : ''} ${i >= 0 ? 'text-ink' : ''}`} style={{ width: colW[h.index] }} title={`${colTips?.[h.column.id] ?? `${h.column.id} (${columns[h.index]?.type})`}${onSort ? '\nClick to sort, shift-click to add a column' : ''}`}
              onClick={(e) => onSort?.(h.column.id, e.shiftKey)} onContextMenu={(e) => { if (onHeaderMenu) { e.preventDefault(); onHeaderMenu(h.column.id, e.clientX, e.clientY) } }}>
              {columns[h.index]?.pk ? <span className="text-accent mr-1" title="primary key" aria-label="primary key">◆</span> : null}{flexRender(h.column.columnDef.header, h.getContext())}
              {i >= 0 && <span className="text-accent ml-1 text-[10px]" aria-label={sort![i].Desc ? 'descending' : 'ascending'}>{sort![i].Desc ? '▼' : '▲'}{sort!.length > 1 ? i + 1 : ''}</span>}
              {onResize && <span role="separator" aria-orientation="vertical" aria-label={`Resize ${h.column.id}`} className="absolute right-0 top-0 h-full w-[6px] cursor-col-resize hover:bg-accent/40" onMouseDown={(e) => startResize(e, h.column.id, colW[h.index])} onClick={(e) => e.stopPropagation()} onDoubleClick={(e) => { e.stopPropagation(); onResize(h.column.id, null) }} />}
            </div>
          )
        })}
      </div>
      <div style={{ height: virt.getTotalSize(), width, position: 'relative' }}>
        {virt.getVirtualItems().map((v) => {
          const row = rows[v.index]
          const sel = selected === row.key
          const del = row.state === 'deleted', isNew = row.state === 'new'
          const band = zebra && v.index % 2 === 1 ? ZEBRA : ''
          return (
            <div key={row.key} data-rk={row.key} className="group flex absolute left-0"
              style={{ top: v.start, height: ROW_H, width }} role="row" aria-selected={sel}>
              <div className={`${CELL} ${LN} ${del ? 'line-through decoration-1' : ''} ${sel ? SEL_BG : 'bg-panel'} ${
                isNew ? 'text-new font-bold shadow-[inset_3px_0_0_var(--new)]' : `${sel ? 'text-accent' : del ? 'text-del' : 'text-ink-3 group-hover:text-ink-2'} ${sel ? 'font-semibold shadow-[inset_3px_0_0_var(--accent)]' : ''}`}`} onClick={() => onSelect?.(row.key)}>{row.state === 'new' ? '+' : v.index + 1}</div>
              {columns.map((c, i) => {
                const ed = edited?.[row.key]?.[c.name]
                const val = show(ed, row.cells[i])
                const kind = specialOf(ed, val, isNew)
                const wkt = spatial && val ? cellWkt(val) : null
                return (
                  <div key={c.name} data-c={i} role="gridcell" aria-selected={sel && col === i} className={`${CELL} ${numeric[i] ? 'text-right' : ''} ${del ? `text-del line-through decoration-1 ${DEL_BG}` : kind ? 'text-ink-3 italic' : ''} ${
                      ed !== undefined ? MOD_BG : sel || inRange(v.index, i) ? SEL_BG : isNew ? NEW_BG : band || 'group-hover:bg-panel'} ${
                      sel && col === i ? CUR : ed !== undefined ? 'shadow-[inset_0_-2px_0_var(--edit)]' : ''}`} style={{ width: colW[i] }}
                    title={clip(ed !== undefined ? `was: ${row.cells[i] ?? 'NULL'}` : (wkt ?? val ?? 'NULL'), 200)}
                    onClick={(e) => {
                      if (e.ctrlKey || e.metaKey) { onSelect?.(row.key); setCol(i); return open(row, i, true) } // ctrl+click: quick look (read only)
                      if (e.shiftKey && curRow >= 0) { if (!anchor) setAnchor({ r: curRow, c: col }) } else setAnchor(null); onSelect?.(row.key); setCol(i)
                    }}
                    onAuxClick={(e) => { if (e.button === 1) { e.preventDefault(); onSelect?.(row.key); setCol(i); open(row, i, true) } }} // middle-click: quick look
                    onMouseDown={(e) => { if (e.button === 1) e.preventDefault() }}
                    onContextMenu={(e) => { if (!onCellMenu) return; e.preventDefault(); if (!inRange(v.index, i)) { setAnchor(null); onSelect?.(row.key); setCol(i) } onCellMenu(e.clientX, e.clientY) }}
                    onDoubleClick={(e) => { e.stopPropagation(); open(row, i) }}>
                    {kind === 'null' ? 'NULL' : kind === 'default' ? 'DEFAULT' : kind === 'empty' ? "''" : wkt ?? (pretty ? prettyCell(val!) : val)}
                  </div>
                )
              })}
            </div>
          )
        })}
      </div>
      {!rows.length && empty}
      <div data-fill="1" style={{ height: 40, width }} aria-hidden />
    </div>
  )
}
