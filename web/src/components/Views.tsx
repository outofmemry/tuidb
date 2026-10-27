// Milestone 3 tabs: ER diagram, metrics board, AI assistant.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, download, saveBlob, watchJob, type BoardInfo, type BoardPanel, type Chat, type ChatSummary, type Meta, type DiagramTable, type ObjRef, type PanelOut, type Plan, type SessionInfo } from '../utils/api'
import { copyText } from '../utils/copy'
import { ask, confirm } from './Dialogs'
import { DiagramCanvas } from './Canvas'
import { FkDialog, dropTable } from './m13_diagram'
import { chatTitle, enterSubmits, isVarPanel, panelVals, validColour } from '../utils/m13'
import { Select } from './Select'
import { ImportDialog, Plot } from './Tools'
import { Dialog } from './Dialogs'

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

// ---- ER diagram ---------------------------------------------------------------------------------

export function DiagramTab({ sid, info, focus, onStructure, onPlan }: { sid: string; info: SessionInfo; focus?: ObjRef; onStructure: (r: ObjRef) => void; onPlan: (p: Plan) => void }) {
  const [schema, setSchema] = useState('') // '' = the connection's own schema; the switcher lists the others
  const q = useQuery({ queryKey: ['diagram', sid, info.database, info.schema, schema], queryFn: () => api<{ schema?: string; schemas?: string[]; tables: DiagramTable[] }>('GET', `/sessions/${sid}/diagram${schema ? '?schema=' + encodeURIComponent(schema) : ''}`), gcTime: 0 })
  const [note, setNote] = useState('')
  const shownSchema = q.data?.schema ?? info.schema
  const colourKey = `webtuidb-er-colors:${info.name}/${info.database}/${shownSchema}`
  // shortcut: table colours live in this browser's localStorage beside the positions, not in state.json (the terminal keeps its own)
  const [colors, setColors] = useState<Record<string, string>>({})
  useEffect(() => { try { setColors(JSON.parse(localStorage.getItem(colourKey) ?? '{}')) } catch { setColors({}) } }, [colourKey])
  const setColour = (t: string, c: string) => setColors((m) => { const n = { ...m }; if (c) n[t] = c; else delete n[t]; try { localStorage.setItem(colourKey, JSON.stringify(n)) } catch { /* private mode */ } return n })
  const [sort, setSort] = useState('name')
  const [filter, setFilter] = useState(focus?.Name ?? '') // opened from a table's menu: the filter starts on that table and the canvas centres on it
  useEffect(() => { if (focus) setFilter(focus.Name) }, [focus])
  const [list, setList] = useState(() => { try { return localStorage.getItem('webtuidb-er-view') === 'list' } catch { return false } })
  const [link, setLink] = useState<{ from: DiagramTable; col: string; to: DiagramTable } | null>(null)
  const f = filter.toLowerCase()
  const tables = (q.data?.tables ?? []).filter((t) => t.ref.Name.toLowerCase().includes(f))
    .sort((a, b) => (sort === 'columns' ? b.columns.length - a.columns.length : 0) || a.ref.Name.localeCompare(b.ref.Name))
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="toolbar bg-canvas">
        <input className="w-48 max-w-full" placeholder="Filter tables" aria-label="Filter tables" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <Select className="bg-raised border-transparent" aria-label="Sort" value={sort} onChange={setSort} options={[{ value: 'name', label: 'Sort by name' }, { value: 'columns', label: 'Sort by columns' }]} />
        {!!q.data?.schemas?.length && q.data.schemas.length > 1 && <Select className="bg-raised border-transparent" aria-label="Schema" value={shownSchema} onChange={setSchema} options={q.data.schemas} />}
        <button onClick={() => q.refetch()}>Reload</button>
        <button onClick={() => { setList(!list); try { localStorage.setItem('webtuidb-er-view', list ? 'canvas' : 'list') } catch { /* private mode */ } }}>{list ? 'Canvas' : 'List'}</button>
        <span className="text-ink-2 text-[12px]">{tables.length} tables.{note && <b className="font-medium text-ink"> {note}.</b>} One colour per group of tables joined by foreign keys; right-click a table for its own colour.{!list && info.caps.ForeignKeys && ' Drag from the dot at the end of a column onto another table to add a foreign key.'}</span>
      </div>
      {q.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading…</div>}
      {q.error && <div className="p-2.5 text-[12.5px] text-del">{q.error.message}</div>}
      {!list && q.data && <DiagramCanvas key={q.dataUpdatedAt} tables={q.data.tables} storageKey={`${info.name}/${info.database}/${shownSchema}`} colors={colors} onColor={setColour} onNote={setNote} onDelete={(t) => dropTable(sid, t.ref, onPlan, setNote)} query={filter} centerOn={focus ? focus.Schema + '.' + focus.Name : undefined} canLink={info.caps.ForeignKeys} onStructure={onStructure} onLink={(from, col, to) => setLink({ from, col, to })} />}
      {link && <FkDialog sid={sid} {...link} onPlan={onPlan} onClose={() => setLink(null)} />}
      {list && <div className="flex-1 min-h-0 overflow-auto p-3 grid gap-3 content-start grid-cols-[repeat(auto-fill,minmax(230px,1fr))] font-mono text-[12px]">
        {tables.map((t) => {
          const hue = (t.cluster * 67) % 360, tint = validColour(colors[t.ref.Schema + '.' + t.ref.Name]), fkCols = new Set(t.fks.flatMap((k) => k.columns))
          return (
            <section key={t.ref.Schema + '.' + t.ref.Name} className="border bg-panel rounded-[2px] min-w-0" style={{ borderColor: tint ?? `hsl(${hue} 40% 50% / .55)` }}>
              <header className="flex items-center gap-2 px-2.5 py-1.5 font-medium" style={{ background: tint ? `color-mix(in srgb, ${tint} 25%, transparent)` : `hsl(${hue} 45% 50% / .22)` }}>
                <span className="flex-1 min-w-0 truncate" title={t.ref.Name}>{t.ref.Name}</span>
                <button className="btn-ghost" onClick={() => onStructure(t.ref)} title="Open the structure of this table">Structure</button>
              </header>
              <ul className="m-0 p-0 list-none">
                {t.columns.map((c) => (
                  <li key={c.name} className="flex gap-2 px-2.5 py-0.5 border-t border-rule">
                    <span className="w-3 text-accent" aria-label={c.pk ? 'primary key' : fkCols.has(c.name) ? 'foreign key' : undefined}>{c.pk ? '#' : fkCols.has(c.name) ? '→' : ''}</span>
                    <span className="flex-1 min-w-0 truncate" title={c.name}>{c.name}</span><span className="text-ink-3 truncate max-w-[10ch]" title={c.type}>{c.type}</span>
                  </li>))}
              </ul>
              {t.fks.map((k, i) => <div key={i} className="px-2.5 py-0.5 border-t border-rule text-ink-2 truncate" title={`${k.columns.join(', ')} → ${k.refTable}(${k.refColumns.join(', ')})`}>{k.columns.join(', ')} → {k.refTable}</div>)}
            </section>
          )
        })}
      </div>}
    </div>
  )
}

