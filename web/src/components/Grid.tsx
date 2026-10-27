import { useEffect, useMemo, useRef, useState } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { clip, type ResultCol, type Sort, type Val } from '../utils/api'
import type { CellTarget } from './CellEditor'

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
  onSelect?: (rowKey: string) => void
  onSpace?: () => void
  ctx?: 'grid' | 'results'
  colRef?: { current: number } // the cursor column, for copy_cells
  onHeaderMenu?: (col: string, x: number, y: number) => void // right-click on a column header
  pretty?: boolean // auto_pretty_json
}

const ROW_H = 28
const COL_W = 160

// one ledger cell: hairline right and bottom rules, clipped to a single line
const CELL = 'px-[9px] h-[28px] leading-[27px] overflow-hidden text-ellipsis whitespace-nowrap flex-none border-r border-b border-rule'
// the sticky row-number column
const LN = 'sticky left-0 z-[2] w-[48px] text-right cursor-pointer pr-[8px] text-[11px]'
const SEL_BG = 'bg-[color-mix(in_srgb,var(--accent)_12%,var(--canvas))]'

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

export function Grid({ columns, rows, sort, onSort, edited, editable, onOpen, selected, onSelect, onSpace, ctx = 'grid', colRef, onHeaderMenu, pretty }: Props) {
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
  const width = 48 + columns.length * COL_W
  const sortOf = (name: string) => sort?.findIndex((s) => s.Column === name) ?? -1

  const open = (row: GridRow, c: number) => columns[c] && onOpen?.({ row: row.key, col: columns[c].name, readOnly: !editable || row.state === 'deleted' })
  const onKey = (e: React.KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey || e.altKey) return
    const idx = rows.findIndex((r) => r.key === selected)
    const go = (r: number, c: number) => {
      const rr = Math.max(0, Math.min(rows.length - 1, r)), cc = Math.max(0, Math.min(columns.length - 1, c))
      if (rows[rr] && rows[rr].key !== selected) onSelect?.(rows[rr].key)
      setCol(cc); virt.scrollToIndex(rr)
      // horizontal: keep the cursor column in view (the sticky row-number column covers the first 48px)
      const el = scroller.current
      if (el) {
        const left = 48 + cc * COL_W
        if (left - 48 < el.scrollLeft) el.scrollLeft = left - 48
        else if (left + COL_W > el.scrollLeft + el.clientWidth) el.scrollLeft = left + COL_W - el.clientWidth
      }
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
      default: return
    }
    e.preventDefault()
  }

  return (
    <div className="flex-1 min-h-0 overflow-auto relative text-[12px] bg-canvas font-mono outline-none" ref={scroller} data-ctx={ctx} tabIndex={0} onKeyDown={onKey} role="grid" aria-rowcount={rows.length} aria-colcount={columns.length}>
      <div className="sticky top-0 z-[3] flex bg-panel border-b border-rule-2" style={{ width }}>
        <div className={`${CELL} ${LN} border-b-0 font-sans font-medium text-ink-3 bg-panel cursor-pointer select-none hover:text-ink hover:bg-raised`} aria-hidden>#</div>
        {table.getHeaderGroups()[0]?.headers.map((h) => {
          const i = sortOf(h.column.id)
          return (
            <div key={h.id} className={`${CELL} border-b-0 font-sans font-medium text-[12px] text-ink-2 cursor-pointer select-none hover:text-ink hover:bg-raised`} style={{ width: COL_W }} title={`${h.column.id} (${columns[h.index]?.type})`}
              onClick={(e) => onSort?.(h.column.id, e.shiftKey)} onContextMenu={(e) => { if (onHeaderMenu) { e.preventDefault(); onHeaderMenu(h.column.id, e.clientX, e.clientY) } }}>
              {columns[h.index]?.pk ? <span className="text-accent mr-1" title="primary key" aria-label="primary key">◆</span> : null}{flexRender(h.column.columnDef.header, h.getContext())}
              {i >= 0 && <span className="text-accent ml-1 text-[10px]" aria-label={sort![i].Desc ? 'descending' : 'ascending'}>{sort![i].Desc ? '▼' : '▲'}{sort!.length > 1 ? i + 1 : ''}</span>}
            </div>
          )
        })}
      </div>
      <div style={{ height: virt.getTotalSize(), width, position: 'relative' }}>
        {virt.getVirtualItems().map((v) => {
          const row = rows[v.index]
          const sel = selected === row.key
          const del = row.state === 'deleted', isNew = row.state === 'new'
          return (
            <div key={row.key} className="group flex absolute left-0"
              style={{ top: v.start, height: ROW_H, width }} role="row">
              <div className={`${CELL} ${LN} ${del ? 'line-through decoration-1' : ''} ${sel ? SEL_BG : 'bg-panel'} ${
                isNew ? 'text-new font-bold shadow-[inset_3px_0_0_var(--new)]' : `${sel ? 'text-accent' : del ? 'text-del' : 'text-ink-3'} ${sel ? 'font-semibold shadow-[inset_3px_0_0_var(--accent)]' : ''}`}`} onClick={() => onSelect?.(row.key)}>{row.state === 'new' ? '+' : v.index + 1}</div>
              {columns.map((c, i) => {
                const ed = edited?.[row.key]?.[c.name]
                const val = show(ed, row.cells[i])
                return (
                  <div key={c.name} data-c={i} role="gridcell" className={`${CELL} ${del ? 'text-del line-through decoration-1' : val === null ? 'text-ink-3' : ''} ${
                      ed !== undefined ? 'bg-[color-mix(in_srgb,var(--edit)_22%,var(--canvas))]' : sel ? SEL_BG : 'group-hover:bg-panel'} ${
                      sel && col === i ? 'shadow-[inset_0_0_0_2px_var(--accent)]' : ed !== undefined ? 'shadow-[inset_0_-2px_0_var(--edit)]' : ''}`} style={{ width: COL_W }}
                    title={clip(ed !== undefined ? `was: ${row.cells[i] ?? 'NULL'}` : (val ?? 'NULL'), 200)}
                    onClick={() => { onSelect?.(row.key); setCol(i) }}
                    onDoubleClick={() => open(row, i)}>
                    {val === null ? 'NULL' : pretty ? prettyCell(val) : val}
                  </div>
                )
              })}
            </div>
          )
        })}
      </div>
    </div>
  )
}
