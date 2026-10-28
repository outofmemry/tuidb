import { FIND_OPS, type FindOp, type FindOpts } from '../utils/gridfind'
import { Select } from './Select'

export type FindState = FindOpts & { replace: string }
export const newFind = (text = ''): FindState => ({ text, col: '', op: 'Contains', matchCase: false, replace: '' })

// FindBar is the grid / result search: column (or any), operator, match case, and Replace one / all when the grid is editable.
// The owner holds the state and does the walking, so the data grid and a query result share it.
export function FindBar({ value, columns, editable, onChange, onStep, onReplace, onReplaceAll, onClose }: {
  value: FindState; columns: string[]; editable: boolean
  onChange: (v: FindState) => void; onStep: (dir: 1 | -1) => void; onReplace: () => void; onReplaceAll: () => void; onClose: () => void
}) {
  const set = (p: Partial<FindState>) => onChange({ ...value, ...p })
  return (
    <div className="flex flex-col gap-1.5 px-2.5 py-2 border-b border-rule bg-panel" data-ctx="find" role="search" aria-label="Find in the loaded rows"
      onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }}>
      <div className="flex items-center gap-1.5 flex-wrap">
        <Select className="bg-canvas" aria-label="Column" value={value.col} onChange={(v) => set({ col: v })} options={[{ value: '', label: 'Any column' }, ...columns]} />
        <Select className="bg-canvas" aria-label="Operator" value={value.op} onChange={(v) => set({ op: v as FindOp })} options={[...FIND_OPS]} />
        <input autoFocus className="flex-1 min-w-[10ch]" aria-label="Find" placeholder="Find" value={value.text} onChange={(e) => set({ text: e.target.value })}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); onStep(e.shiftKey ? -1 : 1) } }} />
        <label className="flex items-center gap-1.5 text-ink-2"><input type="checkbox" checked={value.matchCase} onChange={(e) => set({ matchCase: e.target.checked })} />Match case</label>
        <button onClick={() => onStep(-1)} aria-label="Previous match">Prev</button>
        <button onClick={() => onStep(1)} aria-label="Next match">Next</button>
        <button className="btn-ghost" onClick={onClose} aria-label="Close find">✕</button>
      </div>
      {editable && value.op !== 'Not contains' && (
        <div className="flex items-center gap-1.5 flex-wrap">
          <input className="flex-1 min-w-[10ch]" aria-label="Replace with" placeholder="Replace with" value={value.replace} onChange={(e) => set({ replace: e.target.value })} />
          <button onClick={onReplace} disabled={!value.text}>Replace</button>
          <button onClick={onReplaceAll} disabled={!value.text}>Replace all</button>
        </div>)}
    </div>
  )
}
