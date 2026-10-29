import { useEffect, useRef } from 'react'
import { Machine, canon, contextOf, keysLocked, type Keymap } from '../utils/keys'

// useKeymap runs the prefix key state machine for a page that has no workspace (the connection list, the SQL viewer): the same bindings as everywhere else.
export function useKeymap(km: Keymap | undefined, supported: string[], run: (action: string) => void) {
  const cb = useRef(run); cb.current = run
  const names = supported.join(',')
  useEffect(() => {
    if (!km) return
    const m = new Machine(km), ok = new Set(names.split(','))
    const onKey = (e: KeyboardEvent) => {
      if (keysLocked()) return
      const k = canon(e); if (!k) return
      const was = m.pending, r = m.feed(contextOf(), k)
      if (r.passthrough || (r.action && !was && !ok.has(r.action))) return
      e.preventDefault(); e.stopPropagation()
      if (r.action && ok.has(r.action)) cb.current(r.action)
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [km, names])
}
