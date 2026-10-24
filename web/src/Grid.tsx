import { useMemo, useRef, useState } from 'react'
import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import { clip, type ResultCol, type Sort, type Val } from './api'
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
}

const ROW_H = 28
const COL_W = 160

export const show = (v: Val | undefined, server: string | null): string | null => {
  if (v === undefined) return server
  if (v === null) return null
  if (typeof v === 'string') return v
  if ('$expr' in v) return v.$expr
  return v.$ === 'null' ? null : v.$ === 'default' ? 'DEFAULT' : ''
}

export function Grid({ columns, rows, sort, onSort, edited, editable, onOpen, selected, onSelect, onSpace, ctx = 'grid' }: Props) {
  const scroller = useRef<HTMLDivElement>(null)
  const [col, setCol] = useState(0) // the cursor column; the cursor row is `selected`
  const defs = useMemo<ColumnDef<GridRow>[]>(
    () => columns.map((c, i) => ({ id: c.name, header: c.name, accessorFn: (r) => r.cells[i] })),
    [columns],
  )
  const table = useReactTable({ data: rows, columns: defs, getCoreRowModel: getCoreRowModel(), manualSorting: true })
  const virt = useVirtualizer({ count: rows.length, getScrollElement: () => scroller.current, estimateSize: () => ROW_H, overscan: 12 })
  const width = 48 + columns.length * COL_W
  const sortOf = (name: string) => sort?.findIndex((s) => s.Column === name) ?? -1

  const open = (row: GridRow, c: number) => onOpen?.({ row: row.key, col: columns[c].name, readOnly: !editable || row.state === 'deleted' })
  const onKey = (e: React.KeyboardEvent) => {
    if (e.ctrlKey || e.metaKey || e.altKey) return
    const idx = rows.findIndex((r) => r.key === selected)
    const go = (r: number, c: number) => {
      const rr = Math.max(0, Math.min(rows.length - 1, r)), cc = Math.max(0, Math.min(columns.length - 1, c))
      if (rows[rr] && rows[rr].key !== selected) onSelect?.(rows[rr].key)
      setCol(cc); virt.scrollToIndex(rr)
      scroller.current?.querySelector<HTMLElement>(`[data-c="${cc}"]`)?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
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
    <div className="grid" ref={scroller} data-ctx={ctx} tabIndex={0} onKeyDown={onKey} role="grid" aria-rowcount={rows.length} aria-colcount={columns.length}>
      <div className="hdr" style={{ width }}>
        <div className="h ln" aria-hidden>#</div>
        {table.getHeaderGroups()[0]?.headers.map((h) => {
          const i = sortOf(h.column.id)
          return (
            <div key={h.id} className="h" style={{ width: COL_W }} title={`${h.column.id} (${columns[h.index]?.type})`}
              onClick={(e) => onSort?.(h.column.id, e.shiftKey)}>
              {columns[h.index]?.pk ? <span className="pk" title="primary key" aria-label="primary key">◆</span> : null}{flexRender(h.column.columnDef.header, h.getContext())}
              {i >= 0 && <span className="dir" aria-label={sort![i].Desc ? 'descending' : 'ascending'}>{sort![i].Desc ? '▼' : '▲'}</span>}
            </div>
          )
        })}
      </div>
      <div style={{ height: virt.getTotalSize(), width, position: 'relative' }}>
        {virt.getVirtualItems().map((v) => {
          const row = rows[v.index]
          const sel = selected === row.key
          return (
            <div key={row.key} className={`r ${row.state === 'deleted' ? 'del' : row.state === 'new' ? 'new' : ''} ${sel ? 'sel' : ''}`}
              style={{ top: v.start, height: ROW_H, width }} role="row">
              <div className="c ln" onClick={() => onSelect?.(row.key)}>{row.state === 'new' ? '+' : v.index + 1}</div>
              {columns.map((c, i) => {
                const ed = edited?.[row.key]?.[c.name]
                const val = show(ed, row.cells[i])
                return (
                  <div key={c.name} data-c={i} role="gridcell" className={`c ${val === null ? 'null' : ''} ${ed !== undefined ? 'edited' : ''} ${sel && col === i ? 'cur' : ''}`} style={{ width: COL_W }}
                    title={clip(ed !== undefined ? `was: ${row.cells[i] ?? 'NULL'}` : (val ?? 'NULL'), 200)}
                    onClick={() => { onSelect?.(row.key); setCol(i) }}
                    onDoubleClick={() => open(row, i)}>
                    {val === null ? 'NULL' : val}
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
