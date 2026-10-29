import { useEffect, useRef, useState } from 'react'
import type { ResultCol, Val } from '../../utils/api'
import { prettyJson, show } from './Grid'

type Props = {
  columns: ResultCol[]
  cells: (string | null)[] // the cursor row as the server sent it
  edited?: Record<string, Val> // pending edits on the cursor row
  hasRow: boolean // a row is selected
  rows: number // how many rows the selection holds: an edit applies to every one of them
  editable: boolean
  cursorCol: number // the cell the grid cursor is on: its field follows
  fieldQ: string; onFieldQ: (q: string) => void; best: string
  pretty: boolean // auto_pretty_json
  onSet: (col: string, v: Val) => void
  onOpen: (col: string) => void // the full popover
  onClose: () => void
}

const minify = (t: string) => { try { return JSON.stringify(JSON.parse(t)) } catch { return t } }
const isJson = (t: string) => /^\s*[{[]/.test(t) && (() => { try { JSON.parse(t); return true } catch { return false } })()

// RowDetail is the side panel for the cursor row: every field, edited in place. With several rows selected (shift-click / shift-arrows)
// an edit is applied to the field in every selected row, one pending change per row. Quick values are EMPTY / NULL / DEFAULT / NOW(); JSON gets PRETTY / MINIFY.
export function RowDetail({ columns, cells, edited, hasRow, rows, editable, cursorCol, fieldQ, onFieldQ, best, pretty, onSet, onOpen, onClose }: Props) {
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const follow = columns[cursorCol]?.name
  const hitRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => { hitRef.current?.scrollIntoView({ block: 'nearest' }) }, [follow, best])
  const begin = (name: string, val: string | null) => { if (!editable) return; setEditing(name); setDraft(val ?? '') }
  const commit = (name: string, val: string | null) => { setEditing(null); if (draft !== (val ?? '')) onSet(name, draft) }
  const quick = (name: string, v: Val) => { setEditing(null); onSet(name, v) }
  return (
    <div className="w-[340px] flex-none border-l border-rule flex flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:inset-0 max-[760px]:z-30 max-[760px]:w-full" role="region" aria-label="Row details" data-ctx="detail" tabIndex={0}>
      <div className="toolbar bg-canvas"><strong className="flex-1 min-w-0 font-medium">Row details</strong><button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      <input className="mx-2.5 mt-2 mb-1 bg-panel border-transparent" aria-label="Search for field" placeholder="Search for field…" value={fieldQ} onChange={(e) => onFieldQ(e.target.value)} />
      {rows > 1 && editable && <div className="mx-2.5 mb-1 text-[12px] text-ink-2" role="status">{rows} rows selected: an edit changes this field in all of them.</div>}
      <div className="overflow-auto flex-1">
        {!hasRow && <div className="p-2.5 text-[12.5px] text-ink-2">Select a row to see its fields.</div>}
        {hasRow && columns.map((c, i) => {
          const ed = edited?.[c.name], hit = best === c.name || (!best && follow === c.name)
          const val = show(ed, cells[i])
          const open = editing === c.name
          return (
            <div key={c.name} ref={hit ? hitRef : undefined} className={`px-3 py-2 border-b border-rule ${editable && !open ? 'cursor-pointer hover:bg-panel' : ''} ${ed !== undefined ? 'shadow-[inset_3px_0_0_var(--edit)]' : ''} ${hit ? 'bg-raised' : ''}`}
              onClick={() => !open && begin(c.name, val)} title={editable && !open ? 'Click to edit' : undefined}>
              <div className="text-[12px] text-ink-2 mb-[3px] flex items-baseline gap-1">{c.pk && <span className="text-accent">◆</span>}<span>{c.name}</span><span className="text-ink-3 text-[11px] flex-1 min-w-0">{c.type}</span>
                {editable && <button className="btn-ghost px-1" aria-label={`Open ${c.name} in the editor`} title="Open in the editor" onClick={(e) => { e.stopPropagation(); setEditing(null); onOpen(c.name) }}>⤢</button>}</div>
              {open ? (
                <div onClick={(e) => e.stopPropagation()}>
                  <textarea autoFocus className="w-full bg-canvas font-mono text-[12px] min-h-[3.5em] max-h-[40vh] resize-y" aria-label={`Value of ${c.name}`} spellCheck={false} value={draft} onChange={(e) => setDraft(e.target.value)}
                    onBlur={() => commit(c.name, val)}
                    onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); setDraft(val ?? ''); setEditing(null) } else if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); commit(c.name, val) } }} />
                  <div className="flex flex-wrap gap-1 mt-1" onMouseDown={(e) => e.preventDefault()}>
                    <button onClick={() => quick(c.name, { $: 'empty' })}>EMPTY</button>
                    <button onClick={() => quick(c.name, { $: 'null' })}>NULL</button>
                    <button onClick={() => quick(c.name, { $: 'default' })}>DEFAULT</button>
                    <button onClick={() => quick(c.name, { $expr: 'NOW()' })}>NOW()</button>
                    {isJson(draft) && <><button onClick={() => setDraft(prettyJson(draft))}>PRETTY</button><button onClick={() => setDraft(minify(draft))}>MINIFY</button></>}
                  </div>
                </div>
              ) : (
                <div className={`font-mono text-[12px] whitespace-pre-wrap wrap-anywhere max-h-[9em] overflow-hidden ${val === null ? 'text-ink-3' : ''}`}>{val === null ? 'NULL' : pretty ? prettyJson(val) : val}</div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
