import { useEffect, useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type Meta, type Column, type DbObject, type ObjRef, type Plan, type SessionInfo } from '../utils/api'
import { draftOf, dirty, FK_ACTIONS, newDef, oldDef, type Draft, type DraftCol, type DraftFk, type DraftIdx, type TableDef } from '../utils/tabledef'
import { Select } from './Select'
import { CopyAsDialog } from './Tools'
import { formatRows, copyText, type CopyFormat } from '../utils/copy'
import { filterMenu, whyNot, type MenuItem } from '../utils/menus'
import { TabMenu } from './Extras'

// Handle the owner uses to answer the Structure-context keys (add_row, delete_rows, set_value) for whichever row has the focus.
export type StructureCtl = { current: { add: () => void; del: () => void; toggle: () => void } | null }

type Props = {
  sid: string; caps: SessionInfo['caps']; obj: ObjRef; notify: (m: string) => void
  onPlan: (p: Plan) => void
  columns?: Column[]; total?: { n: number; exact: boolean }; section?: string; selected?: string; onSelect?: (n: string) => void
  creating?: boolean // a new table: starts with one id column and posts old = null
  nonce?: number // changes after a plan was applied, so the draft reloads from the server
  ctl?: StructureCtl
  onDirty?: (d: boolean) => void
}

