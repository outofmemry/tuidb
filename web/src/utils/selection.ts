// Copy the selected cell rectangle in any of the seven "Copy as" formats, and a whole column.
// @ts-ignore: node --test (web/test) needs the .ts extension that tsc refuses
import { formatRows, type CopyFormat } from './copy.ts'
import type { Meta, ResultCol } from './api'

export type Rect = { r0: number; r1: number; c0: number; c1: number }
// the shift-selected rectangle, else the one cursor cell
export const rectOf = (range: Rect | null | undefined, row: number, col: number): Rect => range ?? { r0: row, r1: row, c0: col, c1: col }
export const rectSize = (r: Rect) => (r.r1 - r.r0 + 1) * (r.c1 - r.c0 + 1)

// cell(r, c) is the value as shown (edits applied), null = NULL
export function cellsAs(fmt: CopyFormat, rect: Rect, cols: ResultCol[], cell: (r: number, c: number) => string | null, csv: Meta['config']['csv'], dialect: string, table?: string): string {
  const sub = cols.slice(rect.c0, rect.c1 + 1)
  const rows: (string | null)[][] = []
  for (let r = rect.r0; r <= rect.r1; r++) rows.push(sub.map((_, i) => cell(r, rect.c0 + i)))
  return formatRows(fmt, sub, rows, csv, dialect, table)
}

// one value per line; NULL shows as NULL, like Copy cells
export function columnText(n: number, c: number, cell: (r: number, c: number) => string | null): string {
  const out: string[] = []
  for (let r = 0; r < n; r++) out.push(cell(r, c) ?? 'NULL')
  return out.join('\n')
}
