import { useState, type ReactNode } from 'react'
import { ApiError, api, type Gate, type Plan } from './api'

export function Dialog({ title, children, onClose }: { title: string; children: ReactNode; onClose: () => void }) {
  return (
    <div className="dialog-bg" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="dialog" role="dialog" aria-label={title} onKeyDown={(e) => e.key === 'Escape' && onClose()}>
        <h2>{title}</h2>
        {children}
      </div>
    </div>
  )
}

export function GateFields({ gate, label, password, setPassword }: { gate: Gate; label: string; password: string; setPassword: (s: string) => void }) {
  if (!gate.confirm && !gate.password) return null
  return (
    <div style={{ marginTop: 10 }}>
      <div className="dim">{label}: {gate.password ? 'enter the database password to continue' : 'check the statements above, then continue'}</div>
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
      {!plan.transactions && <div className="dim">This database applies statements one at a time. If one fails, the ones before it stay applied.</div>}
      <GateFields gate={plan.gate} label={plan.levelLabel} password={password} setPassword={setPassword} />
      {err && <div className="err" style={{ marginTop: 8 }}>{err}</div>}
      <div className="actions">
        <button onClick={onClose}>Cancel</button>
        <button className="primary" disabled={busy || (plan.gate.password && !password)} onClick={send}>Send changes</button>
      </div>
    </Dialog>
  )
}
