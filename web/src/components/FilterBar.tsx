import { Select } from './Select'
import { useEffect, useState } from 'react'
import type { Filter, FilterSet } from '../utils/api'

const ANY = ''
const RAW = '\x00raw'

// FilterBar edits a draft and pushes it up on Apply, so typing does not refetch on every key.
export function FilterBar({ value, columns, operators, onApply }: { value: FilterSet; columns: string[]; operators: string[]; onApply: (f: FilterSet) => void }) {
  const [draft, setDraft] = useState<FilterSet>(value.Filters.length ? value : { Filters: [blank(columns, operators)], Or: false })
  const set = (i: number, p: Partial<Filter>) => setDraft({ ...draft, Filters: draft.Filters.map((f, j) => (j === i ? { ...f, ...p } : f)) })
  const noValue = (op: string) => op === 'IS NULL' || op === 'IS NOT NULL'
  useEffect(() => {
    const h = (e: Event) => { if ((e as CustomEvent).detail.action === 'apply_filters') { e.preventDefault(); apply() } }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  })
  const apply = () => onApply({ ...draft, Filters: draft.Filters.filter((f) => f.Column === RAW ? f.Value.trim() : f.Value || noValue(f.Op)) })
  return (
    <div className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule bg-panel" data-ctx="filterbar" onKeyDown={(e) => e.key === 'Enter' && !(e.target as HTMLElement).closest('[role=menu]') && apply()}>
      {draft.Filters.map((f, i) => (
        <div className="flex items-center gap-1.5" key={i}>
          <input type="checkbox" checked={f.Enabled} onChange={(e) => set(i, { Enabled: e.target.checked })} title="enabled" />
          <Select className="bg-canvas" aria-label="Column" value={f.Column} onChange={(v) => set(i, { Column: v })} options={[{ value: ANY, label: 'Any column' }, { value: RAW, label: 'Raw condition' }, ...columns]} />
          {f.Column !== RAW && (
            <Select className="bg-canvas" aria-label="Operator" value={f.Op} onChange={(v) => set(i, { Op: v })} options={operators} />
          )}
          {!noValue(f.Op) || f.Column === RAW ? (
            <input className="flex-1 min-w-0" value={f.Value} placeholder={f.Column === RAW ? 'e.g. age > 30 AND active' : 'value'} onChange={(e) => set(i, { Value: e.target.value, Enabled: true })} />
          ) : <span className="flex-1 min-w-0" />}
          <button className="btn-ghost" aria-label="Remove this filter" title="Remove this filter" onClick={() => setDraft({ ...draft, Filters: draft.Filters.filter((_, j) => j !== i) })}>Remove</button>
        </div>
      ))}
      <div className="flex items-center gap-1.5">
        <button onClick={() => setDraft({ ...draft, Filters: [...draft.Filters, blank(columns, operators)] })}>Add filter</button>
        <label className="flex items-center gap-1.5 text-ink-2"><input type="checkbox" checked={draft.Or} onChange={(e) => setDraft({ ...draft, Or: e.target.checked })} /> Match any filter</label>
        <button className="btn-primary" onClick={apply}>Apply filters</button>
        <button onClick={() => { setDraft({ Filters: [blank(columns, operators)], Or: false }); onApply({ Filters: [], Or: false }) }}>Clear filters</button>
      </div>
    </div>
  )
}

const blank = (_c: string[], ops: string[]): Filter => ({ Column: ANY, Op: ops.includes('Contains') ? 'Contains' : ops[0] ?? '=', Value: '', Enabled: true })