// ---- metrics board ---------------------------------------------------------------------------------

type Layout = { order: number[]; span: Record<number, number>; h: Record<number, number> }
const layoutKey = (board: string) => 'webtuidb-board:' + board
// shortcut: the arrangement lives in this browser's localStorage, not in the board's .toml (the engine's Board type would drop unknown keys on its next save)
const loadLayout = (board: string): Layout => { try { return { order: [], span: {}, h: {}, ...JSON.parse(localStorage.getItem(layoutKey(board)) ?? '{}') } } catch { return { order: [], span: {}, h: {} } } }
const KINDS = ['number', 'table', 'bar', 'line', 'button', 'import', 'exporter', 'input', 'dropdown']
const EXPORT_FORMATS = [{ value: 'csv', label: 'CSV' }, { value: 'json', label: 'JSON' }, { value: 'sql', label: 'SQL INSERTs' }, { value: 'excel', label: 'Excel' }]
const ACTION_KINDS = new Set(['button', 'import', 'exporter', 'input', 'dropdown'])

export function MetricsTab({ sid, info, shown, others, notify }: { sid: string; info: SessionInfo; shown: boolean; others: Record<string, string>; notify: (m: string) => void }) {
  const list = useQuery({ queryKey: ['boards'], queryFn: () => api<BoardInfo[]>('GET', '/boards'), gcTime: 0 })
  const [name, setName] = useState('')
  const [runSid, setRunSid] = useState(sid) // a board can run on another open connection
  const [vals, setVals] = useState<Record<string, string>>({})
  const [panels, setPanels] = useState<PanelOut[]>([])
  const [err, setErr] = useState('')
  const [layout, setLayoutState] = useState<Layout>({ order: [], span: {}, h: {} })
  const [cols, setCols] = useState(3)
  const [form, setForm] = useState<'' | 'new' | 'panel'>('')
  const [importInto, setImportInto] = useState<ObjRef | null>(null)
  const dragging = useRef<number | null>(null)
  const board = (list.data ?? []).find((b) => b.name === name && !b.error)
  const valsRef = useRef(vals); valsRef.current = vals
  useEffect(() => { if (!name && list.data?.length) setName(list.data.find((b) => !b.error)?.name ?? '') }, [list.data, name])
  useEffect(() => { if (board) setVals(Object.fromEntries(board.vars.map((v) => [v.Name, v.Default]))) }, [board?.name])
  useEffect(() => { if (name) setLayoutState(loadLayout(name)) }, [name])
  const setLayout = (l: Layout) => { setLayoutState(l); try { localStorage.setItem(layoutKey(name), JSON.stringify(l)) } catch { /* private mode: the arrangement lasts until reload */ } }
  const curBoard = useRef(''); curBoard.current = board?.name ?? ''
  const run = async () => {
    if (!board) return
    const mine = board.name // a slow run of another board must not land on this one
    try { const p = (await api<{ panels: PanelOut[] }>('POST', `/sessions/${runSid}/boards/run`, { Name: mine, Vals: valsRef.current })).panels; if (curBoard.current === mine) { setPanels(p); setErr('') } } catch (e) { if (curBoard.current === mine) setErr(msg(e)) }
  }
  const runRef = useRef(run); runRef.current = run
  // a button runs its target panel alone, then the panels set to refresh on it (a panel runs once per click, so a cycle ends)
  const runPanel = async (title: string, seen = new Set<string>()) => {
    if (!board || seen.has(title)) return
    seen.add(title)
    try {
      const r = await api<{ panel: PanelOut; dependents: string[] }>('POST', `/sessions/${runSid}/boards/panel`, { Name: board.name, Title: title, Vals: valsRef.current })
      if (curBoard.current === board.name) setPanels((l) => (l.some((p) => p.title === title) ? l.map((p) => (p.title === title ? r.panel : p)) : [...l, r.panel]))
      for (const d of r.dependents) await runPanel(d, seen)
    } catch (e) { notify(msg(e)) }
  }
  // refresh while the board is open and on screen; the timer dies with the tab
  useEffect(() => {
    if (!board || !shown) return
    runRef.current()
    if (board.refreshSecs <= 0) return
    const t = setInterval(() => runRef.current(), board.refreshSecs * 1000)
    return () => clearInterval(t)
  }, [board?.name, board?.refreshSecs, shown, runSid])
  const box = useCallback((el: HTMLDivElement | null) => {
    if (!el) return
    const on = () => setCols(Math.max(1, Math.floor(el.clientWidth / 320)))
    on(); new ResizeObserver(on).observe(el)
  }, [])
  // display order: the saved order first, then panels the layout does not know yet
  const order = [...layout.order.filter((i) => i < panels.length), ...panels.map((_, i) => i).filter((i) => !layout.order.includes(i))]
  const move = (from: number, to: number) => { const o = order.filter((i) => i !== from); o.splice(Math.max(0, Math.min(o.length, to)), 0, from); setLayout({ ...layout, order: o }) }
  const dup = async () => {
    const n = await ask('Name for the copy', `${name} copy`); if (!n) return
    try { await api('POST', '/boards/duplicate', { Name: name, NewName: n }); await list.refetch(); setName(n.trim()); notify(`Copied to ${n.trim()}`) } catch (e) { notify(msg(e)) }
  }
  const save = (from: string, b: BoardInfo, nameTo = b.name) => api('PUT', `/boards/${encodeURIComponent(from)}`, { Name: nameTo, RefreshSecs: b.refreshSecs, Connection: b.connection ?? '', Vars: b.vars, Panels: b.panels }).then(async () => { await list.refetch(); setName(nameTo) })
  // rename = save under the new name; the server removes the old file
  const rename = async () => {
    if (!board) return
    const n = (await ask('Rename board', board.name))?.trim(); if (!n || n === board.name) return
    try { await save(board.name, board, n); notify(`Renamed to ${n}`) } catch (e) { notify(msg(e)) }
  }
  const del = async () => {
    if (!board) return
    const t = await ask(`Type ${board.name} to delete this board`); if (t === null) return
    if (t !== board.name) return notify('The name did not match; nothing deleted.')
    try { await api('DELETE', `/boards/${encodeURIComponent(board.name)}`); setName(''); setPanels([]); await list.refetch(); notify('Board deleted') } catch (e) { notify(msg(e)) }
  }
  const conns = Object.keys(others)
  const actions = (board?.panels ?? []).filter((p) => p.Kind === 'button' || p.Kind === 'import')
  const target = (t: string): ObjRef => { const dot = info.caps.Schemas ? t.indexOf('.') : -1; return dot > 0 ? { Schema: t.slice(0, dot), Name: t.slice(dot + 1), Kind: 'table' } : { Schema: info.caps.Schemas ? info.schema : '', Name: t, Kind: 'table' } }
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="toolbar bg-canvas">
        <Select className="bg-raised border-transparent" aria-label="Board" value={name} onChange={setName} options={(list.data ?? []).filter((b) => !b.error).map((b) => b.name)} />
        {board?.vars.map((v) => (
          <label key={v.Name} className="flex items-center gap-1.5 text-[12px]">{v.Label || v.Name}
            {v.Options?.length ? <Select className="bg-raised border-transparent" aria-label={v.Name} value={vals[v.Name] ?? ''} onChange={(x) => setVals({ ...vals, [v.Name]: x })} options={v.Options} />
              : <input className="w-28" aria-label={v.Name} value={vals[v.Name] ?? ''} onChange={(e) => setVals({ ...vals, [v.Name]: e.target.value })} onKeyDown={(e) => e.key === 'Enter' && run()} />}
          </label>))}
        {conns.length > 1 && <label className="flex items-center gap-1.5 text-[12px]">Run on
          <Select className="bg-raised border-transparent" aria-label="Run on connection" value={conns.find((c) => others[c] === runSid) ?? ''} onChange={(c) => setRunSid(others[c])} options={conns} /></label>}
        <button onClick={run} disabled={!board}>Refresh</button>
        {actions.map((a) => a.Kind === 'button'
          ? <button key={a.Title} className="btn-primary" onClick={() => runPanel(a.Target ?? '')} title={`Runs the panel ${a.Target}`}>{a.Title}</button>
          : <button key={a.Title} onClick={() => setImportInto(target(a.Table ?? ''))} title={`Import into ${a.Table}`}>{a.Title}</button>)}
        <span className="flex-1 min-w-0" />
        <button onClick={() => setForm('new')}>New board…</button>
        <button onClick={() => setForm('panel')} disabled={!board}>Add panel…</button>
        <button onClick={rename} disabled={!board}>Rename…</button>
        <button onClick={dup} disabled={!board}>Duplicate</button>
        <button className="btn-danger" onClick={del} disabled={!board}>Delete…</button>
        {layout.order.length + Object.keys(layout.span).length + Object.keys(layout.h).length > 0 && <button className="btn-ghost" onClick={() => setLayout({ order: [], span: {}, h: {} })} title="Forget the arrangement of the panels">Reset layout</button>}
        {board && board.refreshSecs > 0 && <span className="text-ink-2 text-[12px]">every {board.refreshSecs}s</span>}
      </div>
      {list.isLoading && <div className="p-2.5 text-[12.5px] text-ink-2">Loading…</div>}
      {list.error && <div className="p-2.5 text-[12.5px] text-del">{list.error.message}</div>}
      {list.data && !board && <div className="px-8 py-14 max-w-[56ch] text-ink-2 text-[15px] leading-normal">{list.data.length ? 'None of the boards can be read.' : 'No boards yet. Choose New board…, chart a query result and choose Add to Metrics Board, or put a .toml file in the boards folder of the data directory.'}</div>}
      {err && <div className="p-2.5 text-[12.5px] text-del">{err}</div>}
      <div ref={box} className="flex-1 min-h-0 overflow-auto p-3 grid gap-3 content-start" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
        {order.map((i, at) => {
          const p = panels[i], span = Math.min(cols, layout.span[i] ?? 1)
          return (
            <section key={i} className="border border-rule bg-panel rounded-[2px] min-w-0 flex flex-col" style={{ gridColumn: `span ${span}` }}
              onDragOver={(e) => { if (dragging.current !== null) e.preventDefault() }} onDrop={(e) => { e.preventDefault(); if (dragging.current !== null && dragging.current !== i) move(dragging.current, at); dragging.current = null }}>
              <header draggable className="flex items-center gap-1 px-2.5 py-1.5 border-b border-rule font-medium text-[12.5px] cursor-grab active:cursor-grabbing" title="Drag to rearrange the panels"
                onDragStart={(e) => { dragging.current = i; e.dataTransfer.effectAllowed = 'move'; e.dataTransfer.setData('text/plain', String(i)) }} onDragEnd={() => { dragging.current = null }}>
                <span className="flex-1 min-w-0 truncate">{p.title}</span>
                <button className="btn-ghost px-1" aria-label={`Run ${p.title} alone`} title="Run only this panel" onClick={() => runPanel(p.title)}>↻</button>
                <button className="btn-ghost px-1" aria-label={`Move ${p.title} earlier`} disabled={at === 0} onClick={() => move(i, at - 1)}>◀</button>
                <button className="btn-ghost px-1" aria-label={`Move ${p.title} later`} disabled={at === order.length - 1} onClick={() => move(i, at + 1)}>▶</button>
                <button className="btn-ghost px-1" aria-label={`Make ${p.title} narrower`} disabled={span <= 1} onClick={() => setLayout({ ...layout, span: { ...layout.span, [i]: span - 1 } })}>−</button>
                <button className="btn-ghost px-1" aria-label={`Make ${p.title} wider`} disabled={span >= cols} onClick={() => setLayout({ ...layout, span: { ...layout.span, [i]: span + 1 } })}>+</button>
              </header>
              <div className="p-2.5 overflow-auto max-h-[80vh] min-h-[90px] resize-y text-[12px] font-mono" style={{ height: layout.h[i] ?? 260 }}
                onMouseUp={(e) => { const h = Math.round(e.currentTarget.offsetHeight); if (Math.abs(h - (layout.h[i] ?? 0)) > 4 && e.currentTarget.dataset.resized === '1') setLayout({ ...layout, h: { ...layout.h, [i]: h } }) }}
                onMouseDown={(e) => { const r = e.currentTarget.getBoundingClientRect(); e.currentTarget.dataset.resized = e.clientY > r.bottom - 16 && e.clientX > r.right - 16 ? '1' : '0' }}>
                {p.error ? <div className="text-del">{p.error}</div>
                  : p.kind === 'number' ? <div className="text-[40px] leading-none font-medium">{p.number}</div>
                  : p.kind === 'table' && p.result ? (
                    <table className="border-collapse w-full"><thead><tr>{p.result.columns.map((c) => <th key={c.name} className="text-left p-1 border-b border-rule-2 font-medium text-ink-2">{c.name}</th>)}</tr></thead>
                      <tbody>{p.result.cells.map((r, j) => <tr key={j}>{r.map((c, k) => <td key={k} className="p-1 border-b border-rule">{c ?? <span className="text-ink-3">NULL</span>}</td>)}</tr>)}</tbody></table>)
                  : p.values.length ? <Plot s={{ kind: p.kind, labels: p.labels, values: p.values, series: p.series }} /> : <span className="text-ink-2">No data.</span>}
              </div>
            </section>)
        })}
      </div>
      {form && <BoardForm mode={form} board={form === 'panel' ? board : undefined} onClose={() => setForm('')} onSave={async (from, b, n) => { if (form === 'new' && list.data?.some((x) => x.name === n)) return notify(`A board named ${n} already exists`); try { await save(from, b, n); setForm(''); notify('Board saved') } catch (e) { notify(msg(e)) } }} />}
      {importInto && <ImportDialog sid={sid} table={importInto} notify={notify} onClose={() => setImportInto(null)} onDone={() => run()} />}
    </div>
  )
}

