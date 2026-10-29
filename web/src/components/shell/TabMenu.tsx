// Tab menu: a small context menu of labelled actions.
import { Layer, MENU, atPoint } from './Layer'

// items: [label, run, why]; a third entry greys the item and shows it as the tooltip. "-" is a separator.
export function TabMenu({ x, y, items, onClose, label = 'Menu' }: { x: number; y: number; items: [string, () => void, string?][]; onClose: () => void; label?: string }) {
  return (
    <Layer onClose={onClose}>
      <div className={`${MENU} min-w-[200px]`} style={atPoint(x, y, { height: 30 * items.length + 16 })} role="menu" aria-label={label}
        onKeyDown={(e) => { const l = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role=menuitem]:not(:disabled)')], i = l.indexOf(document.activeElement as HTMLButtonElement); if (e.key === 'ArrowDown') l[(i + 1) % l.length]?.focus(); else if (e.key === 'ArrowUp') l[(i - 1 + l.length) % l.length]?.focus(); else if (e.key === 'Home') l[0]?.focus(); else if (e.key === 'End') l[l.length - 1]?.focus(); else if (e.key === 'Escape' || e.key === 'Tab') onClose(); else return; e.preventDefault(); e.stopPropagation() }}>
        {items.map(([text, run, why], i) => text === '-' ? <hr key={i} className="border-t border-rule my-1" /> : <button key={text} role="menuitem" autoFocus={i === 0} disabled={!!why} title={why} className={`menu-item ${/^(Delete|Drop|Truncate|Close all)/.test(text) ? 'btn-danger' : ''}`} onClick={() => { onClose(); run() }}>{text}</button>)}
      </div>
    </Layer>
  )
}
