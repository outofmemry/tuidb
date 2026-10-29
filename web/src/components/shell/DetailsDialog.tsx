// Connection details: colour and notes of the open connection.
import { Fragment, useState } from 'react'
import { api, type SessionInfo } from '../../utils/api'
import { Dialog } from './Dialogs'
import { ACTIONS, msg } from './ui'

const SWATCHES = ['#7aa2f7', '#4fc4a0', '#e0af68', '#ff9a5c', '#f7768e', '#bb9af7']

export function DetailsDialog({ sid, info, onClose, onSaved }: { sid: string; info: SessionInfo; onClose: () => void; onSaved: () => void }) {
  const [color, setColor] = useState(info.color || ''), [err, setErr] = useState('')
  const tls = [info.sslMode || 'default', info.ssh ? 'through an SSH tunnel' : ''].filter(Boolean).join(', ')
  const server = [info.host && `${info.host}${info.port ? ':' + info.port : ''}`].filter(Boolean).join('')
  const rows: [string, string][] = [['Connection', info.name], ['Driver', info.driver], ['Server version', info.serverInfo], ['Host', server], [info.caps.DatabaseNoun ? info.caps.DatabaseNoun[0].toUpperCase() + info.caps.DatabaseNoun.slice(1) : 'Database', info.database], ['Schema', info.schema], ['User', info.user ?? ''], ['TLS', info.host ? tls : ''], ['Tag', info.tag], ['Safe mode', info.levelLabel], ['Encoding', info.encoding ?? ''], ['Row limit', String(info.rowLimit)]]
  const save = async () => {
    try { await api('POST', `/sessions/${sid}/color`, { color }); onSaved() } catch (e) { setErr(msg(e)) }
  }
  return (
    <Dialog title="Connection details" onClose={onClose}>
      <div className="grid grid-cols-[8em_1fr] gap-x-3 gap-y-1 font-mono text-[12.5px]">{rows.filter(([, v]) => v).map(([k, v]) => <Fragment key={k}><span className="text-ink-2">{k}</span><span className="wrap-anywhere">{v}</span></Fragment>)}</div>
      <div className="flex items-center gap-2 mt-4 text-[12.5px]"><span className="w-[8em] text-ink-2 font-mono">Colour</span>
        {SWATCHES.map((c) => <button key={c} className={`w-5 h-5 p-0 rounded-[2px] ${color === c ? 'outline outline-2 outline-ink' : ''}`} style={{ background: c }} aria-label={`Use ${c}`} onClick={() => setColor(c)} />)}
        <input type="color" aria-label="Pick a colour" className="w-8 h-6 p-0" value={/^#[0-9a-f]{6}$/i.test(color) ? color : '#7aa2f7'} onChange={(e) => setColor(e.target.value)} />
        <button className="btn-ghost" disabled={!color} onClick={() => setColor('')}>Default</button></div>
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Close</button><button className="btn-primary" disabled={color === (info.color || '')} onClick={save}>Save colour</button></div>
    </Dialog>
  )
}
