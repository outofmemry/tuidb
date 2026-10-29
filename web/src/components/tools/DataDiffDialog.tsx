// Data diff: compare two tables; the server returns a plan that opens in the preview dialog.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, qs, type DbObject, type ObjRef, type Plan, type SessionInfo } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

const refKey = (r: ObjRef) => JSON.stringify([r.Schema, r.Name])
const refOf = (k: string): ObjRef => { const [Schema, Name] = JSON.parse(k); return { Schema, Name, Kind: 'table' } }

export function DataDiffDialog({ sid, info, onPlan, onClose }: { sid: string; info: SessionInfo; onPlan: (p: Plan) => void; onClose: () => void }) {
  const objs = useQuery({ queryKey: ['objects', sid, info.database, info.schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema: info.schema })), gcTime: 0 })
  const tables = (objs.data ?? []).filter((o) => o.Kind === 'table').map((o) => ({ value: refKey(o), label: o.Name }))
  const [from, setFrom] = useState(''), [to, setTo] = useState(''), [pk, setPk] = useState('')
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false), [none, setNone] = useState(false)
  const go = async () => {
    setBusy(true); setErr(''); setNone(false)
    try {
      const p = await api<Plan & { changes: number }>('POST', `/sessions/${sid}/tools/data-diff`, { From: refOf(from), To: refOf(to), PK: pk.split(',').map((x) => x.trim()).filter(Boolean) })
      if (!p.changes) { setNone(true); setBusy(false); return }
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title="Data diff" onClose={onClose}>
      <p className="text-ink-2 mt-0">Plans the inserts, updates and deletes that make the first table hold the same rows as the second, matched by primary key.</p>
      <div className={ROW}><label className="w-28 text-ink-2">Change table</label><Select aria-label="From table" value={from} onChange={setFrom} options={tables} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">to match</label><Select aria-label="To table" value={to} onChange={setTo} options={tables.filter((t) => t.value !== from)} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">Key columns</label><input className="flex-1 min-w-0" aria-label="Key columns" placeholder="the primary key of the first table" value={pk} onChange={(e) => setPk(e.target.value)} /></div>
      {none && <div className="text-ink-2">The tables hold the same rows.</div>}
      {err && <div className="text-del whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={busy || !from || !to} onClick={go}>{busy ? 'Comparing…' : 'Plan changes'}</button></div>
    </Dialog>
  )
}
