import { useEffect, useState, type ReactNode } from 'react'
import { ApiError, api, type Gate, type Plan } from '../utils/api'

// openSession opens a saved connection. A password = "ask" connection answers 422 passwordRequired: the password is asked for here and the call retried.
export async function openSession(name: string): Promise<{ id: string }> {
  try { return await api<{ id: string }>('POST', '/sessions', { Name: name }) } catch (e) {
    if (!(e instanceof ApiError && e.status === 422 && e.body.passwordRequired)) throw e
    const pw = await askSecret(`Password for ${name}`)
    if (pw === null) throw new Error('Cancelled')
    return api<{ id: string }>('POST', '/sessions', { Name: name, Password: pw })
  }
}

export function Dialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  return (
    <div className="fixed inset-0 bg-backdrop flex items-center justify-center z-50 max-[760px]:items-stretch" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="popover px-5 py-[18px] w-[min(680px,94vw)] max-h-[88vh] overflow-auto max-[760px]:w-full max-[760px]:max-h-none max-[760px]:h-full max-[760px]:rounded-none max-[760px]:px-4 [&_input]:bg-canvas [&_select]:bg-canvas [&_pre]:m-0 [&_pre]:bg-canvas [&_pre]:border [&_pre]:border-rule [&_pre]:rounded-[2px] [&_pre]:px-3 [&_pre]:py-[10px] [&_pre]:max-h-[40vh] [&_pre]:overflow-auto [&_pre]:text-[12px] [&_pre]:whitespace-pre-wrap" role="dialog" aria-label={title} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
        <h2 className="m-0 mb-[14px] text-[18px]">{title}</h2>
        {children}
      </div>
    </div>
  )
}

// ask / confirm replace window.prompt and window.confirm with the app's own dialog. <Prompts/> is
// mounted once at the root and renders whatever request is pending.
type Req = { kind: 'prompt' | 'confirm'; title: string; init: string; secret?: boolean; resolve: (v: string | null | boolean) => void }
let pending: ((r: Req | null) => void) | null = null
export const ask = (title: string, init = '') => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init, resolve: resolve as Req['resolve'] }))
export const askSecret = (title: string) => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init: '', secret: true, resolve: resolve as Req['resolve'] }))
export const confirm = (title: string) => new Promise<boolean>((resolve) => pending?.({ kind: 'confirm', title, resolve: resolve as Req['resolve'], init: '' }))

export function Prompts() {
  const [req, setReq] = useState<Req | null>(null)
  const [val, setVal] = useState('')
  useEffect(() => { pending = (r) => { setReq(r); setVal(r?.init ?? '') }; return () => { pending = null } }, [])
  if (!req) return null
  const done = (v: string | null | boolean) => { setReq(null); req.resolve(v) }
  const cancel = () => done(req.kind === 'prompt' ? null : false)
  const ok = () => done(req.kind === 'prompt' ? val : true)
  return (
    <Dialog title={req.title} onClose={cancel}>
      {req.kind === 'prompt' && <input autoFocus className="w-full" type={req.secret ? 'password' : 'text'} autoComplete="off" value={val} onChange={(e) => setVal(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); ok() } }} />}
      <div className="flex justify-end gap-2 mt-[18px]">
        <button onClick={cancel}>Cancel</button>
        <button className="btn-primary" autoFocus={req.kind === 'confirm'} disabled={req.kind === 'prompt' && !val.trim()} onClick={ok}>{req.kind === 'prompt' ? 'OK' : 'Continue'}</button>
      </div>
    </Dialog>
  )
}

export function GateFields({ gate, label, password, setPassword }: { gate: Gate; label: string; password: string; setPassword: (s: string) => void }) {
  if (!gate.confirm && !gate.password) return null
  return (
    <div style={{ marginTop: 10 }}>
      <div className="text-ink-2">{label}: {gate.password ? 'enter the database password to continue' : 'check the statements above, then continue'}</div>
      {gate.password && (
        <input type="password" autoFocus placeholder="database password" value={password} onChange={(e) => setPassword(e.target.value)} style={{ marginTop: 6, width: '100%' }} />
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
      {err && <div className="text-del" style={{ marginTop: 8 }}>{err}</div>}
      <div className="flex justify-end gap-2 mt-[18px]">
        <button onClick={onClose}>Cancel</button>
        <button className="btn-primary" disabled={busy || (plan.gate.password && !password)} onClick={send}>Send changes</button>
      </div>
    </Dialog>
  )
}
