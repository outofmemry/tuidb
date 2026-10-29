import { useEffect, useRef, useState, type ReactNode } from 'react'
import { ApiError, api, type Gate, type Plan } from '../../utils/api'
import { openInWindow } from '../../utils/binary'
import { askSecret, confirm, setPromptHost, type PromptReq } from '../../utils/prompts'
import { layersOpen } from './Layer'

export { ask, askSecret, confirm } from '../../utils/prompts'

// openSession opens a saved connection. A password = "ask" connection answers 422 passwordRequired: the password is asked for here and the call retried.
export async function openSession(name: string): Promise<{ id: string }> {
  const open = async (body: Record<string, string>) => {
    const r = await api<{ id: string; bootstrapErrors?: string[] }>('POST', '/sessions', body)
    // the terminal's "Bootstrap command failed (the connection is still open)" dialog; shortcut: a confirm box with the text, OK or Cancel both go on
    if (r.bootstrapErrors?.length) await confirm('Bootstrap command failed (the connection is still open)', r.bootstrapErrors.join('\n'), 'OK')
    return r
  }
  try { return await open({ Name: name }) } catch (e) {
    if (!(e instanceof ApiError && e.status === 422 && e.body.passwordRequired)) throw e
    const pw = await askSecret(`Password for ${name}`)
    if (pw === null) throw new Error('Cancelled')
    return open({ Name: name, Password: pw })
  }
}

const FOCUSABLE = 'a[href], button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [tabindex]:not([tabindex="-1"])'

const stack: object[] = [] // the open dialogs, the top-most last: Escape closes only that one

export function Dialog({ title, children, onClose, wide }: { title: string; children: ReactNode; onClose: () => void; wide?: boolean }) {
  const box = useRef<HTMLDivElement>(null)
  const close = useRef(onClose); close.current = onClose
  // focus moves into the dialog (unless a field took it with autoFocus), Tab stays inside it, and closing returns focus to where it was
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null
    const el = box.current
    if (el && !el.contains(document.activeElement)) (el.querySelector<HTMLElement>(FOCUSABLE) ?? el).focus()
    const me = {}
    stack.push(me)
    // Escape is heard on the window, not on the box: when Save disables the focused button the focus falls to <body>, where the box hears nothing.
    // An open menu (Layer) takes Escape first; a field that used it (preventDefault) keeps it.
    const key = (e: KeyboardEvent) => {
      if (stack[stack.length - 1] !== me || e.defaultPrevented || layersOpen()) return
      if (e.key === 'Escape') { e.preventDefault(); close.current() }
      else if (e.key === 'Tab' && el && !el.contains(document.activeElement)) { e.preventDefault(); (el.querySelector<HTMLElement>(FOCUSABLE) ?? el).focus() }
    }
    window.addEventListener('keydown', key)
    return () => { window.removeEventListener('keydown', key); stack.splice(stack.indexOf(me), 1); if (before?.isConnected) before.focus() }
  }, [])
  useEffect(() => { // the focused control went away or was disabled (Save while saving): keep the focus inside the dialog
    const el = box.current, a = document.activeElement
    if (el && (!a || a === document.body)) el.focus()
  })
  const trap = (e: React.KeyboardEvent<HTMLDivElement>) => {
    if (e.key !== 'Tab') return
    const l = [...e.currentTarget.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((x) => x.getClientRects().length)
    if (!l.length) return
    const first = l[0], last = l[l.length - 1], at = document.activeElement
    if (e.shiftKey && (at === first || at === e.currentTarget)) { e.preventDefault(); last.focus() } else if (!e.shiftKey && at === last) { e.preventDefault(); first.focus() }
  }
  return (
    <div className="fixed inset-0 bg-backdrop flex items-center justify-center z-50 motion-safe:animate-fade max-[760px]:items-stretch" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className={`popover motion-safe:animate-pop px-5 py-[18px] focus:outline-none ${wide ? 'w-[min(960px,94vw)]' : 'w-[min(680px,94vw)]'} max-h-[88vh] overflow-auto max-[760px]:w-full max-[760px]:max-h-none max-[760px]:h-full max-[760px]:rounded-none max-[760px]:px-4 [&_input]:bg-canvas [&_select]:bg-canvas [&_pre]:m-0 [&_pre]:bg-canvas [&_pre]:border [&_pre]:border-rule [&_pre]:rounded-[2px] [&_pre]:px-3 [&_pre]:py-[10px] [&_pre]:max-h-[40vh] [&_pre]:overflow-auto [&_pre]:text-[12px] [&_pre]:whitespace-pre-wrap`} role="dialog" aria-modal="true" aria-label={title} ref={box} tabIndex={-1} onKeyDown={trap}>
        <div className="flex items-start gap-3 mb-[14px]">
          <h2 className="flex-1 min-w-0 m-0 text-[18px] leading-tight [overflow-wrap:anywhere]">{title}</h2>
          <button className="btn-ghost btn-icon -mr-1.5 -mt-0.5 text-ink-3 hover:text-ink" onClick={onClose} aria-label="Close" title="Close (Esc)">✕</button>
        </div>
        {children}
      </div>
    </div>
  )
}

