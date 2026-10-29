import { useEffect, useRef, useState } from 'react'
import { Select } from '../shell/Select'
import { forThisTab } from '../../utils/links'
import { api, recentKey, type Filter, type FilterSet, type ObjRef } from '../../utils/api'
import { copyText } from '../../utils/copy'

const ANY = ''
const RAW = '\x00raw'

// FilterBar edits a draft and pushes it up on Apply, so typing does not refetch on every key.
export function FilterBar({ sid, obj, value, columns, operators, onApply, onExport, active, defaults, pk, menu }: { menu?: { label: string; action: string }[]; active?: boolean; defaults?: Defaults; pk?: string; sid: string; obj: ObjRef; value: FilterSet; columns: string[]; operators: string[]; onApply: (f: FilterSet) => void; onExport?: (f: FilterSet) => void }) {
  const fresh = () => blank(columns, operators, defaults, pk)
  // the button labels are meta.menus.filter_bar (internal/menus.FilterBar), the terminal's filter bar menu: same words, same order
  const L = (action: string, dflt: string) => menu?.find((i) => i.action === action)?.label ?? dflt
  const [draft, setDraft] = useState<FilterSet>(value.Filters.length ? value : { Filters: [fresh()], Or: false })
  const rows = useRef<HTMLDivElement>(null)
  const row = useRef(0) // the filter row last focused: add-as-copy copies it
  const set = (i: number, p: Partial<Filter>) => setDraft({ ...draft, Filters: draft.Filters.map((f, j) => (j === i ? { ...f, ...p } : f)) })
  const noValue = (op: string) => op === 'IS NULL' || op === 'IS NOT NULL'
  const [sql, setSql] = useState<{ text: string; err?: boolean } | null>(null) // filter_sql: the WHERE the filters make
  const showSql = async () => {
    try { setSql({ text: (await api<{ where: string }>('POST', `/sessions/${sid}/filters/sql`, { Ref: obj, Filters: cleaned() })).where }) } catch (e) { setSql({ text: e instanceof Error ? e.message : String(e), err: true }) }
  }
  useEffect(() => {
    if (!active) return // hidden tabs stay mounted; only the visible one answers
    const h = (e: Event) => {
      if (e.defaultPrevented) return
      const a = (e as CustomEvent).detail.action
      if (a === 'apply_filters') { e.preventDefault(); apply() } else if (a === 'filter_sql') { e.preventDefault(); showSql() }
      else if (a === 'filter_add_copy') { e.preventDefault(); addCopy() } else if (a === 'filter_clear_all') { e.preventDefault(); clearAll() } else if (a === 'filter_export') { e.preventDefault(); exportRows() }
    }
    // header menu "Filter with this column": a row for that column (the empty first row is reused), with the value box focused
    const onCol = (e: Event) => {
      const d = (e as CustomEvent).detail, column = d?.column as string | undefined
      if (!column || !forThisTab(d, sid, recentKey(obj))) return
      setDraft((d) => {
        const blankFirst = d.Filters.length === 1 && !d.Filters[0].Value && d.Filters[0].Column === ANY
        const row = { ...fresh(), Column: column }
        const n = blankFirst ? [row] : [...d.Filters, row]
        setTimeout(() => rows.current?.querySelectorAll<HTMLInputElement>('input.fb-value')[n.length - 1]?.focus(), 0)
        return { ...d, Filters: n }
      })
    }
    window.addEventListener('webtuidb:action', h)
    window.addEventListener('webtuidb:filter-column', onCol)
    return () => { window.removeEventListener('webtuidb:action', h); window.removeEventListener('webtuidb:filter-column', onCol) }
  })
  const cleaned = (): FilterSet => ({ ...draft, Filters: draft.Filters.filter((f) => f.Column === RAW ? f.Value.trim() : f.Value || noValue(f.Op)) })
  const apply = () => onApply(cleaned())
  // add a filter as a copy of the current one (the TUI's M-n); clear all filters and export the filtered rows are bar actions too (M-c, M-e)
  const addCopy = () => setDraft((d) => { const at = Math.min(row.current, d.Filters.length - 1); return at < 0 ? { ...d, Filters: [fresh()] } : { ...d, Filters: [...d.Filters.slice(0, at + 1), { ...d.Filters[at] }, ...d.Filters.slice(at + 1)] } })
  const clearAll = () => { setDraft({ Filters: [fresh()], Or: false }); onApply({ Filters: [], Or: false }) }
  const exportRows = () => onExport?.(cleaned())
  return (
    <div ref={rows} className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule bg-panel" data-ctx="filterbar" role="group" aria-label="Filters" onKeyDown={(e) => {
      if (e.defaultPrevented) return // the keymap already dispatched it
      // Alt+N / Alt+C / Alt+E arrive as keymap actions (no-prefix bindings), answered below
      if (e.key === 'Enter' && !(e.target as HTMLElement).closest('[role=menu]')) apply()
    }}>
      {draft.Filters.map((f, i) => (
        <div className="flex items-center gap-1.5 max-[760px]:flex-wrap" key={i} onFocusCapture={() => { row.current = i }}>
          <input type="checkbox" checked={f.Enabled} onChange={(e) => set(i, { Enabled: e.target.checked })} title="enabled" />
          <Select className="bg-canvas" aria-label="Column" value={f.Column} onChange={(v) => set(i, { Column: v })} options={[{ value: ANY, label: 'Any column' }, { value: RAW, label: 'Raw condition' }, ...columns]} />
          {f.Column !== RAW && (
            <Select className="bg-canvas" aria-label="Operator" value={f.Op} onChange={(v) => set(i, { Op: v })} options={operators} />
          )}
          {!noValue(f.Op) || f.Column === RAW ? (
            <input className="fb-value flex-1 min-w-0" aria-label="Filter value" value={f.Value} placeholder={f.Column === RAW ? 'e.g. age > 30 AND active' : 'value'} onChange={(e) => set(i, { Value: e.target.value, Enabled: true })} />
          ) : <span className="flex-1 min-w-0" />}
          <button className="btn-ghost btn-icon text-ink-3 hover:text-del" aria-label="Remove this filter" title="Remove this filter" onClick={() => setDraft({ ...draft, Filters: draft.Filters.filter((_, j) => j !== i) })}>✕</button>
        </div>
      ))}
      <div className="flex items-center gap-1.5 flex-wrap">
        <button className="btn-primary" onClick={apply} title="Apply the filters (Enter)">{L('apply_filters', 'Apply')}</button>
        <button onClick={() => setDraft({ ...draft, Filters: [...draft.Filters, fresh()] })}>{L('filter_add', 'Add filter')}</button>
        <button className="btn-ghost" onClick={addCopy} title="Add a filter as a copy of the current one (Alt+N)">{L('filter_add_copy', 'Add a filter as a copy')}</button>
        <label className="flex items-center gap-1.5 text-ink-2 whitespace-nowrap px-1"><input type="checkbox" checked={draft.Or} onChange={(e) => setDraft({ ...draft, Or: e.target.checked })} />Match any</label>
        <span className="flex-1 min-w-2" />
        <button className="btn-ghost" onClick={showSql} title="Show the WHERE clause these filters make">{L('filter_sql', 'Show SQL')}</button>
        <button className="btn-ghost" onClick={clearAll} title="Remove every filter and show all rows (Alt+C)">{L('filter_clear_all', 'Clear all')}</button>
        {onExport && <button className="btn-ghost" onClick={exportRows} title="Export the rows these filters match (Alt+E)">{L('filter_export', 'Export filtered rows')}</button>}
      </div>
      {sql && <div className="flex items-start gap-2 border border-rule-2 bg-canvas p-2 rounded-[2px] motion-safe:animate-rise" role="status" aria-label="Generated WHERE clause">
        <pre className={`flex-1 min-w-0 m-0 whitespace-pre-wrap wrap-anywhere text-[12px] ${sql.err ? 'text-del' : ''}`}>{sql.err ? sql.text : sql.text ? 'WHERE ' + sql.text : 'No active filters: no WHERE clause.'}</pre>
        {!sql.err && sql.text && <button onClick={() => copyText(sql.text)}>Copy</button>}<button className="btn-ghost" onClick={() => setSql(null)} aria-label="Close">✕</button></div>}
    </div>
  )
}

// [table] filter_default_column (pk | any | raw) and filter_default_operator (= | contains) pick what a new row starts with; pk is the table's first primary key column.
export type Defaults = { column: string; operator: string }
export const blank = (_c: string[], ops: string[], d?: Defaults, pk?: string): Filter => {
  const want = d?.operator === '=' ? '=' : 'Contains'
  return { Column: d?.column === 'raw' ? RAW : d?.column === 'pk' && pk ? pk : ANY, Op: ops.includes(want) ? want : ops.includes('Contains') ? 'Contains' : ops[0] ?? '=', Value: '', Enabled: true }
}
