// Milestone 4/5 dialogs over the /explain and /tools/* endpoints: explain tree, schema diff, data diff, bulk update, fake data.
// The tools plan on the server and hand back a stored plan; onPlan opens it in the usual preview dialog, which applies it behind the safe-mode gate.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, qs, type Column, type DbObject, type DiagramTable, type FilterSet, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { Dialog } from './Dialogs'
import { Select } from './Select'

const ACTIONS = 'flex justify-end gap-2 mt-[18px]'
const ROW = 'flex items-center gap-2.5 my-2 text-[12.5px]'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ---- explain as a tree --------------------------------------------------------------------------------

type Node = { depth: number; label: string; cost?: string; rows?: string }

export function ExplainDialog({ sid, body, onClose }: { sid: string; body: { Text: string; Current?: boolean; Offset?: number }; onClose: () => void }) {
  const q = useQuery({ queryKey: ['explain', sid, body], queryFn: () => api<{ statement: string; nodes: Node[] }>('POST', `/sessions/${sid}/explain`, body), gcTime: 0, retry: false })
  return (
    <Dialog title="Explain" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Planning…</div>}
      {q.error && <div className="text-del whitespace-pre-wrap">{msg(q.error)}</div>}
      {q.data && <>
        <pre className="mb-3">{q.data.statement}</pre>
        <div className="font-mono text-[12.5px] max-h-[50vh] overflow-auto" role="tree" aria-label="Query plan">
          {q.data.nodes.map((n, i) => (
            <div key={i} role="treeitem" aria-level={n.depth + 1} className="flex gap-3 py-0.5 border-b border-rule" style={{ paddingLeft: n.depth * 18 }}>
              <span className="flex-1 min-w-0 wrap-anywhere"><span className="text-ink-3">{n.depth ? '└ ' : ''}</span>{n.label}</span>
              {n.cost && <span className="text-ink-2 whitespace-nowrap">cost {n.cost}</span>}
              {n.rows && <span className="text-ink-2 whitespace-nowrap">rows {n.rows}</span>}
            </div>
          ))}
          {!q.data.nodes.length && <div className="text-ink-2">The plan is empty.</div>}
        </div>
      </>}
      <div className={ACTIONS}><button className="btn-primary" autoFocus onClick={onClose}>Close</button></div>
    </Dialog>
  )
}

// ---- schema diff --------------------------------------------------------------------------------------

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

// ---- data diff ----------------------------------------------------------------------------------------

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

// ---- bulk update --------------------------------------------------------------------------------------

type SetRow = { col: string; val: string; isNull: boolean }

