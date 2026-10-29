// Sort by several columns.
import { useState } from 'react'
import type { Sort } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS } from '../shell/ui'

export function SortDialog({ columns, sort, onApply, onClose }: { columns: string[]; sort: Sort[]; onApply: (s: Sort[]) => void; onClose: () => void }) {
  const [rows, setRows] = useState<Sort[]>(sort.length ? sort : [{ Column: columns[0] ?? '', Desc: false }])
  const set = (i: number, p: Partial<Sort>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...p } : r)))
  return (
    <Dialog title="Sort by several columns" onClose={onClose}>
      {rows.map((r, i) => (
        <div key={i} className="flex items-center gap-2 my-1.5">
          <span className="w-5 text-ink-3">{i + 1}</span>
          <Select aria-label={`Column ${i + 1}`} value={r.Column} onChange={(v) => set(i, { Column: v })} options={columns} />
          <Select aria-label={`Direction ${i + 1}`} value={r.Desc ? 'desc' : 'asc'} onChange={(v) => set(i, { Desc: v === 'desc' })} options={[{ value: 'asc', label: 'Ascending' }, { value: 'desc', label: 'Descending' }]} />
          <button className="btn-ghost" onClick={() => setRows(rows.filter((_, j) => j !== i))}>Remove</button>
        </div>
      ))}
      <div className={ACTIONS}>
        <button onClick={() => setRows([...rows, { Column: columns.find((c) => !rows.some((r) => r.Column === c)) ?? columns[0], Desc: false }])}>Add column</button>
        <span className="flex-1" /><button onClick={onClose}>Cancel</button>
        <button className="btn-primary" onClick={() => onApply(rows.filter((r, i) => r.Column && rows.findIndex((x) => x.Column === r.Column) === i))}>Sort</button>
      </div>
    </Dialog>
  )
}
