// Process list: running sessions of the server, with kill.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, saveBlob, type Gate, type Meta, type SessionInfo } from '../../utils/api'
import { copyText, formatRows, type CopyFormat } from '../../utils/copy'
import { filterMenu, whyNot, type MenuItem } from '../../utils/menus'
import { sortProcs, type ProcRow } from '../../utils/processes'
import { CellEditor } from '../grid/CellEditor'
import { Dialog, GateFields, confirm } from '../shell/Dialogs'
import { TabMenu } from '../shell/TabMenu'
import { CopyAsDialog } from '../query/CopyAsDialog'
import { ACTIONS, msg } from '../shell/ui'

type Proc = ProcRow

const PROC_MENU: MenuItem[] = [{ label: 'Kill selected', action: 'kill' }, { label: 'Quick look', action: 'quick_look' }, { label: 'Copy as…', action: 'copy_rows_as' }, { label: 'Export…', action: 'export' }, { label: 'Dismiss', action: 'close' }]

export function ProcessDialog({ sid, info, onClose, notify }: { sid: string; info: SessionInfo; onClose: () => void; notify: (m: string) => void }) {
  const q = useQuery({ queryKey: ['procs', sid], queryFn: () => api<{ supported: boolean; processes: Proc[] }>('GET', `/sessions/${sid}/processes`), gcTime: 0 })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const menu = filterMenu(meta?.menus?.process_list, {}).length ? filterMenu(meta?.menus?.process_list, {}) : PROC_MENU // the labels come from the shared list (menus.ProcessList)
  const label = (a: string) => menu.find((m) => m.action === a)?.label ?? a
  const [by, setBy] = useState('elapsed'), [desc, setDesc] = useState(true) // sort column; elapsed puts the slowest first
  const [sel, setSel] = useState<Set<string>>(new Set()) // multi-select for Kill selected, Copy as and Export
  const [look, setLook] = useState<string | null>(null) // quick look at a query
  const [copyAs, setCopyAs] = useState(false), [ctx, setCtx] = useState<{ x: number; y: number; id: string } | null>(null)
  const [kill, setKill] = useState<{ ids: string[]; gate: Gate } | null>(null)
  const [password, setPassword] = useState('')
  const [err, setErr] = useState('')
  // kills the ids one after the other; a safe-mode gate stops the loop and asks, the rest follows once confirmed
  const send = async (ids: string[], confirmed: boolean) => {
    setErr(''); let n = 0
    for (const id of ids) {
      try { await api('POST', `/sessions/${sid}/processes/kill`, { ID: id, Confirmed: confirmed, Password: password }); n++ } catch (e) {
        if (e instanceof ApiError && e.status === 409 && e.body.gate) { setKill({ ids: ids.slice(n), gate: e.body.gate }); if (confirmed) setErr(e.message) } else setErr(msg(e))
        break
      }
    }
    if (n === ids.length) { setKill(null); setPassword('') }
    if (n) { notify(n === 1 ? `Killed process ${ids[0]}` : `Killed ${n} processes`); setSel((s) => new Set([...s].filter((x) => !ids.slice(0, n).includes(x)))); q.refetch() }
  }
  const procs = sortProcs(q.data?.processes ?? [], by, desc)
  const chosen = sel.size ? procs.filter((p) => sel.has(p.id)) : procs // no selection = every row
  const header = (k: string) => { if (k === by) setDesc(!desc); else { setBy(k); setDesc(k === 'elapsed') } }
  const rowsOf = (l: Proc[]) => l.map((p) => [p.id, p.user, p.db, p.state, String(p.elapsedMs), p.query])
  const COLS = ['id', 'user', 'db', 'state', 'elapsed_ms', 'query'].map((name) => ({ name, type: 'text' }))
  const copyRows = (f: CopyFormat) => { setCopyAs(false); copyText(formatRows(f, COLS, rowsOf(chosen), meta?.config.csv ?? { Delimiter: ',', Quote: '"', LineBreak: '\n', Decimal: '.' }, info.caps.Dialect, 'processes')).then(() => notify(`Copied ${chosen.length} processes as ${f}`), (e) => notify(msg(e))) }
  const exportPage = () => saveBlob(new Blob([formatRows('CSV with Header', COLS, rowsOf(chosen), meta?.config.csv ?? { Delimiter: ',', Quote: '"', LineBreak: '\n', Decimal: '.' }, info.caps.Dialect) + '\n'], { type: 'text/csv;charset=utf-8' }), 'processes.csv')
  const askKill = async (ids: string[]) => { if (ids.length && await confirm(ids.length === 1 ? `Kill process ${ids[0]}?` : `Kill ${ids.length} processes?`, undefined, 'Kill')) send(ids, true) }
  const killSelected = () => (sel.size ? askKill([...sel]) : notify('Tick the processes to kill first'))
  const toggle = (id: string) => setSel((s) => { const n = new Set(s); n.has(id) ? n.delete(id) : n.add(id); return n })
  const run = (a: string, id?: string) => {
    setCtx(null)
    if (a === 'kill') id && !sel.has(id) ? askKill([id]) : killSelected()
    else if (a === 'quick_look') { const p = procs.find((x) => x.id === (id ?? [...sel][0])); if (p) setLook(p.query); else notify('Click a row first') }
    else if (a === 'copy_rows_as') setCopyAs(true)
    else if (a === 'export') exportPage()
    else if (a === 'close') onClose()
  }
  const HEADS = [['Id', 'id'], ['User', 'user'], ['Database', 'db'], ['State', 'state'], ['Elapsed', 'elapsed']] as const
  return (
    <Dialog title="Process list" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Loading…</div>}
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !q.data.supported && <div className="text-ink-2">The process list is not supported by {info.driver}.</div>}
      {q.data?.supported && (q.data.processes.length === 0 ? <div className="text-ink-2">No other processes.</div> : (
        <div className="overflow-auto max-h-[50vh] border border-rule">
          <table className="text-[12px] font-mono border-collapse w-full">
            <thead><tr>
              <th className="p-1 border-b border-rule-2"><input type="checkbox" aria-label="Select all processes" checked={sel.size === procs.length && procs.length > 0} onChange={(e) => setSel(e.target.checked ? new Set(procs.map((p) => p.id)) : new Set())} /></th>
              {HEADS.map(([h, k]) => <th key={k} className="text-left p-1 border-b border-rule-2 font-medium text-ink-2" aria-sort={k === by ? (desc ? 'descending' : 'ascending') : undefined}><button className="btn-ghost p-0 font-medium" title={`Sort by ${h}`} onClick={() => header(k)}>{h}{k === by ? (desc ? ' ▾' : ' ▴') : ''}</button></th>)}
              <th className="text-left p-1 border-b border-rule-2 font-medium text-ink-2">Query</th><th className="p-1 border-b border-rule-2" aria-label="Actions" />
            </tr></thead>
            <tbody>{procs.map((p) => (
              <tr key={p.id} className={sel.has(p.id) ? 'bg-raised' : ''} aria-selected={sel.has(p.id)} onContextMenu={(e) => { e.preventDefault(); setCtx({ x: e.clientX, y: e.clientY, id: p.id }) }}>
                <td className="p-1 border-b border-rule"><input type="checkbox" aria-label={`Select process ${p.id}`} checked={sel.has(p.id)} onChange={() => toggle(p.id)} /></td>
                <td className="p-1 border-b border-rule">{p.id}</td><td className="p-1 border-b border-rule">{p.user}</td><td className="p-1 border-b border-rule">{p.db}</td><td className="p-1 border-b border-rule">{p.state}</td>
                <td className="p-1 border-b border-rule whitespace-nowrap">{(p.elapsedMs / 1000).toFixed(1)}s</td>
                <td className="p-1 border-b border-rule max-w-[36ch] truncate cursor-pointer" title="Click for a closer look" onClick={() => setLook(p.query)}>{p.query}</td>
                <td className="p-1 border-b border-rule"><button className="btn-ghost btn-danger" aria-label={`Kill process ${p.id}`} onClick={() => askKill([p.id])}>Kill</button></td>
              </tr>))}</tbody>
          </table>
        </div>))}
      {kill && <><GateFields gate={kill.gate} label="Safe mode" password={password} setPassword={setPassword} /><div className={ACTIONS}><button onClick={() => setKill(null)}>Cancel</button><button className="btn-primary" disabled={kill.gate.password && !password} onClick={() => send(kill.ids, true)}>Kill {kill.ids.length === 1 ? kill.ids[0] : `${kill.ids.length} processes`}</button></div></>}
      {err && <div className="text-del mt-2" role="alert">{err}</div>}
      {look !== null && <CellEditor col="Query" readOnly value={look} onDone={() => setLook(null)} />}
      {copyAs && <CopyAsDialog onPick={copyRows} onClose={() => setCopyAs(false)} />}
      {ctx && <TabMenu x={ctx.x} y={ctx.y} label="Process" onClose={() => setCtx(null)} items={menu.map((it): [string, () => void, string?] => [it.label, () => run(it.action, ctx.id), whyNot(it)])} />}
      <div className={ACTIONS}>
        <button onClick={() => q.refetch()}>Refresh</button>
        {procs.length > 0 && <>
          <button className="btn-danger" disabled={!sel.size} onClick={killSelected}>{label('kill')}{sel.size ? ` (${sel.size})` : ''}</button>
          <button onClick={() => setCopyAs(true)}>{label('copy_rows_as')}</button><button onClick={exportPage}>{label('export')}</button></>}
        <button className="btn-primary" onClick={onClose}>{label('close')}</button>
      </div>
    </Dialog>
  )
}