// New board (name and a first panel) or Add panel (one more panel on the open board). The server checks the whole board when it saves it.
function BoardForm({ mode, board, onSave, onClose }: { mode: 'new' | 'panel'; board?: BoardInfo; onSave: (from: string, b: BoardInfo, name: string) => void; onClose: () => void }) {
  const [name, setName] = useState(mode === 'new' ? '' : board!.name)
  const [secs, setSecs] = useState(String(board?.refreshSecs ?? 30))
  const [p, setP] = useState<BoardPanel>({ Title: '', Kind: 'number', Query: '', Chart: '', Target: '', RefreshOn: '', Table: '' })
  const set = (k: keyof BoardPanel, v: string) => setP({ ...p, [k]: v })
  const titles = (board?.panels ?? []).filter((x) => x.Kind !== 'button' && x.Kind !== 'import').map((x) => x.Title)
  const all = (board?.panels ?? []).map((x) => x.Title)
  const plot = p.Kind === 'bar' || p.Kind === 'line'
  const runs = p.Kind !== 'button' && p.Kind !== 'import'
  const ok = name.trim() && p.Title.trim() && (runs ? p.Query.trim() : p.Kind === 'button' ? p.Target : p.Table?.trim())
  const go = () => {
    const keep: BoardPanel = { Title: p.Title.trim(), Kind: p.Kind, Query: runs ? p.Query : '', Chart: plot ? p.Chart : '', Target: p.Kind === 'button' ? p.Target : '', RefreshOn: runs ? p.RefreshOn : '', Table: p.Kind === 'import' ? p.Table?.trim() : '' }
    const b: BoardInfo = { name: name.trim(), refreshSecs: Number(secs) || 0, vars: board?.vars ?? [], panels: [...(board?.panels ?? []), keep], error: '' }
    onSave(board?.name ?? name.trim(), b, name.trim())
  }
  return (
    <Dialog title={mode === 'new' ? 'New board' : `Add a panel to ${board!.name}`} onClose={onClose}>
      <div className="grid grid-cols-[130px_1fr] gap-x-3 gap-y-2 items-center text-[12.5px] [&_label]:text-ink-2">
        {mode === 'new' && <><label>Board name</label><input autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          <label>Refresh (seconds)</label><input type="number" min={0} value={secs} onChange={(e) => setSecs(e.target.value)} /></>}
        <label>Panel title</label><input autoFocus={mode === 'panel'} value={p.Title} onChange={(e) => set('Title', e.target.value)} />
        <label>Kind</label><Select aria-label="Kind" value={p.Kind} onChange={(v) => set('Kind', v)} options={KINDS} />
        {runs && <><label>Query</label><textarea className="font-mono min-h-[5em] bg-canvas" aria-label="Query" placeholder="select count(*) from orders" value={p.Query} onChange={(e) => set('Query', e.target.value)} /></>}
        {plot && <><label>Chart columns</label><input aria-label="Chart columns" placeholder="x: day, y: orders, refunds" value={p.Chart} onChange={(e) => set('Chart', e.target.value)} /></>}
        {p.Kind === 'button' && <><label>Runs panel</label><Select aria-label="Target" value={p.Target ?? ''} onChange={(v) => set('Target', v)} options={[{ value: '', label: 'pick a panel' }, ...titles]} /></>}
        {p.Kind === 'import' && <><label>Import into</label><input aria-label="Table" placeholder="table name" value={p.Table ?? ''} onChange={(e) => set('Table', e.target.value)} /></>}
        {runs && all.length > 0 && <><label>Refresh on</label><Select aria-label="Refresh on" value={p.RefreshOn ?? ''} onChange={(v) => set('RefreshOn', v)} options={[{ value: '', label: 'nothing' }, ...all]} /></>}
      </div>
      <div className="flex justify-end gap-2 mt-[18px]"><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={!ok} onClick={go}>Save</button></div>
    </Dialog>
  )
}

