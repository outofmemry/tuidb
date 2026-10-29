// Editor completion for the query tab. It follows queryTab.complete in internal/ui/tab_query.go: the same candidates
// (objects, the columns of the tables the statement reads, `alias.` / `schema.` qualifiers, saved-query keywords, keywords)
// and the same ranking (fuzzy subsequence score, +20 for a prefix match, best 50).
import { acceptCompletion, closeCompletion, moveCompletionSelection, startCompletion, type Completion, type CompletionContext, type CompletionResult } from '@codemirror/autocomplete'
import { Prec } from '@codemirror/state'
import { keymap } from '@codemirror/view'
import type { DbObject } from '../../utils/api'

// same rule as fuzzy() in internal/ui/util.go
export function fuzzy(pattern: string, s: string): number | null {
  if (!pattern) return 0
  const p = pattern.toLowerCase(), t = s.toLowerCase()
  let score = 0, pi = 0, last = -2
  for (let ti = 0; ti < t.length && pi < p.length; ti++) {
    if (t[ti] !== p[pi]) continue
    score += 1
    if (ti === last + 1) score += 3
    if (ti === 0 || '_. :'.includes(t[ti - 1])) score += 2
    last = ti; pi++
  }
  return pi < p.length ? null : score - Math.floor(t.length / 8)
}

export type Cand = { text: string; kind: string; insert?: string }
export function rank(word: string, all: Cand[], limit = 50): Cand[] {
  const hits: { c: Cand; s: number }[] = []
  for (const c of all) {
    let s = fuzzy(word, c.text)
    if (s === null) continue
    if (c.text.toLowerCase().startsWith(word.toLowerCase())) s += 20
    hits.push({ c, s })
  }
  return hits.sort((a, b) => b.s - a.s).slice(0, limit).map((h) => h.c) // Array.sort is stable
}

