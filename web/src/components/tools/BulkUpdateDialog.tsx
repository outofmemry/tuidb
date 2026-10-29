// Bulk update: set columns on every row that matches the filter.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type FilterSet, type ObjRef, type Plan } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

type SetRow = { col: string; val: string; isNull: boolean }

export function BulkUpdateDialog({ sid, obj, filters, columns, onPlan, onClose }: { sid: string; obj: ObjRef; filters: FilterSet; columns: string[]; onPlan: (p: Plan) => void; onClose: () => void }) {
  const active = filters.Filters.some((f) => f.Enabled)
  const n = useQuery({ queryKey: ['bulkcount', sid, obj, filters], queryFn: () => api<{ n: number; exact: boolean }>('POST', `/sessions/${sid}/count`, { ref: obj, filters, estimate: false }), enabled: active, gcTime: 0 })
  const [rows, setRows] = useState<SetRow[]>([{ col: columns[0] ?? '', val: '', isNull: false }])
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const set = (i: number, p: Partial<SetRow>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...p } : r)))
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/tools/bulk-update`, { Ref: obj, Filters: filters, Set: rows.map((r) => ({ Col: r.col, Val: r.isNull ? null : r.val })) })
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title={`Bulk update ${obj.Name}`} onClose={onClose}>
      <p className="mt-0 text-ink-2">{active ? <>Sets these columns on every row the current filter matches{n.data ? <>: <b className="text-ink">{n.data.n} row{n.data.n === 1 ? '' : 's'}</b></> : ''}.</> : 'Add a filter first: a bulk update without one is refused, so a slip cannot rewrite the whole table.'}</p>
      {rows.map((r, i) => (
        <div key={i} className={ROW}>
          <Select aria-label={`Column ${i + 1}`} value={r.col} onChange={(v) => set(i, { col: v })} options={columns} />
          <span className="text-ink-2">=</span>
          <input className="flex-1 min-w-0" aria-label={`Value ${i + 1}`} disabled={r.isNull} value={r.isNull ? '' : r.val} onChange={(e) => set(i, { val: e.target.value })} />
          <label className="flex items-center gap-1.5"><input type="checkbox" checked={r.isNull} onChange={(e) => set(i, { isNull: e.target.checked })} />NULL</label>
          {rows.length > 1 && <button className="btn-ghost" onClick={() => setRows(rows.filter((_, j) => j !== i))} aria-label={`Remove column ${i + 1}`}>✕</button>}
        </div>
      ))}
      <button onClick={() => setRows([...rows, { col: columns.find((c) => !rows.some((r) => r.col === c)) ?? columns[0], val: '', isNull: false }])}>Add column</button>
      {err && <div className="text-del mt-2 whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={busy || !active || rows.some((r) => !r.col)} onClick={go}>{busy ? 'Planning…' : 'Preview statement'}</button></div>
    </Dialog>
  )
}
