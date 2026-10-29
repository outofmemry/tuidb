// Open Anything palette: fuzzy search over tables, tabs and actions.
import { useEffect, useRef, useState } from 'react'
import { Dialog } from './Dialogs'

// run gets true for alt+Enter: open the hit in a new tab
export type PaletteItem = { group: string; label: string; detail?: string; run: (newTab?: boolean) => void }

// match scores a query against a label: -1 no match, lower is better. `^` anchors the start, `$` the end, otherwise fuzzy (substring first, then subsequence).
export function match(q: string, s: string): number {
  let t = q.toLowerCase().trim()
  const start = t.startsWith('^'), end = t.endsWith('$')
  if (start) t = t.slice(1)
  if (end) t = t.slice(0, -1)
  const h = s.toLowerCase()
  if (!t) return 0
  if ((start && !h.startsWith(t)) || (end && !h.endsWith(t))) return -1
  if (start || end) return 0
  const at = h.indexOf(t)
  if (at >= 0) return at
  let j = 0, first = -1, last = 0
  for (let i = 0; i < h.length && j < t.length; i++) if (h[i] === t[j]) { if (first < 0) first = i; last = i; j++ }
  return j === t.length ? 1000 + last - first : -1
}

export function Palette({ items, onClose }: { items: PaletteItem[]; onClose: () => void }) {
  const [q, setQ] = useState(''), [at, setAt] = useState(0)
  const shown = items.map((it) => ({ it, s: match(q, it.label) })).filter((x) => x.s >= 0).sort((a, b) => a.s - b.s).slice(0, 60).map((x) => x.it)
  const list = useRef<HTMLDivElement>(null)
  useEffect(() => { list.current?.querySelector('[aria-selected=true]')?.scrollIntoView({ block: 'nearest' }) }, [at])
  const pick = (it?: PaletteItem, newTab = false) => { if (it) { onClose(); it.run(newTab) } }
  return (
    <Dialog title="Open anything" onClose={onClose}>
      <input autoFocus className="w-full text-[14px] py-1.5" aria-label="Search" placeholder="Tables, queries, databases, actions…" value={q}
        onChange={(e) => { setQ(e.target.value); setAt(0) }}
        onKeyDown={(e) => {
          if (e.key === 'ArrowDown') { e.preventDefault(); setAt(Math.min(shown.length - 1, at + 1)) }
          else if (e.key === 'ArrowUp') { e.preventDefault(); setAt(Math.max(0, at - 1)) }
          else if (e.key === 'Enter') { e.preventDefault(); pick(shown[at], e.altKey) }
          else if (e.key === 'Tab' && shown[at]) { e.preventDefault(); setQ(shown[at].label); setAt(0) } // complete the keyword with the selected name
        }} />
      <div ref={list} className="mt-2 max-h-[50vh] overflow-auto font-mono text-[12.5px]" role="listbox">
        {shown.map((it, i) => (
          <div key={it.group + it.label + i} role="option" aria-selected={i === at} className={`flex items-center gap-3 px-2 py-1 rounded-[2px] cursor-pointer ${i === at ? 'bg-raised shadow-[inset_2px_0_0_var(--accent)]' : 'hover:bg-panel'}`} onMouseEnter={() => setAt(i)} onClick={() => pick(it)}>
            <span className={`w-20 flex-none text-[11px] ${it.group === 'action' || it.group === 'feature' ? 'text-accent' : 'text-ink-3'}`}>{it.group}</span><span className="flex-1 min-w-0 truncate">{it.label}</span>{it.detail && <span className="text-ink-3 text-[11.5px] truncate max-w-[40%]">{it.detail}</span>}
          </div>
        ))}
        {!shown.length && <div className="p-2 text-ink-2">Nothing matches.</div>}
      </div>
      <div className="flex gap-3 mt-2 text-[11.5px] text-ink-3 font-mono"><span><kbd>↑</kbd> <kbd>↓</kbd> move</span><span><kbd>Enter</kbd> open</span><span><kbd>Alt+Enter</kbd> new tab</span><span><kbd>Tab</kbd> complete</span><span><kbd>^</kbd> starts with, <kbd>$</kbd> ends with</span></div>
    </Dialog>
  )
}
