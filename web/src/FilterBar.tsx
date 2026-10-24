import { useEffect, useState } from 'react'
import type { Filter, FilterSet } from './api'

const ANY = ''
const RAW = '\x00raw'

// FilterBar edits a draft and pushes it up on Apply, so typing does not refetch on every key.
export function FilterBar({ value, columns, operators, onApply }: { value: FilterSet; columns: string[]; operators: string[]; onApply: (f: FilterSet) => void }) {
  const [draft, setDraft] = useState<FilterSet>(value.Filters.length ? value : { Filters: [blank(columns, operators)], Or: false })
  const set = (i: number, p: Partial<Filter>) => setDraft({ ...draft, Filters: draft.Filters.map((f, j) => (j === i ? { ...f, ...p } : f)) })
  const noValue = (op: string) => op === 'IS NULL' || op === 'IS NOT NULL'
  useEffect(() => {
    const h = (e: Event) => { if ((e as CustomEvent).detail.action === 'apply_filters') { e.preventDefault(); apply() } }
    window.addEventListener('tuidb:action', h)
    return () => window.removeEventListener('tuidb:action', h)
  })
  const apply = () => onApply({ ...draft, Filters: draft.Filters.filter((f) => f.Column === RAW ? f.Value.trim() : f.Enabled || f.Value || noValue(f.Op)) })
  return (
    <div className="filterbar" data-ctx="filterbar" onKeyDown={(e) => e.key === 'Enter' && apply()}>
      {draft.Filters.map((f, i) => (
        <div className="row" key={i}>
          <input type="checkbox" checked={f.Enabled} onChange={(e) => set(i, { Enabled: e.target.checked })} title="enabled" />
          <select value={f.Column} onChange={(e) => set(i, { Column: e.target.value })}>
            <option value={ANY}>Any column</option>
            <option value={RAW}>Raw condition</option>
            {columns.map((c) => <option key={c}>{c}</option>)}
          </select>
          {f.Column !== RAW && (
            <select value={f.Op} onChange={(e) => set(i, { Op: e.target.value })}>
              {operators.map((o) => <option key={o}>{o}</option>)}
            </select>
          )}
          {!noValue(f.Op) || f.Column === RAW ? (
            <input className="grow" value={f.Value} placeholder={f.Column === RAW ? 'e.g. age > 30 AND active' : 'value'} onChange={(e) => set(i, { Value: e.target.value, Enabled: true })} />
          ) : <span className="grow" />}
          <button className="ghost" aria-label="Remove this filter" title="Remove this filter" onClick={() => setDraft({ ...draft, Filters: draft.Filters.filter((_, j) => j !== i) })}>Remove</button>
        </div>
      ))}
      <div className="row">
        <button onClick={() => setDraft({ ...draft, Filters: [...draft.Filters, blank(columns, operators)] })}>Add filter</button>
        <label className="row dim"><input type="checkbox" checked={draft.Or} onChange={(e) => setDraft({ ...draft, Or: e.target.checked })} /> Match any filter</label>
        <button className="primary" onClick={apply}>Apply filters</button>
        <button onClick={() => { setDraft({ Filters: [blank(columns, operators)], Or: false }); onApply({ Filters: [], Or: false }) }}>Clear filters</button>
      </div>
    </div>
  )
}

const blank = (_c: string[], ops: string[]): Filter => ({ Column: ANY, Op: ops.includes('Contains') ? 'Contains' : ops[0] ?? '=', Value: '', Enabled: true })