// ---- AI assistant ---------------------------------------------------------------------------------------

type Block = { sql: string; readOnly: boolean }
type Msg = { role: 'user' | 'assistant'; content: string; blocks?: Block[] }

// split a reply into prose and fenced code so sql blocks get their buttons
function parts(content: string): { code: boolean; text: string }[] {
  return content.split(/(```[^\n]*\n[\s\S]*?```)/g).filter(Boolean).map((t) => {
    const m = /^```([^\n]*)\n([\s\S]*?)```$/.exec(t)
    return m ? { code: true, text: m[2].trim() } : { code: false, text: t }
  })
}

export function AssistantTab({ sid, onRun, notify }: { sid: string; onRun: (sql: string) => void; notify: (m: string) => void }) {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const [models, setModels] = useState<string[]>([])
  const [provider, setProvider] = useState('') // "" = the default provider; the others are [ai.providers.<name>] (M7)
  const [model, setModelState] = useState(() => { try { return localStorage.getItem('webtuidb-ai-model') ?? '' } catch { return '' } })
  const setModel = (m: string) => { setModelState(m); try { localStorage.setItem('webtuidb-ai-model', m) } catch { /* private mode */ } }
  const loadModels = async () => { try { setModels((await api<{ models: string[] }>('GET', '/assistant/models' + (provider ? `?provider=${encodeURIComponent(provider)}` : ''))).models) } catch (e) { setErr(msg(e)) } }
  const pick = (v: string) => { setProvider(v); setModels([]); setModel('') } // a model name belongs to its provider
  const ctrl = useRef<AbortController | null>(null)
  const end = useRef<HTMLDivElement>(null)
  useEffect(() => { end.current?.scrollIntoView?.({ block: 'end' }) }, [msgs.length, busy])
  useEffect(() => () => ctrl.current?.abort(), [])
  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText(''); chat([...msgs, { role: 'user', content: t }])
  }
  // resend: everything after that user message is dropped and the model answers again
  const resend = (i: number) => { if (!busy) chat(msgs.slice(0, i + 1)) }
  const chat = async (next: Msg[]) => {
    setMsgs(next); setBusy(true); setErr('')
    const mine = ctrl.current = new AbortController()
    try {
      const r = await api<{ reply: string; blocks: Block[] }>('POST', `/sessions/${sid}/assistant`, { messages: next.map(({ role, content }) => ({ role, content })), model: model || undefined, provider: provider || undefined }, mine.signal)
      setMsgs([...next, { role: 'assistant', content: r.reply, blocks: r.blocks }])
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') setErr('Stopped.'); else setErr(msg(e))
    } finally { setBusy(false) }
  }
  const run = async (m: Msg, sql: string) => {
    const b = m.blocks?.find((x) => x.sql === sql)
    if (!b?.readOnly && !(await confirm('This statement changes data or structure. Open it in a query tab and run it?'))) return
    onRun(sql)
  }
  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="flex-1 min-h-0 overflow-auto p-3 flex flex-col gap-3 font-mono text-[12.5px]">
        {msgs.length === 0 && <div className="max-w-[56ch] text-ink-2 text-[15px] leading-normal">Ask about this database. Write @table to send that table&apos;s definition along, for example: how many orders per day in @orders.</div>}
        {msgs.map((m, i) => (
          <div key={i} className={`group max-w-[90ch] ${m.role === 'user' ? 'self-end bg-raised px-3 py-2 rounded-[2px]' : ''}`}>
            {parts(m.content).map((p, j) => p.code ? (
              <div key={j} className="my-2 border border-rule bg-panel rounded-[2px]">
                <pre className="m-0 p-2.5 overflow-auto text-[12px] whitespace-pre-wrap">{p.text}</pre>
                {m.role === 'assistant' && <div className="flex gap-2 px-2 py-1 border-t border-rule">
                  <button className="btn-ghost" onClick={() => copyText(p.text).then(() => notify('Copied'), (e) => notify(String(e)))}>Copy</button>
                  <button className="btn-ghost" onClick={() => run(m, p.text)}>Run</button>
                  {m.blocks?.find((x) => x.sql === p.text)?.readOnly === false && <span className="text-ink-2 self-center">changes data</span>}
                </div>}
              </div>) : <p key={j} className="m-0 whitespace-pre-wrap">{p.text}</p>)}
            <div className="flex gap-1 mt-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 max-[760px]:opacity-100">
              <button className="btn-ghost" onClick={() => copyText(m.content).then(() => notify('Copied the message'), (e) => notify(String(e)))}>Copy</button>
              {m.role === 'user' && <button className="btn-ghost" disabled={busy} onClick={() => resend(i)}>Resend</button>}
              <button className="btn-ghost" disabled={busy} onClick={() => setMsgs(msgs.filter((_, j) => j !== i))}>Delete</button>
            </div>
          </div>))}
        {busy && <div className="text-ink-2">Thinking…</div>}
        {err && <div className="text-del">{err}</div>}
        <div ref={end} />
      </div>
      <div className="toolbar bg-canvas">
        {!!meta?.ai?.providers?.length && <label className="flex items-center gap-1.5 text-[12px] text-ink-2">Provider
          <Select className="bg-raised border-transparent" aria-label="Provider" value={provider} onChange={pick} options={[{ value: '', label: `Default${meta.ai.provider ? ` (${meta.ai.provider})` : ''}` }, ...meta.ai.providers]} /></label>}
        <label className="flex items-center gap-1.5 text-[12px] text-ink-2">Model
          <Select className="bg-raised border-transparent" aria-label="Model" value={model} onChange={setModel} options={[{ value: '', label: `Default${meta?.ai?.model ? ` (${meta.ai.model})` : ''}` }, ...(model && !models.includes(model) ? [model] : []), ...models]} /></label>
        <button className="btn-ghost" onClick={loadModels} title="Ask the provider which models it offers">List models</button>
        <span className="text-ink-3 text-[12px]">ACP agents and the Copilot and Codex sign-ins are terminal-only.</span>
      </div>
      <div className="toolbar bg-canvas items-end">
        <textarea className="flex-1 min-w-0 h-16 bg-canvas resize-none" aria-label="Message" placeholder="Ask a question. Ctrl/Cmd+Enter sends." value={text} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); send() } }} />
        {busy ? <button className="btn-danger" onClick={() => ctrl.current?.abort()}>Stop</button> : <button className="btn-primary" disabled={!text.trim()} onClick={send}>Send</button>}
      </div>
    </div>
  )
}
