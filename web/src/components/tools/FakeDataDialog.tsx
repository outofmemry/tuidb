// Fake data: insert generated rows into a table.
import { useState } from 'react'
import { api, type ObjRef, type Plan } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { ACTIONS, ROW, msg } from '../shell/ui'

export function FakeDataDialog({ sid, obj, onPlan, onClose, notify }: { sid: string; obj: ObjRef; onPlan: (p: Plan) => void; onClose: () => void; notify: (m: string) => void }) {
  const [n, setN] = useState('10'), [seed, setSeed] = useState('')
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const ok = Number.isInteger(Number(n)) && Number(n) >= 1 && Number(n) <= 5000 && (seed === '' || Number.isInteger(Number(seed)))
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const p = await api<Plan & { seed: number }>('POST', `/sessions/${sid}/tools/fake-rows`, { Ref: obj, N: Number(n), Seed: seed === '' ? 0 : Number(seed) })
      notify(`Fake rows planned with seed ${p.seed}`); onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title={`Fake data for ${obj.Name}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (ok && !busy) go() }}>
        <p className="text-ink-2 mt-0">Plans INSERTs with values that fit each column's type. You preview them before anything is sent.</p>
        <div className={ROW}><label className="w-28 text-ink-2">Rows (1 to 5000)</label><input autoFocus type="number" min={1} max={5000} aria-label="Rows" value={n} onChange={(e) => setN(e.target.value)} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">Seed</label><input className="flex-1 min-w-0" aria-label="Seed" placeholder="random; the same seed gives the same rows" value={seed} onChange={(e) => setSeed(e.target.value)} /></div>
        {err && <div className="text-del whitespace-pre-wrap">{err}</div>}
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button className="btn-primary" type="submit" disabled={busy || !ok}>{busy ? 'Planning…' : 'Preview inserts'}</button></div>
      </form>
    </Dialog>
  )
}
