import { useState } from 'react'
import type { Val } from './api'
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
      {value === null && <p className="dim" style={{ margin: '0 0 6px' }}>This cell is NULL.</p>}
      <textarea className="mono cell" value={text} readOnly={readOnly} autoFocus spellCheck={false} onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !readOnly) { e.preventDefault(); save() } }} />
      <div className="actions">
        {!readOnly && <>
          <span className="grow dim">{text.length} characters</span>
          <button onClick={() => onDone({ $: 'null' })}>Set NULL</button>
          <button onClick={() => onDone({ $: 'default' })}>Set DEFAULT</button>
          <button onClick={() => onDone({ $: 'empty' })}>Set empty</button>
        </>}
        <button onClick={() => onDone(undefined)}>{readOnly ? 'Close' : 'Cancel'}</button>
        {!readOnly && <button className="primary" onClick={save} title="Ctrl+Enter">Save</button>}
      </div>
    </Dialog>
  )
}
