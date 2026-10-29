// Find / replace over grid cells: a column (or any), an operator, match case. Pure so it can be tested without a browser.

export const FIND_OPS = ['Contains', 'Not contains', 'Has prefix', 'Has suffix'] as const
export type FindOp = (typeof FIND_OPS)[number]
export type FindOpts = { text: string; col: string; op: FindOp; matchCase: boolean } // col '' = any column

// re builds a case-insensitive (or exact) RegExp from the escaped text, so matching and slicing use the same string and indexes
// (toLowerCase can change a string's length: 'İ' lowers to two code units).
const re = (o: FindOpts, g = false) => {
  const t = o.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(o.op === 'Has prefix' ? '^' + t : o.op === 'Has suffix' ? t + '$' : t, (g ? 'g' : '') + (o.matchCase ? '' : 'i') + 'u')
}

// cellMatches: a NULL cell never matches (Not contains included: there is no text to be absent from).
export function cellMatches(v: string | null, o: FindOpts): boolean {
  if (v === null || o.text === '') return false
  const hit = re(o).test(v)
  return o.op === 'Not contains' ? !hit : hit
}

// findNext walks the cells row-major from just after (row, col) in the direction dir and wraps; null when nothing matches.
export function findNext(cell: (r: number, c: number) => string | null, rows: number, names: string[], o: FindOpts, from: { r: number; c: number }, dir: 1 | -1): { r: number; c: number } | null {
  const cols = o.col ? names.map((n, i) => (n === o.col ? i : -1)).filter((i) => i >= 0) : names.map((_, i) => i)
  if (!rows || !cols.length || o.text === '') return null
  const n = rows * names.length, pos = from.r * names.length + from.c
  for (let i = 1; i <= n; i++) {
    const p = (((pos + dir * i) % n) + n) % n
    const r = Math.floor(p / names.length), c = p % names.length
    if (cols.includes(c) && cellMatches(cell(r, c), o)) return { r, c }
  }
  return null
}

// replaceIn rewrites one value: Contains replaces every occurrence, Has prefix / Has suffix replace that end. Not contains has nothing to replace.
export function replaceIn(v: string, o: FindOpts, by: string): string | null {
  if (!cellMatches(v, o) || o.op === 'Not contains') return null
  return v.replace(re(o, true), () => by)
}
