import { useEffect, useRef, type CSSProperties, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

// Layer is the one place a floating menu renders: a portal to <body>, so it sits above every dialog, drawer and
// sticky header whatever its opener's stacking context, with a backdrop that swallows the outside click
// (the way the OS menus do, so nothing underneath is pressed by accident). It closes on that click, on a
// right-click outside, on Escape wherever the focus is, and when the window loses focus or changes size.
let open = 0
export const layersOpen = () => open > 0 // a dialog under an open menu leaves Escape to the menu

export function Layer({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const close = useRef(onClose); close.current = onClose
  useEffect(() => {
    open++
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape' && !e.defaultPrevented) { e.preventDefault(); close.current() } }
    const off = () => close.current()
    window.addEventListener('keydown', key)
    window.addEventListener('blur', off)
    window.addEventListener('resize', off)
    return () => { open--; window.removeEventListener('keydown', key); window.removeEventListener('blur', off); window.removeEventListener('resize', off) }
  }, [])
  return createPortal(
    <>
      <div className="fixed inset-0 z-[70]" onMouseDown={onClose} onContextMenu={(e) => { e.preventDefault(); onClose() }} aria-hidden />
      {children}
    </>,
    document.body,
  )
}

// MENU is the panel of a menu inside a Layer: fixed to the viewport, above the backdrop.
export const MENU = 'popover motion-safe:animate-pop fixed z-[71] overflow-auto p-1.5 font-mono text-[12.5px]'

// at places a menu under an anchor (its opener's rectangle), flipping above it when the bottom of the window is near,
// and keeps it inside the window sideways.
export function at(r: { left: number; right: number; top: number; bottom: number }, opts: { width?: number; height?: number; alignRight?: boolean } = {}): CSSProperties {
  const w = opts.width ?? 240, h = opts.height ?? 260, gap = 4
  const left = Math.max(4, Math.min(opts.alignRight ? r.right - w : r.left, window.innerWidth - w - 4))
  const below = window.innerHeight - r.bottom
  if (below < h && r.top > below) return { left, bottom: window.innerHeight - r.top + gap, maxHeight: r.top - gap - 4 }
  return { left, top: r.bottom + gap, maxHeight: below - gap - 4 }
}

// atPoint places a context menu at the pointer, kept inside the window.
export function atPoint(x: number, y: number, opts: { width?: number; height?: number } = {}): CSSProperties {
  const w = opts.width ?? 240, h = Math.min(opts.height ?? 260, window.innerHeight * 0.8)
  return { left: Math.max(4, Math.min(x, window.innerWidth - w - 4)), top: Math.max(4, Math.min(y, window.innerHeight - h - 4)), maxHeight: 'calc(100vh - 8px)' }
}
