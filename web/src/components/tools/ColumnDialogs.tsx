// Column dialogs: add a foreign key, move a column, edit one column as a form.
import { useState } from 'react'
import { api, type Column, type DiagramTable, type ObjRef, type Plan } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

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
