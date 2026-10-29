// Safe mode picker: the guard level of a connection.
import { useState } from 'react'
import { ApiError, api, type SessionInfo } from '../../utils/api'
import { Dialog, confirm } from './Dialogs'
import { ACTIONS, msg } from './ui'

const LEVELS: [string, string, string][] = [
  ['silent', 'Silent Mode', 'send queries with no warning'], ['alert1', 'Alert Mode 1', 'confirm every query'], ['alert2', 'Alert Mode 2', 'confirm all but SELECT/EXPLAIN/SHOW'],
  ['safe1', 'Safe Mode 1', 'password for every query'], ['safe2', 'Safe Mode 2', 'password for all but SELECT/EXPLAIN/SHOW'],
]

export function SafeModeDialog({ sid, info, onClose, onDone }: { sid: string; info: SessionInfo; onClose: () => void; onDone: () => void }) {
  const [pick, setPick] = useState<string | null>(null) // set when leaving a password level asks for the password
  const [pw, setPw] = useState(''), [err, setErr] = useState('')
  const go = async (level: string, password = '', confirmed = false) => {
    try { await api('POST', `/sessions/${sid}/safe-mode`, { level, password, ...(confirmed ? { confirmed } : {}) }); onDone() } catch (e) {
      if (e instanceof ApiError && e.status === 409 && e.body.gate?.confirm) { // no stored password to ask for: confirm instead
        if (!confirmed && await confirm(e.message || 'Change safe mode?', undefined, 'Change')) return go(level, password, true)
        setErr('')
      } else if (e instanceof ApiError && e.status === 409) { setPick(level); setErr(password ? 'Wrong password' : '') } else setErr(msg(e))
    }
  }
  return (
    <Dialog title={`Safe mode for ${info.name}`} onClose={onClose}>
      {pick === null ? (
        <div className="flex flex-col gap-0.5">
          {LEVELS.map(([v, label, d]) => (
            <button key={v} className={`text-left border-transparent hover:border-ink-3 flex gap-3 ${v === info.level ? 'btn-on' : ''}`} autoFocus={v === info.level} onClick={() => go(v)}>
              <span className="w-4">{v === info.level ? '•' : ''}</span><span className="w-32">{label}</span><span className="text-ink-2">{d}</span>
            </button>
          ))}
        </div>
      ) : (
        <form onSubmit={(e) => { e.preventDefault(); go(pick, pw) }}>
          <p className="text-ink-2 mt-0">Enter the database password to change safe mode.</p>
          <input type="password" autoFocus className="w-full" aria-label="Database password" value={pw} onChange={(e) => setPw(e.target.value)} />
          <div className={ACTIONS}><button type="button" onClick={() => setPick(null)}>Back</button><button className="btn-primary" type="submit" disabled={!pw}>Change</button></div>
        </form>
      )}
      {err && <div className="text-del mt-2">{err}</div>}
    </Dialog>
  )
}
