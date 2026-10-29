import { useImperativeHandle, useState, type CSSProperties, type Ref } from 'react'
import { Layer, MENU, at as place } from './Layer'

// Select is the app's own drop-down: a caret button that opens the same popover menu the connection
// switcher uses, so nothing in the UI falls back to the browser's native widget. The menu is fixed to
// the viewport (and flips upward near the bottom), so a scrolling dialog never clips it.
export type Opt = { value: string; label: string }
export type SelectHandle = { open: () => void }

const LIST = `${MENU} w-max min-w-[160px] max-w-[min(480px,calc(100vw-24px))] m-0`
const ITEM = 'menu-item'

type Props = {
  value: string
  options: (string | Opt)[]
  onChange: (v: string) => void
  className?: string
  id?: string
  'aria-label'?: string
  title?: string
  disabled?: boolean
  ref?: Ref<SelectHandle>
}

export function Select({ value, options, onChange, className = '', id, title, disabled, ref, ...a }: Props) {
  const [at, setAt] = useState<CSSProperties | null>(null) // null = closed
  const [btn, setBtn] = useState<HTMLButtonElement | null>(null)
  const open = () => {
    const r = btn?.getBoundingClientRect(); if (!r) return
    setAt(place(r, { width: 160, height: Math.min(320, 26 * options.length + 12) }))
  }
  useImperativeHandle(ref, () => ({ open }))
  const opts = options.map((o) => (typeof o === 'string' ? { value: o, label: o } : o))
  const cur = opts.find((o) => o.value === value)
  // arrows move between items, Escape closes; focus starts on the current item
  const onKey = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const items = [...e.currentTarget.querySelectorAll<HTMLButtonElement>('[role^=menuitem]')]
    const i = items.indexOf(document.activeElement as HTMLButtonElement)
    if (e.key === 'ArrowDown') items[(i + 1) % items.length]?.focus()
    else if (e.key === 'ArrowUp') items[(i - 1 + items.length) % items.length]?.focus()
    else if (e.key === 'Home') items[0]?.focus()
    else if (e.key === 'End') items[items.length - 1]?.focus()
    else if (e.key === 'Escape' || e.key === 'Tab') { setAt(null); btn?.focus() }
    else if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) { // type the first letter to jump
      const k = e.key.toLowerCase(), from = i + 1
      const hit = [...items.slice(from), ...items.slice(0, from)].find((b) => (b.textContent ?? '').trim().toLowerCase().startsWith(k))
      if (!hit) return
      hit.focus()
    }
    else return
    e.preventDefault(); e.stopPropagation()
  }
  return (
    <>
      <button ref={setBtn} id={id} type="button" className={`inline-flex items-center gap-1.5 px-2 ${className}`} onClick={() => (at ? setAt(null) : open())} aria-haspopup="menu" aria-expanded={!!at} disabled={disabled} aria-label={a['aria-label']} title={title}>
        <span className="flex-1 min-w-0 truncate text-left">{cur?.label ?? value}</span><span className="text-[10px] opacity-70" aria-hidden>▾</span>
      </button>
      {at && (
        <Layer onClose={() => setAt(null)}>
          <div className={LIST} style={at} role="menu" aria-label={a['aria-label']} onKeyDown={onKey}>
            {opts.map((o) => (
              <button key={o.value} type="button" role="menuitemradio" aria-checked={o.value === value} autoFocus={o.value === value} className={`${ITEM} ${o.value === value ? 'btn-on' : ''}`} onClick={() => { setAt(null); if (o.value !== value) onChange(o.value) }}>
                <span className={`w-3 flex-none text-[10px] ${o.value === value ? 'text-accent' : 'opacity-0'}`} aria-hidden>●</span><span className="flex-1 min-w-0 truncate">{o.label}</span>
              </button>
            ))}
          </div>
        </Layer>
      )}
    </>
  )
}
