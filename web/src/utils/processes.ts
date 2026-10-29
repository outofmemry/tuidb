// Pure helpers of the process list, tested without a browser.


export type ProcRow = { id: string; user: string; db: string; state: string; elapsedMs: number; query: string }
export function sortProcs(l: ProcRow[], by: string, desc: boolean): ProcRow[] {
  const dir = desc ? -1 : 1
  const key = (p: ProcRow) => (by === 'elapsed' ? p.elapsedMs : by === 'id' ? Number(p.id) : (p as any)[by] ?? '')
  return [...l].sort((a, b) => {
    const x = key(a), y = key(b)
    const r = typeof x === 'number' && typeof y === 'number' && !Number.isNaN(x) && !Number.isNaN(y) ? x - y : String(x).localeCompare(String(y), undefined, { numeric: true })
    return r * dir
  })
}