// Every table named after FROM / JOIN, with its alias: `alias.` narrows to that table. The value keeps the schema
// (`shop.customers`), so `c.` on `shop.customers c` offers shop's columns, not the current schema's customers.
const ALIAS = /\b(?:from|join)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)(?:\s+(?:as\s+)?(\w+))?/gi
const NOT_ALIAS = new Set(['where', 'on', 'using', 'join', 'inner', 'left', 'right', 'full', 'outer', 'cross', 'natural', 'group', 'order', 'limit', 'offset', 'having', 'union', 'except', 'intersect', 'window', 'returning', 'set', 'values', 'select', 'lateral'])
export function aliasesIn(sql: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const m of sql.matchAll(ALIAS)) {
    const full = m[1].replace(/"/g, '').toLowerCase(), t = full.split('.').pop()!
    out[t] = full
    if (m[2] && !NOT_ALIAS.has(m[2].toLowerCase())) out[m[2].toLowerCase()] = full
  }
  return out
}
// the tables an unqualified column may come from (refRe in the terminal UI), schema-qualified as written
const REFS = /\b(?:from|join|update|into|table)\s+((?:"[^"]+"|\w+)(?:\.(?:"[^"]+"|\w+))?)/gi
export const tablesRead = (sql: string) => [...new Set([...sql.matchAll(REFS)].map((m) => m[1].replace(/"/g, '').toLowerCase()))]
// splitRef turns `shop.customers` into ['shop', 'customers'] and `customers` into ['', 'customers']
export const splitRef = (ref: string): [string, string] => { const i = ref.lastIndexOf('.'); return [i < 0 ? '' : ref.slice(0, i), ref.slice(i + 1)] }

// Mongo shell methods offered after `db.<collection>.` and after a closing `)` (a chained cursor method)
export const MONGO_METHODS = ['find', 'findOne', 'aggregate', 'count', 'insertOne', 'insertMany', 'updateOne', 'updateMany', 'deleteOne', 'deleteMany', 'sort', 'limit', 'skip', 'project']

// the statement around the cursor, so an alias from another statement does not leak in (statements split at ';')
export function statementAt(doc: string, pos: number): string {
  const a = doc.lastIndexOf(';', pos - 1) + 1, b = doc.indexOf(';', pos)
  return doc.slice(a, b < 0 ? doc.length : b)
}

export type Env = {
  kind: 'sql' | 'mongo' | 'redis'
  objects: DbObject[]
  colsOf: (schema: string, table: string) => Promise<string[]>
  schemaObjects: (schema: string) => Promise<DbObject[]>
  saved: { SQL: string; Keywords: string[] | null }[]
  keywords: string[]
  opts: { tables: boolean; functions: boolean; keywords: boolean; prefixSchema: boolean; addSpace: boolean; upper: boolean }
}

const TABLE_KINDS = ['table', 'view', 'matview']
const typeOf = (kind: string) => (kind === 'column' ? 'property' : kind === 'function' || kind === 'procedure' || kind === 'method' ? 'function' : kind === 'keyword' ? 'keyword' : kind === 'table' || kind === 'collection' ? 'class' : kind === 'snippet' || kind === 'saved' ? 'text' : 'type')

export function completionSource(env: () => Env) {
  return async (ctx: CompletionContext): Promise<CompletionResult | null> => {
    const e = env()
    const chain = e.kind === 'mongo' ? ctx.matchBefore(/\)\.\w*/) : null // db.c.find(...).sort
    if (chain) return { from: chain.from + chain.text.lastIndexOf('.') + 1, options: MONGO_METHODS.map((label) => ({ label, type: 'method', apply: label + '(' })), validFor: /^\w*$/ }
    const w = ctx.matchBefore(/[\w$"]*(?:\.[\w$"]*)*/)
    if (!w || (w.from === w.to && !ctx.explicit)) return null
    const dot = w.text.lastIndexOf('.'), word = w.text.slice(dot + 1).replace(/"/g, '')
    let qual = dot >= 0 ? w.text.slice(0, dot).replace(/"/g, '').toLowerCase() : ''
    const stmt = statementAt(ctx.state.doc.toString(), ctx.pos)
    if (qual) qual = aliasesIn(stmt)[qual] ?? qual // "c." where c is an alias of customers
    let all: Cand[] = []
    if (qual) {
      if (e.kind === 'mongo') {
        if (/^db\.\w+$/i.test(qual)) all = MONGO_METHODS.map((text) => ({ text, kind: 'method', insert: text + '(' }))
        else if (qual === 'db') all = e.objects.map((o) => ({ text: o.Name, kind: 'collection' }))
      } else {
        const [schema, table] = splitRef(qual)
        for (const c of await e.colsOf(schema, table)) all.push({ text: c, kind: 'column' })
        let inSchema = e.objects.filter((o) => o.Schema.toLowerCase() === qual) // schema.table
        if (!inSchema.length && !all.length) inSchema = await e.schemaObjects(qual)
        for (const o of inSchema) all.push({ text: o.Name, kind: o.Kind })
      }
    } else {
      const seen = new Set<string>()
      const add = (c: Cand) => { const k = c.text.toLowerCase(); if (!seen.has(k)) { seen.add(k); all.push(c) } }
      for (const o of e.objects) {
        const fn = o.Kind === 'function' || o.Kind === 'procedure'
        if (fn ? !e.opts.functions : !e.opts.tables) continue
        add({ text: o.Name, kind: o.Kind, insert: e.opts.prefixSchema && o.Schema ? `${o.Schema}.${o.Name}` : undefined })
      }
      if (e.kind === 'sql') for (const t of tablesRead(stmt)) for (const c of await e.colsOf(...splitRef(t))) add({ text: c, kind: 'column' })
      for (const q of e.saved) for (const k of q.Keywords ?? []) {
        if (seen.has(k.toLowerCase())) continue
        seen.add(k.toLowerCase())
        const sn = k.startsWith('snippet:') // a saved query keyworded snippet:<trigger> expands from <trigger>
        all.push({ text: sn ? k.slice(8) : k, kind: sn ? 'snippet' : 'saved', insert: q.SQL })
      }
      if (e.opts.keywords) for (const k of e.keywords) add({ text: e.opts.upper ? k.toUpperCase() : k.toLowerCase(), kind: 'keyword' })
    }
    const options: Completion[] = rank(word, all).map((c) => {
      const base = c.insert ?? c.text
      return { label: c.text, type: typeOf(c.kind), detail: c.kind, apply: e.opts.addSpace && TABLE_KINDS.includes(c.kind) && !c.insert?.includes('\n') ? base + ' ' : base }
    })
    // filter:false keeps the terminal's order; the list is rebuilt on each keystroke (columns are cached)
    return options.length ? { from: dot >= 0 ? w.from + dot + 1 : w.from, options, filter: false } : null
  }
}

// [editor] complete_key: enter_or_tab | enter | tab accept the highlighted entry. Used with autocompletion({ defaultKeymap: false }).
export const completionKeys = (completeKey: string) => Prec.highest(keymap.of([
  { key: 'Ctrl-Space', run: startCompletion }, { key: 'Escape', run: closeCompletion },
  { key: 'ArrowDown', run: moveCompletionSelection(true) }, { key: 'ArrowUp', run: moveCompletionSelection(false) },
  { key: 'PageDown', run: moveCompletionSelection(true, 'page') }, { key: 'PageUp', run: moveCompletionSelection(false, 'page') },
  ...(completeKey !== 'tab' ? [{ key: 'Enter', run: acceptCompletion }] : []),
  ...(completeKey !== 'enter' ? [{ key: 'Tab', run: acceptCompletion }] : []),
]))