// Prompts renders the pending ask / confirm (utils/prompts.ts); it is mounted once at the root.
export function Prompts() {
  const [req, setReq] = useState<PromptReq | null>(null)
  const [val, setVal] = useState('')
  useEffect(() => { setPromptHost((r) => { setReq(r); setVal(r?.init ?? '') }); return () => setPromptHost(null) }, [])
  if (!req) return null
  const done = (v: string | null | boolean) => { setReq(null); req.resolve(v) }
  const cancel = () => done(req.kind === 'prompt' ? null : false)
  const ok = () => done(req.kind === 'prompt' ? val : true)
  return (
    <Dialog title={req.title} onClose={cancel}>
      {req.text && <pre className="m-0 mb-2 max-h-[40vh] overflow-auto whitespace-pre-wrap wrap-anywhere text-[12px]">{req.text}</pre>}
      {req.kind === 'prompt' && <input autoFocus className="w-full" type={req.secret ? 'password' : 'text'} autoComplete="off" value={val} onChange={(e) => setVal(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); ok() } }} />}
      <div className="flex justify-end gap-2 mt-[18px]">
        <button onClick={cancel}>{req.no ?? 'Cancel'}</button>
        <button className="btn-primary" autoFocus={req.kind === 'confirm'} disabled={req.kind === 'prompt' && !val.trim()} onClick={ok}>{req.ok ?? (req.kind === 'prompt' ? 'OK' : 'Continue')}</button>
      </div>
    </Dialog>
  )
}

export function GateFields({ gate, label, password, setPassword }: { gate: Gate; label: string; password: string; setPassword: (s: string) => void }) {
  if (!gate.confirm && !gate.password) return null
  return (
    <div className="mt-2.5">
      <div className="text-ink-2"><span className="chip mr-1.5">{label}</span>{gate.password ? 'enter the database password to continue' : 'check the statements above, then continue'}</div>
      {gate.password && (
        <input type="password" autoFocus placeholder="database password" aria-label="Database password" value={password} onChange={(e) => setPassword(e.target.value)} className="mt-1.5 w-full" />
      )}
    </div>
  )
}

// PlanDialog previews statements the server planned and applies them. The server re-checks the
// safe-mode gate on apply, so a wrong password or missing confirmation comes back as a 409 here.
export function PlanDialog({ sid, plan, onClose, onApplied }: { sid: string; plan: Plan; onClose: () => void; onApplied: () => void }) {
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const send = async () => {
    setBusy(true); setErr('')
    try {
      await api('POST', `/sessions/${sid}/apply`, { planId: plan.planId, confirmed: true, password })
      onApplied()
    } catch (e) {
      setErr(e instanceof ApiError ? e.message : String(e))
      setBusy(false)
    }
  }
  return (
    <Dialog title={`Send ${plan.statements.length} statement${plan.statements.length === 1 ? '' : 's'}?`} onClose={onClose}>
      <pre>{plan.statements.map((s) => s.display).join('\n')}</pre>
      {!plan.transactions && <div className="text-ink-2">This database applies statements one at a time. If one fails, the ones before it stay applied.</div>}
      <GateFields gate={plan.gate} label={plan.levelLabel} password={password} setPassword={setPassword} />
      {err && <div className="text-del mt-2" role="alert">{err}</div>}
      <div className="flex justify-end gap-2 mt-[18px]">
        <button className="btn-ghost" onClick={() => openInWindow('SQL preview', plan.statements.map((s) => s.display).join('\n'))} title="Open the SQL preview in its own window">Open in window</button>
        <button onClick={onClose}>Cancel</button>
        <button className="btn-primary" disabled={busy || (plan.gate.password && !password)} onClick={send}>{busy ? 'Sending…' : 'Send changes'}</button>
      </div>
    </Dialog>
  )
}
