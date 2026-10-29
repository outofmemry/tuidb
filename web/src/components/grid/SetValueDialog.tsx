// Set Value dialog: EMPTY / NULL / DEFAULT / NOW(), a date, or a file for a cell.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, qs, type DbObject, type Val } from '../../utils/api'
import { Dialog, confirm } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS } from '../shell/ui'

// Set Value menu: EMPTY / NULL / DEFAULT / NOW(), a date, or a file. Cells of binary columns travel as \x<hex> text (db.FormatValue), so a file is sent that way on PostgreSQL.
// shortcut: on other engines a file that is not valid UTF-8 text is also sent as \x<hex>, which they store as text; native binary literals are the upgrade.
// Only these run without the server's advanced flag (mirrors exprMacros in internal/web/changes.go).
const MACROS = new Set(['now()', 'current_timestamp', 'current_date', 'current_time', 'localtimestamp', 'gen_random_uuid()', 'uuid_generate_v4()', 'uuid()', 'unix_timestamp()'])
async function expression(e: string, onPick: (v: Val) => void) {
  if (MACROS.has(e.split(/\s+/).filter(Boolean).join('').toLowerCase())) return onPick({ $expr: e })
  if (await confirm('This expression is sent to the database as unescaped SQL. Use it?', e, 'Use it')) onPick({ $expr: e, $advanced: 'true' })
}
export function SetValueDialog({ sid, schema, col, dialect, onPick, onClose }: { sid: string; schema: string; col: string; dialect: string; onPick: (v: Val) => void; onClose: () => void }) {
  const [date, setDate] = useState(''), [err, setErr] = useState(''), [expr, setExpr] = useState(''), [seq, setSeq] = useState('')
  // PostgreSQL: a column can take the next value of a sequence
  const objs = useQuery({ queryKey: ['setvalue-seqs', sid, schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects` + qs({ schema })), enabled: dialect === 'postgres', gcTime: 0 })
  const seqs = (objs.data ?? []).filter((o) => o.Kind === 'sequence').map((o) => {
    const q = (x: string) => '"' + x.replace(/"/g, '""') + '"' // quoted, so a mixed-case sequence resolves
    return { value: o.Schema ? `${q(o.Schema)}.${q(o.Name)}` : q(o.Name), label: o.Schema ? `${o.Schema}.${o.Name}` : o.Name }
  })
  const file = async (f?: File) => {
    if (!f) return
    const b = new Uint8Array(await f.arrayBuffer())
    let text: string | null = null
    if (dialect !== 'postgres') { try { text = new TextDecoder('utf-8', { fatal: true }).decode(b) } catch { /* binary */ } }
    if (text !== null) return onPick(text)
    if (b.length > 8 << 20) return setErr('That file is larger than 8 MB')
    onPick('\\x' + Array.from(b, (x) => x.toString(16).padStart(2, '0')).join(''))
  }
  return (
    <Dialog title={`Set value of ${col}`} onClose={onClose}>
      <div className="flex flex-wrap gap-1.5">
        <button autoFocus onClick={() => onPick({ $: 'empty' })}>EMPTY</button>
        <button onClick={() => onPick({ $: 'null' })}>NULL</button>
        <button onClick={() => onPick({ $: 'default' })}>DEFAULT</button>
        <button onClick={() => onPick({ $expr: 'NOW()' })}>NOW()</button>
        {dialect === 'postgres' && <button onClick={() => onPick({ $expr: 'gen_random_uuid()' })}>gen_random_uuid()</button>}
      </div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Date</label>
        <input type="datetime-local" step={1} aria-label="Date" value={date} onChange={(e) => setDate(e.target.value)} />
        <button disabled={!date} onClick={() => onPick(date.replace('T', ' '))}>Set date</button></div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">File</label>
        <input type="file" aria-label="Add a file" onChange={(e) => file(e.target.files?.[0])} /></div>
      <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Advanced</label>
        <input className="flex-1 min-w-0 font-mono" aria-label="SQL expression" placeholder="now() + interval '1 day'" value={expr} onChange={(e) => setExpr(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && expr.trim()) expression(expr.trim(), onPick) }} />
        <button disabled={!expr.trim()} onClick={() => expression(expr.trim(), onPick)}>Use expression</button></div>
      {seqs.length > 0 && <div className="flex items-center gap-2 mt-3"><label className="w-24 text-ink-2">Sequence</label>
        <Select aria-label="Sequence" value={seq} onChange={setSeq} options={[{ value: '', label: 'pick a sequence' }, ...seqs]} />
        <button disabled={!seq} onClick={() => expression(`nextval('${seq.replace(/'/g, "''")}'::regclass)`, onPick)}>Use nextval</button></div>}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button></div>
    </Dialog>
  )
}