export function BulkUpdateDialog({ sid, obj, filters, columns, onPlan, onClose }: { sid: string; obj: ObjRef; filters: FilterSet; columns: string[]; onPlan: (p: Plan) => void; onClose: () => void }) {
  const active = filters.Filters.some((f) => f.Enabled)
  const n = useQuery({ queryKey: ['bulkcount', sid, obj, filters], queryFn: () => api<{ n: number; exact: boolean }>('POST', `/sessions/${sid}/count`, { ref: obj, filters, estimate: false }), enabled: active, gcTime: 0 })
  const [rows, setRows] = useState<SetRow[]>([{ col: columns[0] ?? '', val: '', isNull: false }])
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const set = (i: number, p: Partial<SetRow>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...p } : r)))
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/tools/bulk-update`, { Ref: obj, Filters: filters, Set: rows.map((r) => ({ Col: r.col, Val: r.isNull ? null : r.val })) })
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title={`Bulk update ${obj.Name}`} onClose={onClose}>
      <p className="mt-0 text-ink-2">{active ? <>Sets these columns on every row the current filter matches{n.data ? <>: <b className="text-ink">{n.data.n} row{n.data.n === 1 ? '' : 's'}</b></> : ''}.</> : 'Add a filter first: a bulk update without one is refused, so a slip cannot rewrite the whole table.'}</p>
      {rows.map((r, i) => (
        <div key={i} className={ROW}>
          <Select aria-label={`Column ${i + 1}`} value={r.col} onChange={(v) => set(i, { col: v })} options={columns} />
          <span className="text-ink-2">=</span>
          <input className="flex-1 min-w-0" aria-label={`Value ${i + 1}`} disabled={r.isNull} value={r.isNull ? '' : r.val} onChange={(e) => set(i, { val: e.target.value })} />
          <label className="flex items-center gap-1.5"><input type="checkbox" checked={r.isNull} onChange={(e) => set(i, { isNull: e.target.checked })} />NULL</label>
          {rows.length > 1 && <button className="btn-ghost" onClick={() => setRows(rows.filter((_, j) => j !== i))} aria-label={`Remove column ${i + 1}`}>✕</button>}
        </div>
      ))}
      <button onClick={() => setRows([...rows, { col: columns.find((c) => !rows.some((r) => r.col === c)) ?? columns[0], val: '', isNull: false }])}>Add column</button>
      {err && <div className="text-del mt-2 whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={busy || !active || rows.some((r) => !r.col)} onClick={go}>{busy ? 'Planning…' : 'Preview statement'}</button></div>
    </Dialog>
  )
}

// ---- fake data ----------------------------------------------------------------------------------------

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

// ---- add a foreign key (the ER canvas: drag from a column onto a table) -------------------------------------

const ACTIONS_FK = ['NO ACTION', 'RESTRICT', 'CASCADE', 'SET NULL', 'SET DEFAULT']

export function AddFkDialog({ sid, from, col, to, onPlan, onClose }: { sid: string; from: DiagramTable; col: string; to: DiagramTable; onPlan: (p: Plan) => void; onClose: () => void }) {
  const [ref, setRef] = useState(to.columns.find((c) => c.pk)?.name ?? to.columns.find((c) => c.name === col)?.name ?? to.columns[0]?.name ?? '')
  const [name, setName] = useState(`fk_${from.ref.Name}_${col}`), [onDelete, setDel] = useState('NO ACTION'), [onUpdate, setUpd] = useState('NO ACTION')
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/plan/table`, { Ref: from.ref, Op: 'add_fk', FK: { Name: name, Columns: [col], RefSchema: to.ref.Schema, RefTable: to.ref.Name, RefColumns: [ref], OnUpdate: onUpdate, OnDelete: onDelete } })
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  return (
    <Dialog title="Add foreign key" onClose={onClose}>
      <p className="text-ink-2 mt-0"><b className="text-ink font-medium">{from.ref.Name}.{col}</b> references a column of <b className="text-ink font-medium">{to.ref.Name}</b>. You preview the statement before it is sent. A key over several columns is added from the structure.</p>
      <div className={ROW}><label className="w-28 text-ink-2">Referenced column</label><Select aria-label="Referenced column" value={ref} onChange={setRef} options={to.columns.map((c) => c.name)} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">Name</label><input className="flex-1 min-w-0" aria-label="Constraint name" value={name} onChange={(e) => setName(e.target.value)} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">On delete</label><Select aria-label="On delete" value={onDelete} onChange={setDel} options={ACTIONS_FK} /></div>
      <div className={ROW}><label className="w-28 text-ink-2">On update</label><Select aria-label="On update" value={onUpdate} onChange={setUpd} options={ACTIONS_FK} /></div>
      {err && <div className="text-del whitespace-pre-wrap">{err}</div>}
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={busy || !ref || !name.trim()} onClick={go}>{busy ? 'Planning…' : 'Preview statement'}</button></div>
    </Dialog>
  )
}

// ---- move a column (structure view, engines that can reorder columns) ----------------------------------------

export async function planMoveColumn(sid: string, ref: ObjRef, column: string, to: number): Promise<Plan> {
  return api<Plan>('POST', `/sessions/${sid}/plan/table`, { Ref: ref, Op: 'move_column', Column: column, To: to })
}

// ---- edit one column's attributes as a form (structure view, Row detail on a column) ----------------------------

export function ColumnDialog({ sid, obj, col, onPlan, onClose }: { sid: string; obj: ObjRef; col: Column; onPlan: (p: Plan) => void; onClose: () => void }) {
  const [name, setName] = useState(col.Name), [type, setType] = useState(col.Type), [nullable, setNullable] = useState(col.Nullable)
  const [def, setDef] = useState(col.HasDef ? col.Default : ''), [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const go = async () => {
    setBusy(true); setErr('')
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/plan/table`, { Ref: obj, Op: 'alter_column', Column: col.Name, Col: { Name: name.trim(), Type: type.trim(), Nullable: nullable, Default: def, HasDef: def !== '' } })
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  const same = name === col.Name && type === col.Type && nullable === col.Nullable && def === (col.HasDef ? col.Default : '')
  return (
    <Dialog title={`Column ${col.Name}`} onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); if (!same && name.trim() && type.trim()) go() }}>
        <div className={ROW}><label className="w-28 text-ink-2">Name</label><input autoFocus className="flex-1 min-w-0" aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">Type</label><input className="flex-1 min-w-0" aria-label="Type" value={type} onChange={(e) => setType(e.target.value)} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">Nullable</label><label className="flex items-center gap-1.5"><input type="checkbox" checked={nullable} onChange={(e) => setNullable(e.target.checked)} />Allows NULL</label></div>
        <div className={ROW}><label className="w-28 text-ink-2">Default</label><input className="flex-1 min-w-0 font-mono" aria-label="Default" placeholder="none; an SQL expression" value={def} onChange={(e) => setDef(e.target.value)} /></div>
        {(col.PK || col.Extra) && <div className="text-ink-3 text-[12px]">{col.PK ? 'Primary key. ' : ''}{col.Extra ? `Extra: ${col.Extra}. ` : ''}These are not changed here.</div>}
        {err && <div className="text-del whitespace-pre-wrap">{err}</div>}
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button className="btn-primary" type="submit" disabled={busy || same || !name.trim() || !type.trim()}>{busy ? 'Planning…' : 'Preview statement'}</button></div>
      </form>
    </Dialog>
  )
}
