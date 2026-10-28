// Sidebar name matching, ported from matchName / fuzzy in internal/ui/util.go so both UIs rank the same way.

export function fuzzy(pattern: string, s: string): [number, boolean] {
  if (pattern === '') return [0, true]
  const p = Array.from(pattern.toLowerCase()), t = Array.from(s.toLowerCase())
  let score = 0, pi = 0, last = -2
  for (let ti = 0; ti < t.length && pi < p.length; ti++) {
    if (t[ti] !== p[pi]) continue
    score += 1
    if (ti === last + 1) score += 3 // consecutive
    if (ti === 0 || '_. :'.includes(t[ti - 1])) score += 2 // word start
    last = ti
    pi++
  }
  return pi === p.length ? [score - Math.floor(t.length / 8), true] : [0, false] // longer names rank lower, as in the TUI
}

// matchName: `^` anchors the start, `$` the end (case-insensitive, score 10); otherwise fuzzy or plain contains per [sidebar] matching.
export function matchName(pattern: string, name: string, fuzzyMode: boolean): [number, boolean] {
  const a = pattern.startsWith('^'), z = pattern.endsWith('$')
  if (a || z) {
    const l = name.toLowerCase(), pp = pattern.slice(a ? 1 : 0, z ? -1 : undefined).toLowerCase()
    return [10, a && z ? l === pp : a ? l.startsWith(pp) : l.endsWith(pp)]
  }
  if (fuzzyMode) return fuzzy(pattern, name)
  return [0, name.toLowerCase().includes(pattern.toLowerCase())]
}

// The sidebar's mode: anything but "contains" is fuzzy, as in the TUI.
export const isFuzzy = (matching: unknown) => matching !== 'contains'

// ---- Redis key folders (internal/ui/keytree.go) ----

export type KeyNode<T> = { seg: string; path: string; obj?: T; kids: KeyNode<T>[]; idx: Map<string, KeyNode<T>> }

// buildKeyTree splits names on ":" into folders; a folder with no key of its own and a single child is merged into it (user:1:name stays one row).
export function buildKeyTree<T extends { Name: string }>(objs: T[]): KeyNode<T> {
  const root: KeyNode<T> = { seg: '', path: '', kids: [], idx: new Map() }
  for (const o of objs) {
    let n = root
    for (const seg of o.Name.split(':')) {
      let c = n.idx.get(seg)
      if (!c) {
        c = { seg, path: n === root ? seg : n.path + ':' + seg, kids: [], idx: new Map() }
        n.idx.set(seg, c); n.kids.push(c)
      }
      n = c
    }
    n.obj = o
  }
  const fix = (n: KeyNode<T>) => {
    n.kids.sort((a, b) => (a.seg < b.seg ? -1 : a.seg > b.seg ? 1 : 0))
    for (const k of n.kids) {
      while (!k.obj && k.kids.length === 1) {
        const c = k.kids[0]
        k.seg += ':' + c.seg; k.path = c.path; k.obj = c.obj; k.kids = c.kids; k.idx = c.idx
      }
      fix(k)
    }
  }
  fix(root)
  return root
}

export const countKeys = <T,>(n: KeyNode<T>): number => (n.obj ? 1 : 0) + n.kids.reduce((s, k) => s + countKeys(k), 0)

// flattenKeys lists the visible rows: folders (collapsed unless their path is in open) and keys.
export type KeyRow<T> = { depth: number; folder: boolean; seg: string; path: string; open: boolean; count: number; obj?: T }
export function flattenKeys<T>(n: KeyNode<T>, open: Set<string>, depth = 0, out: KeyRow<T>[] = []): KeyRow<T>[] {
  for (const k of n.kids) {
    if (!k.kids.length) { out.push({ depth, folder: false, seg: k.seg, path: k.path, open: false, count: 1, obj: k.obj }); continue }
    const o = open.has(k.path)
    out.push({ depth, folder: true, seg: k.seg, path: k.path, open: o, count: countKeys(k), obj: k.obj })
    if (o) flattenKeys(k, open, depth + 1, out)
  }
  return out
}