const IN = 'w-full min-w-[5ch] bg-canvas px-1 py-0.5 text-[12px] font-mono'
const DEFAULTS = ['NULL', "''", '0', 'true', 'false', 'CURRENT_TIMESTAMP', 'now()', 'gen_random_uuid()', 'uuid()']
type Sec = 'columns' | 'indexes' | 'fks'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// StructureEditor is the TUI's structure view on the web: columns, indexes and foreign keys are edited in place as a draft,
// and the draft is posted as an old / new TableDef to /structure/plan; the plan then goes through the usual preview / safe-mode / apply dialog.
export function StructureEditor({ sid, caps, obj, notify, onPlan, columns = [], total, section, selected, onSelect, creating, nonce = 0, ctl, onDirty }: Props) {
  const idxQ = useQuery({ queryKey: ['obj', sid, 'indexes', obj, nonce], queryFn: () => api<any[]>('GET', `/sessions/${sid}/object/indexes?${new URLSearchParams({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })}`), enabled: !creating && caps.Indexes, gcTime: 0 })
  const fkQ = useQuery({ queryKey: ['obj', sid, 'fks', obj, nonce], queryFn: () => api<any[]>('GET', `/sessions/${sid}/object/fks?${new URLSearchParams({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })}`), enabled: !creating && caps.ForeignKeys, gcTime: 0 })
  const trgQ = useQuery({ queryKey: ['obj', sid, 'triggers', obj, nonce], queryFn: () => api<any[]>('GET', `/sessions/${sid}/object/triggers?${new URLSearchParams({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })}`), enabled: !creating && caps.Triggers, gcTime: 0 })
  const typesQ = useQuery({ queryKey: ['datatypes', sid], queryFn: () => api<{ types: string[] }>('GET', `/sessions/${sid}/datatypes`), staleTime: Infinity, gcTime: 0 })
  const tablesQ = useQuery({ queryKey: ['fk-tables', sid, obj.Schema], queryFn: () => api<DbObject[]>('GET', `/sessions/${sid}/objects?schema=${encodeURIComponent(obj.Schema)}`), gcTime: 0 })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const [copyAs, setCopyAs] = useState(false), [menu, setMenu] = useState<{ x: number; y: number } | null>(null), [dragFrom, setDragFrom] = useState(-1)
  const types = typesQ.data?.types?.length ? typesQ.data.types : caps.Types ?? []
  const tables = (tablesQ.data ?? []).filter((o) => o.Kind === 'table').map((o) => o.Name)

  const loaded = creating || (!columns.length ? false : (!caps.Indexes || idxQ.isSuccess || idxQ.isError) && (!caps.ForeignKeys || fkQ.isSuccess || fkQ.isError))
  const base = useMemo<Draft>(() => (creating
    ? { name: obj.Name, cols: [{ orig: '', name: 'id', type: types[0] ?? 'integer', nullable: false, def: '', pk: true, extra: '' }], idx: [], fks: [] }
    : draftOf(obj.Name, columns, idxQ.data ?? [], fkQ.data ?? [])),
  // eslint-disable-next-line react-hooks/exhaustive-deps
  [creating, obj.Name, columns, idxQ.data, fkQ.data])
  const [d, setD] = useState<Draft>(base)
  const seen = useRef<Draft | null>(null)
  // a new table's draft is set once: its base is rebuilt on every render (the columns prop defaults to a fresh []), and resetting it would undo each edit
  useEffect(() => { if (loaded && seen.current !== base && !(creating && seen.current)) { seen.current = base; setD(base) } }, [loaded, base])

  const changed = creating || dirty(d, base)
  useEffect(() => { onDirty?.(!!changed && !creating) }, [changed]) // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { if (creating) onDirty?.(!!seen.current && dirty(d, seen.current)) }, [d]) // eslint-disable-line react-hooks/exhaustive-deps -- a new table: edited past its one-column start
  useEffect(() => () => onDirty?.(false), []) // eslint-disable-line react-hooks/exhaustive-deps -- leaving the view drops the draft, so the tab is clean again
  const [err, setErr] = useState(''), [busy, setBusy] = useState(false)
  const cur = useRef<{ sec: Sec; i: number }>({ sec: 'columns', i: 0 })
  const focus = (sec: Sec, i: number) => () => { cur.current = { sec, i } }

  const setCol = (i: number, p: Partial<DraftCol>) => setD((x) => ({ ...x, cols: x.cols.map((c, j) => (j === i ? { ...c, ...p } : c)) }))
  const setIdx = (i: number, p: Partial<DraftIdx>) => setD((x) => ({ ...x, idx: x.idx.map((c, j) => (j === i ? { ...c, ...p } : c)) }))
  const setFk = (i: number, p: Partial<DraftFk>) => setD((x) => ({ ...x, fks: x.fks.map((c, j) => (j === i ? { ...c, ...p } : c)) }))
  // removing a row that was added here drops it; a server row is struck through and can be restored
  const drop = <T extends { orig: string; gone?: boolean }>(l: T[], i: number) => (l[i].orig ? l.map((c, j) => (j === i ? { ...c, gone: !c.gone } : c)) : l.filter((_, j) => j !== i))
  const addCol = () => setD((x) => ({ ...x, cols: [...x.cols, { orig: '', name: '', type: types[0] ?? '', nullable: true, def: '', pk: false, extra: '' }] }))
  const addIdx = () => setD((x) => ({ ...x, idx: [...x.idx, { orig: '', name: '', cols: '', unique: false, algorithm: '', condition: '' }] }))
  const addFk = () => setD((x) => ({ ...x, fks: [...x.fks, { orig: '', name: '', cols: '', refSchema: obj.Schema, refTable: '', refCols: '', onUpdate: 'NO ACTION', onDelete: 'NO ACTION' }] }))
  const ops = {
    add: () => ({ columns: addCol, indexes: addIdx, fks: addFk })[cur.current.sec](),
    del: () => {
      const { sec, i } = cur.current
      if (sec === 'columns') setD((x) => (x.cols[i] ? { ...x, cols: drop(x.cols, i) } : x))
      else if (sec === 'indexes') setD((x) => (x.idx[i] ? { ...x, idx: drop(x.idx, i) } : x))
      else setD((x) => (x.fks[i] ? { ...x, fks: drop(x.fks, i) } : x))
    },
    toggle: () => { const { sec, i } = cur.current; if (sec === 'columns') setD((x) => (x.cols[i] ? { ...x, cols: x.cols.map((c, j) => (j === i ? { ...c, nullable: !c.nullable } : c)) } : x)); else notify('Set value toggles Null on a column row') },
  }
  if (ctl) ctl.current = ops

  const preview = async () => {
    setErr('')
    let nd: TableDef
    try { nd = newDef(obj.Schema, d, creating ? undefined : base) } catch (e) { return setErr(msg(e)) }
    setBusy(true)
    try {
      const p = await api<Plan>('POST', `/sessions/${sid}/structure/plan`, { old: creating ? null : oldDef(obj.Schema, base), new: nd })
      if (!p.statements?.length) notify('No structure changes')
      else {
        onPlan(p)
        if (!creating && nd.Name !== base.name) notify(`After the rename, reopen the table as ${nd.Name} from the sidebar`)
      }
    } catch (e) { setErr(msg(e)) } finally { setBusy(false) }
  }

  const reorder = !!caps.ReorderColumns && !creating
  // M15.3 copy as: the column rows as the grid shows them, in any of the "Copy as" formats
  const copyRows = (fmt: CopyFormat) => {
    const rows = d.cols.filter((c) => !c.gone).map((c) => [c.name, c.type, c.nullable ? 'YES' : 'NO', c.def || null, c.pk ? 'PK' : '', c.extra])
    const cols = ['Name', 'Type', 'Null', 'Default', 'PK', 'Extra'].map((name) => ({ name, type: 'text' }))
    setCopyAs(false)
    copyText(formatRows(fmt, cols, rows, meta?.config.csv ?? { Delimiter: ',', Quote: '"', LineBreak: '\n', Decimal: '.' }, caps.Dialect, obj.Name)).then(() => notify(`Copied ${rows.length} columns as ${fmt}`), (e) => notify(msg(e)))
  }
  // a move reorders the draft, so several moves (and other edits) are planned together on Preview; the planner emits FIRST / AFTER where the order changed
  const move = (from: number, to: number) => setD((x) => {
    if (from === to || to < 0 || to >= x.cols.length) return x
    const cols = [...x.cols], [c] = cols.splice(from, 1)
    cols.splice(to, 0, c)
    return { ...x, cols }
  })
  useEffect(() => { if (section) document.getElementById('sec-' + section)?.scrollIntoView({ block: 'start' }) }, [section, idxQ.data, trgQ.data])

  // the structure menu comes from the shared list (menus.Structure); menu ids are the keymap actions
  const items = filterMenu(meta?.menus?.structure, { caps: caps as unknown as Record<string, unknown> })
  const act = (a: string) => {
    setMenu(null)
    const { i } = cur.current, c = d.cols[i]
    if (a === 'add_row') ops.add()
    else if (a === 'delete_rows') ops.del()
    else if (a === 'set_value') ops.toggle()
    else if ((a === 'move_up' || a === 'move_down') && c && cur.current.sec === 'columns') { const to = a === 'move_up' ? i - 1 : i + 1; move(i, to); cur.current = { sec: 'columns', i: Math.max(0, Math.min(d.cols.length - 1, to)) } }
    else if (a === 'copy_rows_as') setCopyAs(true)
  }
  const struck = (g?: boolean) => (g ? 'opacity-60 line-through decoration-1' : '')
  const Del = ({ g, onClick, what }: { g?: boolean; onClick: () => void; what: string }) => <button className="btn-ghost" aria-label={`${g ? 'Keep' : 'Delete'} ${what}`} onClick={onClick}>{g ? 'Keep' : 'Delete'}</button>
  const sorted = (l: string[]) => [...new Set(l)]

  return (
    <div className="px-4 py-3 overflow-auto flex-1 min-h-0 [&_h4]:mt-4 [&_h4]:mb-1.5 [&_h4:first-child]:mt-0 [&_h4]:text-[13px] [&_h4]:text-ink-2 [&_table]:border-collapse [&_table]:mb-2.5 [&_table]:text-[12px] [&_table]:font-mono [&_:is(th,td)]:text-left [&_:is(th,td)]:py-1 [&_:is(th,td)]:pr-[10px] [&_:is(th,td)]:pl-0 [&_:is(th,td)]:border-b [&_td]:border-rule [&_th]:border-rule-2 [&_th]:font-sans [&_th]:font-medium [&_th]:text-ink-2" data-ctx="structure" onContextMenu={(e) => { if (items.length && (e.target as Element).closest('tr')) { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY }) } }}>
      <datalist id={`types-${sid}`}>{sorted(types).map((t) => <option key={t} value={t} />)}</datalist>
      <datalist id={`defaults-${sid}`}>{DEFAULTS.map((t) => <option key={t} value={t} />)}</datalist>
      <datalist id={`tables-${sid}`}>{tables.map((t) => <option key={t} value={t} />)}</datalist>
      <div className="flex items-center gap-2 mb-3 sticky top-0 z-[2] bg-canvas py-1">
        <button className="btn-primary" disabled={busy || (!changed)} onClick={preview}>{busy ? 'Planning…' : creating ? 'Preview CREATE' : 'Preview changes'}</button>
        {!creating && <button disabled={!changed} onClick={() => { setD(base); setErr('') }}>Discard</button>}
        <button onClick={() => setCopyAs(true)} title="Copy the column rows in CSV, JSON, Markdown and other formats">Copy as…</button>
        <span className="text-ink-2">{creating ? 'Describe the table, then preview the statements.' : changed ? 'Not sent to the database yet.' : 'Edit a cell to start a change.'}</span>
      </div>
      {err && <div className="text-del whitespace-pre-wrap mb-2" role="alert">{err}</div>}
      <h4>Overview</h4>
      <table><tbody>
        <tr><td>Name</td><td><input className={IN} aria-label="Table name" value={d.name} onChange={(e) => setD({ ...d, name: e.target.value })} /></td></tr>
        {!creating && <tr><td>Kind</td><td>{obj.Kind}</td></tr>}
        {!creating && total && <tr><td>Rows</td><td>{total.exact ? '' : '~'}{total.n}</td></tr>}
      </tbody></table>
      <h4>Columns</h4>
      <table><thead><tr>{reorder && <th aria-label="Order" colSpan={2} />}<th>Name</th><th>Type</th><th>Null</th><th>Default</th><th>PK</th><th>Extra</th><th aria-label="Actions" /></tr></thead><tbody>
        {d.cols.map((c, i) => (
          <tr key={i} aria-selected={selected === c.orig && !!c.orig} className={`${struck(c.gone)} ${selected === c.orig && c.orig ? 'bg-raised' : ''} ${dragFrom >= 0 && dragFrom !== i ? 'outline-dashed outline-1 outline-rule-2 -outline-offset-1' : ''}`}
            onDragOver={(e) => { if (dragFrom >= 0) e.preventDefault() }} onDrop={(e) => { e.preventDefault(); const from = dragFrom; setDragFrom(-1); if (from >= 0) move(from, i) }} onFocusCapture={() => { focus('columns', i)(); if (c.orig) onSelect?.(c.orig) }}>
            {reorder && <td className="px-0 cursor-grab">
              <span draggable role="img" aria-label={`Drag ${c.name} to reorder`} title="Drag to move the column" onDragStart={(e) => { e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', c.name); setDragFrom(i) }} onDragEnd={() => setDragFrom(-1)}>⠿</span></td>}
            {reorder && <td className="whitespace-nowrap">
              <button className="btn-ghost px-1" disabled={i === 0} aria-label={`Move ${c.name} up`} onClick={() => move(i, i - 1)}>▲</button>
              <button className="btn-ghost px-1" disabled={i === d.cols.length - 1} aria-label={`Move ${c.name} down`} onClick={() => move(i, i + 1)}>▼</button></td>}
            <td><input className={IN} aria-label={`Column ${i + 1} name`} value={c.name} disabled={c.gone} onChange={(e) => setCol(i, { name: e.target.value })} /></td>
            <td><input className={IN} aria-label={`Column ${i + 1} type`} list={`types-${sid}`} value={c.type} disabled={c.gone} onChange={(e) => setCol(i, { type: e.target.value })} /></td>
            <td><input type="checkbox" aria-label={`Column ${i + 1} allows NULL`} checked={c.nullable} disabled={c.gone} onChange={(e) => setCol(i, { nullable: e.target.checked })} /></td>
            <td><input className={IN} aria-label={`Column ${i + 1} default`} list={`defaults-${sid}`} placeholder="none" value={c.def} disabled={c.gone} onChange={(e) => setCol(i, { def: e.target.value })} /></td>
            <td><input type="checkbox" aria-label={`Column ${i + 1} is part of the primary key`} checked={c.pk} disabled={c.gone} onChange={(e) => setCol(i, { pk: e.target.checked })} /></td>
            <td><input className={IN} aria-label={`Column ${i + 1} extra`} value={c.extra} disabled={c.gone} onChange={(e) => setCol(i, { extra: e.target.value })} /></td>
            <td><Del g={c.gone} what={`column ${c.name}`} onClick={() => setD({ ...d, cols: drop(d.cols, i) })} /></td>
          </tr>))}
      </tbody></table>
      <button onClick={addCol}>Add column</button>
      {caps.Indexes && <>
        <h4 id="sec-indexes">Indexes</h4>
        <table><thead><tr><th>Name</th><th>Columns</th><th>Unique</th><th>Algorithm</th><th>Condition</th><th aria-label="Actions" /></tr></thead><tbody>
          {d.idx.map((x, i) => (
            <tr key={i} className={struck(x.gone)} onFocusCapture={focus('indexes', i)}>
              <td><input className={IN} aria-label={`Index ${i + 1} name`} value={x.name} disabled={x.gone} onChange={(e) => setIdx(i, { name: e.target.value })} /></td>
              <td><input className={IN} aria-label={`Index ${i + 1} columns`} placeholder="a, b" value={x.cols} disabled={x.gone} onChange={(e) => setIdx(i, { cols: e.target.value })} /></td>
              <td><input type="checkbox" aria-label={`Index ${i + 1} is unique`} checked={x.unique} disabled={x.gone} onChange={(e) => setIdx(i, { unique: e.target.checked })} /></td>
              <td><input className={IN} aria-label={`Index ${i + 1} algorithm`} placeholder="btree" value={x.algorithm} disabled={x.gone} onChange={(e) => setIdx(i, { algorithm: e.target.value })} /></td>
              <td><input className={IN} aria-label={`Index ${i + 1} condition`} placeholder="WHERE …" value={x.condition} disabled={x.gone} onChange={(e) => setIdx(i, { condition: e.target.value })} /></td>
              <td><Del g={x.gone} what={`index ${x.name}`} onClick={() => setD({ ...d, idx: drop(d.idx, i) })} /></td>
            </tr>))}
        </tbody></table>
        <button onClick={addIdx}>Add index</button>
      </>}
      {caps.ForeignKeys && <>
        <h4 id="sec-fks">Foreign keys</h4>
        <table><thead><tr><th>Name</th><th>Columns</th><th>References</th><th>Ref. columns</th><th>On update</th><th>On delete</th><th aria-label="Actions" /></tr></thead><tbody>
          {d.fks.map((x, i) => (
            <tr key={i} className={struck(x.gone)} onFocusCapture={focus('fks', i)}>
              <td><input className={IN} aria-label={`Foreign key ${i + 1} name`} value={x.name} disabled={x.gone} onChange={(e) => setFk(i, { name: e.target.value })} /></td>
              <td><input className={IN} aria-label={`Foreign key ${i + 1} columns`} placeholder="a, b" value={x.cols} disabled={x.gone} onChange={(e) => setFk(i, { cols: e.target.value })} /></td>
              <td><input className={IN} aria-label={`Foreign key ${i + 1} referenced table`} list={`tables-${sid}`} value={x.refTable} disabled={x.gone} onChange={(e) => setFk(i, { refTable: e.target.value })} /></td>
              <td><input className={IN} aria-label={`Foreign key ${i + 1} referenced columns`} placeholder="id" value={x.refCols} disabled={x.gone} onChange={(e) => setFk(i, { refCols: e.target.value })} /></td>
              <td><Select className="bg-canvas" aria-label={`Foreign key ${i + 1} on update`} value={x.onUpdate || 'NO ACTION'} onChange={(v) => setFk(i, { onUpdate: v })} options={FK_ACTIONS} /></td>
              <td><Select className="bg-canvas" aria-label={`Foreign key ${i + 1} on delete`} value={x.onDelete || 'NO ACTION'} onChange={(v) => setFk(i, { onDelete: v })} options={FK_ACTIONS} /></td>
              <td><Del g={x.gone} what={`foreign key ${x.name || x.refTable}`} onClick={() => setD({ ...d, fks: drop(d.fks, i) })} /></td>
            </tr>))}
        </tbody></table>
        <button onClick={addFk}>Add foreign key</button>
      </>}
      {copyAs && <CopyAsDialog onPick={copyRows} onClose={() => setCopyAs(false)} />}
      {menu && <TabMenu x={menu.x} y={menu.y} label="Structure" onClose={() => setMenu(null)} items={items.map((it: MenuItem): [string, () => void, string?] => [it.label, () => act(it.action), whyNot(it)])} />}
      {!creating && caps.Triggers && <><h4 id="sec-triggers">Triggers</h4><table><tbody>{(trgQ.data ?? []).map((t) => <tr key={t.Name}><td>{t.Name}</td><td>{t.Timing} {t.Event}</td></tr>)}</tbody></table></>}
    </div>
  )
}
