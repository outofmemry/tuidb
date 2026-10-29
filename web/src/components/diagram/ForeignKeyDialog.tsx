// Foreign key dialogs of the ER diagram: the Add foreign key form (columns, referenced columns, ON UPDATE / ON DELETE) and Delete table.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type DiagramTable, type ObjRef, type Plan } from '../../utils/api'
import { draftOf, FK_ACTIONS, newDef } from '../../utils/tabledef'
import { withForeignKey } from '../../utils/diagram'
import { Dialog, askMatch } from '../shell/Dialogs'
import { Select } from '../shell/Select'

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const ROW = 'flex items-center gap-2.5 my-2 text-[12.5px]'

// FkDialog loads the source table's definition, adds the key to the "new" side and posts old / new to /structure/plan.
export function FkDialog({ sid, from, col, to, onPlan, onClose }: { sid: string; from: DiagramTable; col: string; to: DiagramTable; onPlan: (p: Plan) => void; onClose: () => void }) {
  const q = (what: string) => `/sessions/${sid}/object/${what}?${new URLSearchParams({ schema: from.ref.Schema, name: from.ref.Name, kind: from.ref.Kind })}`
  const defQ = useQuery({ queryKey: ['fkdef', sid, from.ref], gcTime: 0, queryFn: async () => {
    const [cols, idx, fks] = await Promise.all([api<any[]>('GET', q('columns')), api<any[]>('GET', q('indexes')).catch(() => []), api<any[]>('GET', q('fks')).catch(() => [])])
    return draftOf(from.ref.Name, cols, idx, fks)
  } })
  const first = to.columns.find((c) => c.pk)?.name ?? to.columns.find((c) => c.name === col)?.name ?? to.columns[0]?.name ?? ''
  const [pairs, setPairs] = useState<[string, string][]>([[col, first]])
  const [name, setName] = useState(`fk_${from.ref.Name}_${col}`), [onDelete, setDel] = useState('NO ACTION'), [onUpdate, setUpd] = useState('NO ACTION')
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const setPair = (i: number, k: 0 | 1, v: string) => setPairs(pairs.map((p, j) => (j === i ? (k ? [p[0], v] : [v, p[1]]) : p) as [string, string]))
  const go = async () => {
    if (!defQ.data) return
    setBusy(true); setErr('')
    try {
      const schema = from.ref.Schema, base = newDef(schema, defQ.data)
      const nd = withForeignKey(base, { Name: name.trim(), Columns: pairs.map((p) => p[0]), RefSchema: to.ref.Schema, RefTable: to.ref.Name, RefColumns: pairs.map((p) => p[1]), OnUpdate: onUpdate, OnDelete: onDelete })
      const p = await api<Plan>('POST', `/sessions/${sid}/structure/plan`, { old: base, new: nd })
      if (!p.statements?.length) throw new Error('No structure changes')
      onClose(); onPlan(p)
    } catch (e) { setErr(msg(e)); setBusy(false) }
  }
  const mine = from.columns.map((c) => c.name), theirs = to.columns.map((c) => c.name)
  return (
    <Dialog title="Add foreign key" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); go() }}>
        <p className="text-ink-2 mt-0"><b className="text-ink font-medium">{from.ref.Name}</b> references <b className="text-ink font-medium">{to.ref.Name}</b>. You preview the statement before it is sent.</p>
        {defQ.isLoading && <div className="text-ink-2">Loading the table…</div>}
        {defQ.error && <div className="text-del">{defQ.error.message}</div>}
        {pairs.map((p, i) => (
          <div key={i} className={ROW}>
            <Select aria-label={`Column ${i + 1}`} value={p[0]} onChange={(v) => setPair(i, 0, v)} options={mine} /><span aria-hidden>→</span>
            <Select aria-label={`Referenced column ${i + 1}`} value={p[1]} onChange={(v) => setPair(i, 1, v)} options={theirs} />
            {pairs.length > 1 && <button type="button" className="btn-ghost" aria-label={`Remove pair ${i + 1}`} onClick={() => setPairs(pairs.filter((_, j) => j !== i))}>Remove</button>}
          </div>))}
        <button type="button" onClick={() => setPairs([...pairs, [mine.find((m) => !pairs.some((p) => p[0] === m)) ?? mine[0], theirs[0] ?? ''] ])}>Add column pair</button>
        <div className={ROW}><label className="w-28 text-ink-2" htmlFor="fk-name">Name</label><input id="fk-name" className="flex-1 min-w-0" value={name} onChange={(e) => setName(e.target.value)} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">On update</label><Select aria-label="On update" value={onUpdate} onChange={setUpd} options={FK_ACTIONS} /></div>
        <div className={ROW}><label className="w-28 text-ink-2">On delete</label><Select aria-label="On delete" value={onDelete} onChange={setDel} options={FK_ACTIONS} /></div>
        {err && <div className="text-del whitespace-pre-wrap" role="alert">{err}</div>}
        <div className="flex justify-end gap-2 mt-[18px]"><button type="button" onClick={onClose}>Cancel</button><button type="submit" className="btn-primary" disabled={busy || !defQ.data || !name.trim()}>{busy ? 'Planning…' : 'Preview statement'}</button></div>
      </form>
    </Dialog>
  )
}

// dropTable asks for the table's name, then plans the DROP through the same admin route as the sidebar's Drop….
export async function dropTable(sid: string, ref: ObjRef, onPlan: (p: Plan) => void, notify: (m: string) => void) {
  const t = await askMatch(`Type ${ref.Name} to drop this table`, ref.Name)
  if (t === null) return
  if (t !== ref.Name) return notify('The name did not match; nothing dropped.')
  try { onPlan(await api<Plan>('POST', `/sessions/${sid}/plan/admin`, { Op: 'drop', Ref: ref })) } catch (e) { notify(msg(e)) }
}
