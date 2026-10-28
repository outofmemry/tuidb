import { useEffect, useRef } from 'react'
import type { MenuItem } from '../utils/m15_menus'

export type Entry = MenuItem & { disabled?: boolean }

// ItemMenu is the context menu of the grid, results and column headers (items come from meta.menus).
// Roving tabindex: one item is in the tab order, the arrows, Home and End move it, Esc closes and returns the focus to where it was.
export function ItemMenu({ x, y, items, label, onPick, onClose }: { x: number; y: number; items: Entry[]; label: string; onPick: (it: Entry) => void; onClose: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const was = document.activeElement as HTMLElement | null
    box.current?.querySelector<HTMLElement>('[role=menuitem]:not([aria-disabled=true])')?.focus()
    return () => { if (was && document.contains(was)) was.focus() }
  }, [])
  const key = (e: React.KeyboardEvent) => {
    const l = [...(box.current?.querySelectorAll<HTMLElement>('[role=menuitem]:not([aria-disabled=true])') ?? [])]
    const i = l.indexOf(document.activeElement as HTMLElement)
    let n = -1
    if (e.key === 'ArrowDown') n = (i + 1) % l.length
    else if (e.key === 'ArrowUp') n = (i - 1 + l.length) % l.length
    else if (e.key === 'Home') n = 0
    else if (e.key === 'End') n = l.length - 1
    else if (e.key === 'Escape') { e.preventDefault(); e.stopPropagation(); return onClose() }
    else if (e.key === 'Tab') { e.preventDefault(); return onClose() }
    else return
    e.preventDefault(); e.stopPropagation()
    l.forEach((b, j) => { b.tabIndex = j === n ? 0 : -1 }); l[n]?.focus()
  }
  return (
    <>
      <div className="fixed inset-0 z-40" onClick={onClose} onContextMenu={(e) => { e.preventDefault(); onClose() }} aria-hidden />
      <div ref={box} className="popover motion-safe:animate-pop fixed z-[41] min-w-[220px] max-h-[80vh] overflow-auto p-1.5 font-mono text-[12.5px]" role="menu" aria-label={label} onKeyDown={key}
        style={{ left: Math.max(4, Math.min(x, window.innerWidth - 260)), top: Math.max(4, Math.min(y, window.innerHeight - Math.min(window.innerHeight * 0.8, 30 * items.length + 16) - 4)) }}>
        {items.map((it, i) => (
          <button key={it.action + it.label} role="menuitem" tabIndex={i === 0 ? 0 : -1} aria-disabled={it.disabled || undefined} title={it.disabled ? it.why : undefined}
            className={`menu-item ${it.disabled ? 'opacity-50 cursor-not-allowed' : ''} ${/^(Delete|Drop|Truncate)/.test(it.label) ? 'btn-danger' : ''}`}
            onFocus={(e) => { const b = e.currentTarget; b.parentElement?.querySelectorAll<HTMLElement>('[role=menuitem]').forEach((o) => { o.tabIndex = o === b ? 0 : -1 }) }}
            onClick={() => { if (it.disabled) return; onClose(); onPick(it) }}>{it.label}</button>
        ))}
      </div>
    </>
  )
}
