// Pending grid changes (edits, deletes, new rows) and the /plan request they become. Pure, so it can be tested without a browser.
// Rows are keyed 'r<index>' for a row the server returned and 'n<id>' for a new one. Query results use this; the table tab keeps its own PK-keyed copy.
import type { Val } from './api'

export type Edits = Record<string, Record<string, Val>>
export type Insert = { id: number; vals: Record<string, Val> }
export type Pend = { edits: Edits; deletes: Set<string>; inserts: Insert[] }
type Col = { name: string; pk?: boolean }

export const emptyPend = (): Pend => ({ edits: {}, deletes: new Set(), inserts: [] })
export const pendCount = (p: Pend | undefined) => (p ? Object.keys(p.edits).length + p.deletes.size + p.inserts.length : 0)
export const rowNo = (key: string) => Number(key.slice(1))

// setEdit stores one cell value: on a new row it goes into that row's values, on a server row into edits.
export function setEdit(p: Pend, key: string, col: string, v: Val): Pend {
  if (key[0] === 'n') return { ...p, inserts: p.inserts.map((n) => ('n' + n.id === key ? { ...n, vals: { ...n.vals, [col]: v } } : n)) }
  return { ...p, edits: { ...p.edits, [key]: { ...p.edits[key], [col]: v } } }
}

// editedMap is what the grid shows over the server values: edits plus the new rows' values.
export function editedMap(p: Pend): Edits {
  const m: Edits = { ...p.edits }
  for (const n of p.inserts) m['n' + n.id] = n.vals
  return m
}

// toggleDelete: a new row is dropped, a server row is marked (or unmarked).
export function toggleDelete(p: Pend, key: string): Pend {
  if (key[0] === 'n') return { ...p, inserts: p.inserts.filter((n) => 'n' + n.id !== key) }
  const d = new Set(p.deletes)
  if (d.has(key)) d.delete(key); else d.add(key)
  return { ...p, deletes: d }
}

// dupVals: the values of a duplicated row. The primary key is left out so the database picks it; a NULL stays NULL.
export function dupVals(columns: Col[], cells: (string | null)[], edited: Record<string, Val> | undefined, shown: (v: Val | undefined, server: string | null) => string | null): Record<string, Val> {
  const vals: Record<string, Val> = {}
  columns.forEach((c, i) => {
    if (c.pk) return
    const v = shown(edited?.[c.name], cells[i] ?? null)
    vals[c.name] = v === null ? { $: 'null' } : v
  })
  return vals
}

// planBody is the POST .../plan request: the PK values the user saw ride along so the server rejects a stale snapshot.
export function planBody(resultId: string | undefined, columns: Col[], cells: (string | null)[][], p: Pend) {
  const keyOf = (k: string) => {
    const row = cells[rowNo(k)] ?? []
    return Object.fromEntries(columns.flatMap((c, i) => (c.pk ? [[c.name, row[i] ?? '']] : [])))
  }
  const dels = [...p.deletes].sort((a, b) => rowNo(a) - rowNo(b))
  return {
    resultId,
    updates: Object.entries(p.edits).filter(([k]) => !p.deletes.has(k)).map(([k, set]) => ({ row: rowNo(k), set, key: keyOf(k) })),
    deletes: dels.map(rowNo),
    deleteKeys: dels.map(keyOf),
    inserts: p.inserts.map((n) => n.vals),
  }
}
