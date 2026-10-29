// Search in database: find values across tables.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, qs, watchJob, type DbObject, type FilterSet, type Job, type ObjRef, type SessionInfo } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

const OPS = ['Contains', 'Not contains', '=', 'Has prefix', 'Has suffix']

export function SearchDialog({ sid, info, onClose, onOpen }: { sid: string; info: SessionInfo; onClose: () => void; onOpen: (ref: ObjRef, f: FilterSet) => void }) {
  const base = `/sessions/${sid}`
  const [value, setValue] = useState('')
  const [op, setOp] = useState('Contains')
  const [some, setSome] = useState(false)
  const [picked, setPicked] = useState<string[]>([])
  const [job, setJob] = useState<Job | null>(null)
  const [id, setId] = useState('')
  const [err, setErr] = useState('')
  const objs = useQuery({ queryKey: ['objects', sid, info.database, info.schema], queryFn: () => api<DbObject[]>('GET', `${base}/objects` + qs({ schema: info.schema })), gcTime: 0 })
  const tables = (objs.data ?? []).filter((o) => o.Kind === 'table').map((o) => o.Name)
  const abort = useRef<AbortController | null>(null)
  useEffect(() => () => abort.current?.abort(), [])
  const run = async () => {
    setErr(''); setJob(null); setPaused(false)
    try {
      const r = await api<{ job: string; total: number }>('POST', `${base}/search`, { Value: value, Op: op, Schema: info.schema, Tables: some ? picked : [] })
      setId(r.job); setJob({ done: false, error: '', rows: 0, batches: 0, total: r.total, checked: 0, hits: [], lines: [] })
      const ac = abort.current = new AbortController()
      watchJob(sid, r.job, setJob, ac.signal).catch((e) => { if (!ac.signal.aborted) setErr(msg(e)) })
    } catch (e) { setErr(msg(e)) }
  }
  const stop = () => { api('DELETE', `${base}/jobs/${id}`).catch(() => {}) }
  const [paused, setPaused] = useState(false)
  const pause = () => api('POST', `${base}/search/${id}/${paused ? 'resume' : 'pause'}`).then(() => setPaused(!paused), (e) => setErr(msg(e)))
  const running = !!job && !job.done
  return (
    <Dialog title="Search in database" onClose={() => { stop(); onClose() }}>
      <div className={ROW}>
        <input autoFocus className="flex-1 min-w-0" aria-label="Value" placeholder="Value" value={value} onChange={(e) => setValue(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && value && !running) run() }} />
        <Select aria-label="Operator" value={op} onChange={setOp} options={OPS} />
      </div>
      <div className={ROW}>
        <label className="flex items-center gap-1.5"><input type="radio" checked={!some} onChange={() => setSome(false)} />All tables</label>
        <label className="flex items-center gap-1.5"><input type="radio" checked={some} onChange={() => setSome(true)} />Selected tables</label>
      </div>
      {some && <div className="border border-rule max-h-[140px] overflow-auto p-1.5 text-[12.5px]">
        {tables.map((t) => <label key={t} className="flex items-center gap-1.5"><input type="checkbox" checked={picked.includes(t)} onChange={(e) => setPicked(e.target.checked ? [...picked, t] : picked.filter((x) => x !== t))} />{t}</label>)}
      </div>}
      {job && <div className="mt-3 text-[12.5px]" role="status">{job.checked}/{job.total} tables{running ? '…' : job.hits.length ? '' : '. No matches.'}</div>}
      {job && job.hits.length > 0 && <div className="mt-1 border border-rule max-h-[240px] overflow-auto">
        {job.hits.map((h) => (
          <button key={h.ref.Schema + '.' + h.ref.Name} className="flex w-full justify-between gap-3 text-left border-transparent border-b-rule" disabled={!!h.error} title={h.error} onClick={() => { onOpen(h.ref, h.filters); onClose() }}>
            <span className="truncate">{h.ref.Name}</span><span className={h.error ? 'text-del' : 'text-ink-2'}>{h.error ? 'failed' : `${h.count} row${h.count === 1 ? '' : 's'}`}</span>
          </button>))}
      </div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}>
        <button onClick={() => { stop(); onClose() }}>Close</button>
        {running ? <><button onClick={pause}>{paused ? 'Resume' : 'Pause'}</button><button onClick={stop}>Stop</button></> : <button className="btn-primary" disabled={!value || (some && !picked.length)} onClick={run}>Search</button>}
      </div>
    </Dialog>
  )
}
