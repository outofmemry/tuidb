// normItem: the server sends cells/columns as null for a failed statement; the result tabs always hold arrays.
export const normItem = <T extends { cells?: unknown[] | null; columns?: unknown[] | null }>(x: T): T => ({ ...x, cells: x.cells ?? [], columns: x.columns ?? [] })

// stmtAt picks the statement around offset from POST /split ranges, like db.StatementAt: the one holding it, else the last one before it, else the first.
export function stmtAt(ranges: [number, number][], offset: number): [number, number] | null {
  let prev: [number, number] | null = null
  for (const r of ranges) {
    if (offset >= r[0] && offset <= r[1]) return r
    if (r[1] < offset) prev = r
  }
  return prev ?? ranges[0] ?? null
}
