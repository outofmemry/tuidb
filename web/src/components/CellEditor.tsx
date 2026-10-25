import { useState } from 'react'
import type { Val } from '../utils/api'
import { Dialog } from './Dialogs'

export type CellTarget = { row: string; col: string; readOnly?: boolean }

const pretty = (t: string) => {
  const s = t.trim()
  if (s.length > 1 && (s[0] === '{' || s[0] === '[')) { try { return JSON.stringify(JSON.parse(s), null, 2) } catch { /* not JSON */ } }
  return t
}

// CellEditor is the popover for one cell: long text and JSON documents do not fit in a grid cell.
// Ctrl/Cmd+Enter saves, Esc cancels. JSON opens pretty-printed; saving unchanged text is a no-op.
export function CellEditor({ col, value, readOnly, onDone }: { col: string; value: string | null; readOnly?: boolean; onDone: (v: Val | undefined) => void }) {
  const start = value === null ? '' : pretty(value)
  const [text, setText] = useState(start)
  const save = () => onDone(text === start ? undefined : text)
  return (
    <Dialog title={readOnly ? col : `Edit ${col}`} onClose={() => onDone(undefined)}>
      {value === null && <p className="text-ink-2" style={{ margin: '0 0 6px' }}>This cell is NULL.</p>}
      <textarea className="font-mono w-full min-h-[40vh] max-h-[60vh] resize-y text-[12.5px] leading-[1.45] px-3 py-2.5 bg-canvas" value={text} readOnly={readOnly} autoFocus spellCheck={false} onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !readOnly) { e.preventDefault(); save() } }} />
      <div className="flex justify-end gap-2 mt-[18px]">
        {!readOnly && <>
          <span className="flex-1 min-w-0 text-left text-ink-2">{text.length} characters</span>
          <button onClick={() => onDone({ $: 'null' })}>Set NULL</button>
          <button onClick={() => onDone({ $: 'default' })}>Set DEFAULT</button>
          <button onClick={() => onDone({ $: 'empty' })}>Set empty</button>
        </>}
        <button onClick={() => onDone(undefined)}>{readOnly ? 'Close' : 'Cancel'}</button>
        {!readOnly && <button className="btn-primary" onClick={save} title="Ctrl+Enter">Save</button>}
      </div>
    </Dialog>
  )
}
