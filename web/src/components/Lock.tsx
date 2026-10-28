// M15.37: the passcode lock. With [security] passcode set the page asks for it at start, after lock_after_mins without input, and on the `lock` action.
// The page behind is hidden (display: none and inert), not covered, so nothing is readable or focusable; it stays mounted so unsent edits survive.
// shortcut: the access token stays in sessionStorage while locked, so the lock keeps a bystander out of the page, not out of the API; the upgrade is a server-side session lock.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { api } from '../utils/api'

export const LOCK_EVENT = 'webtuidb:lock'

export function Lock({ on, afterMins, ready = true, children }: { on: boolean; afterMins: number; ready?: boolean; children: ReactNode }) {
  const [locked, setLocked] = useState(on) // locked at start
  const [pw, setPw] = useState(''), [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const input = useRef<HTMLInputElement>(null)
  useEffect(() => { if (on) setLocked(true) }, [on])
  useEffect(() => {
    if (!on) return
    const h = () => setLocked(true)
    window.addEventListener(LOCK_EVENT, h)
    return () => window.removeEventListener(LOCK_EVENT, h)
  }, [on])
  // idle timer: any mouse or key activity restarts it
  useEffect(() => {
    if (!on || locked || !afterMins) return
    let t = setTimeout(() => setLocked(true), afterMins * 60_000)
    const bump = () => { clearTimeout(t); t = setTimeout(() => setLocked(true), afterMins * 60_000) }
    const ev = ['mousemove', 'mousedown', 'keydown', 'wheel', 'touchstart']
    ev.forEach((e) => window.addEventListener(e, bump, { passive: true }))
    return () => { clearTimeout(t); ev.forEach((e) => window.removeEventListener(e, bump)) }
  }, [on, locked, afterMins])
  // while locked no key reaches the workspace's shortcuts (this listener is registered first, so it runs first)
  useEffect(() => {
    if (!locked) return
    document.documentElement.dataset.locked = '1'
    const stop = (e: KeyboardEvent) => { if (e.target !== input.current) { e.stopImmediatePropagation(); e.preventDefault() } }
    window.addEventListener('keydown', stop, true)
    input.current?.focus()
    return () => { delete document.documentElement.dataset.locked; window.removeEventListener('keydown', stop, true) }
  }, [locked])
  const unlock = async () => {
    setBusy(true); setErr('')
    try { const r = await api<{ ok: boolean }>('POST', '/passcode/check', { passcode: pw }); if (r.ok) { setPw(''); setLocked(false) } else setErr('Wrong passcode') } catch (e) { setErr(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  }
  if (!ready) return null // meta is not known yet: a passcode may be set, so the workspace does not render (or query) first
  return (
    <>
      <div className="h-full" hidden={locked} {...(locked ? { inert: true } : {})}>{children}</div>
      {locked && (
        <form className="max-w-[360px] mx-auto mt-[18vh] px-4 flex flex-col gap-3" role="dialog" aria-modal="true" aria-label="Locked" onSubmit={(e) => { e.preventDefault(); if (pw) unlock() }}>
          <h1 className="m-0 text-[28px] font-medium font-mono">Locked</h1>
          <input ref={input} type="password" autoFocus autoComplete="off" aria-label="Passcode" placeholder="Passcode" value={pw} onChange={(e) => setPw(e.target.value)} />
          {err && <div className="text-del" role="alert">{err}</div>}
          <button className="btn-primary" type="submit" disabled={!pw || busy}>Unlock</button>
        </form>
      )}
    </>
  )
}
