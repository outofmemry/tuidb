// Schema diff: compare two schemas; the server returns a plan that opens in the preview dialog.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type Plan, type SessionInfo } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

export function SchemaDiffDialog({ sid, info, onPlan, onClose }: { sid: string; info: SessionInfo; onPlan: (p: Plan) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['schemas', sid], queryFn: () => api<{ schemas: string[]; current: string }>('GET', `/sessions/${sid}/schemas`), gcTime: 0 })
  const [from, setFrom] = useState(info.schema), [to, setTo] = useState('')
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false), [same, setSame] = useState(false)
  const schemas = q.data?.schemas ?? []
  const go = async () => {
    setBusy(true); setErr(''); setSame(false)
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/tools/schema-diff`, { From: from, To: to })
      if (!p.statements.length) { setSame(true); setBusy(false); return }
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title="Schema diff" onClose={onClose}>
      <p className="text-ink-2 mt-0">Plans the statements that make the first schema look like the second: tables, columns, indexes and foreign keys. You see them before anything is sent.</p>
      {q.error && <div className="text-del">{msg(q.error)}</div>}
      {!info.caps.Schemas && <div className="text-ink-2">{info.driver} has no schemas to compare.</div>}
      <div className={ROW}><label className="w-28 text-ink-2">Change schema</label><Select aria-label="From schema" value={from} onChange={setFrom} options={schemas} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">to look like</label><Select aria-label="To schema" value={to} onChange={setTo} options={schemas.filter((s) => s !== from)} /></div>
      {same && <div className="text-ink-2">No differences: the two schemas have the same structure.</div>}
      {err && <div className="text-del whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={busy || !from || !to || from === to} onClick={go}>{busy ? 'Comparing…' : 'Plan changes'}</button></div>
    </Dialog>
  )
}
