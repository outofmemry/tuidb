// Metrics board tab.
import { useCallback, useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Layer, MENU, at as place } from '../shell/Layer'
import { api, download, saveBlob, watchJob, type BoardInfo, type BoardPanel, type ObjRef, type PanelOut, type SessionInfo } from '../../utils/api'
import { ask, askMatch, Dialog } from '../shell/Dialogs'
import { enterSubmits } from '../../utils/forms'
import { isVarPanel, panelVals } from '../../utils/boards'
import { Select } from '../shell/Select'
import { ImportDialog } from '../tools/ImportDialog'
import { Plot } from '../query/Chart'
import { msg } from '../shell/ui'

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
  const [ranAt, setRanAt] = useState<Date | null>(null) // when the board last refreshed and which panels failed
  const [errsOpen, setErrsOpen] = useState(false)
  const errsBtn = useRef<HTMLButtonElement>(null)
  const [opts, setOpts] = useState<Record<string, string[]>>({}) // query-loaded dropdown options by panel title
  const [fmts, setFmts] = useState<Record<string, string>>({})
  const [layout, setLayoutState] = useState<Layout>({ order: [], span: {}, h: {} })
  const [cols, setCols] = useState(3)
  const [form, setForm] = useState<'' | 'new' | 'panel'>('')
  const [importInto, setImportInto] = useState<ObjRef | null>(null)
  const dragging = useRef<number | null>(null)
  const board = (list.data ?? []).find((b) => b.name === name && !b.error)
  const valsRef = useRef(vals); valsRef.current = vals
  useEffect(() => { if (!name && list.data?.length) setName(list.data.find((b) => !b.error)?.name ?? '') }, [list.data, name])
  useEffect(() => { if (board) setVals(panelVals(board.vars, board.panels)) }, [board?.name])
  useEffect(() => { if (name) setLayoutState(loadLayout(name)) }, [name])
  const setLayout = (l: Layout) => { setLayoutState(l); try { localStorage.setItem(layoutKey(name), JSON.stringify(l)) } catch { /* private mode: the arrangement lasts until reload */ } }
  const curBoard = useRef(''); curBoard.current = board?.name ?? ''
  const run = async () => {
    if (!board) return
    const mine = board.name // a slow run of another board must not land on this one
    try { const p = (await api<{ panels: PanelOut[] }>('POST', `/sessions/${runSid}/boards/run`, { Name: mine, Vals: valsRef.current })).panels; if (curBoard.current === mine) { setPanels(p); setErr(''); setRanAt(new Date()) } } catch (e) { if (curBoard.current === mine) setErr(msg(e)) }
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
  // dropdown panels without a fixed list load their options from the panel's query
  const loadOpts = async () => {
    if (!board) return
    const mine = board.name, o: Record<string, string[]> = {}
    for (const p of board.panels) if (p.Kind === 'dropdown' && !p.options?.length && p.Query) {
      try { const r = await api<{ options: string[]; truncated?: boolean }>('GET', `/sessions/${runSid}/boards/${encodeURIComponent(mine)}/options?panel=${encodeURIComponent(p.Title)}`); o[p.Title] = r.options ?? []; if (r.truncated) notify(`${p.Title}: only the first ${r.options.length} options are listed`) } catch (e) { setErr(msg(e)) }
    }
    if (curBoard.current === mine) setOpts(o)
  }
  useEffect(() => { loadOpts() }, [board?.name, runSid]) // eslint-disable-line react-hooks/exhaustive-deps
  // exporter: the server runs the panel's query as an export job, then the file downloads like any other export
  const exportPanel = async (p: BoardPanel) => {
    try {
      const { job } = await api<{ job: string }>('POST', `/sessions/${runSid}/boards/${encodeURIComponent(board!.name)}/export`, { panel: p.Title, format: fmts[p.Title] || p.format || 'csv', vals: valsRef.current })
      const j = await watchJob(runSid, job, () => {})
      if (j.error) throw new Error(j.error)
      const f = await download(`/sessions/${runSid}/jobs/${job}/file`)
      saveBlob(f.blob, f.name); notify('Export saved')
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
    const t = await askMatch(`Type ${board.name} to delete this board`, board.name); if (t === null) return
    if (t !== board.name) return notify('The name did not match; nothing deleted.')
    try { await api('DELETE', `/boards/${encodeURIComponent(board.name)}`); setName(''); setPanels([]); await list.refetch(); notify('Board deleted') } catch (e) { notify(msg(e)) }
  }
  const conns = Object.keys(others)
  const actions = (board?.panels ?? []).filter((p) => ACTION_KINDS.has(p.Kind))
  const failed = panels.filter((p) => p.error)
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
        <button onClick={() => { run(); loadOpts() }} disabled={!board}>Refresh</button>
        {ranAt && <span className="text-ink-2 text-[12px]" role="status">Updated {ranAt.toLocaleTimeString()}</span>}
        {failed.length > 0 && <span className="relative">
          <button ref={errsBtn} className="btn-danger" aria-expanded={errsOpen} aria-haspopup="true" onClick={() => setErrsOpen(!errsOpen)}>{failed.length} error{failed.length === 1 ? '' : 's'}</button>
          {errsOpen && <Layer onClose={() => setErrsOpen(false)}><div className={`${MENU} w-[min(420px,90vw)] p-2 text-[12px]`} style={errsBtn.current ? place(errsBtn.current.getBoundingClientRect(), { width: 420, height: 300 }) : undefined} role="dialog" aria-label="Panel errors" onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); setErrsOpen(false) } }}>
            {failed.map((p) => <div key={p.title} className="mb-1.5"><b className="font-medium">{p.title}</b><div className="text-del whitespace-pre-wrap">{p.error}</div></div>)}
            <button className="btn-ghost" autoFocus onClick={() => setErrsOpen(false)}>Close</button>
          </div></Layer>}
        </span>}
        {actions.map((a) => a.Kind === 'button'
          ? <button key={a.Title} className="btn-primary" onClick={() => runPanel(a.Target ?? '')} title={`Runs the panel ${a.Target}`}>{a.Title}</button>
          : a.Kind === 'import' ? <button key={a.Title} onClick={() => setImportInto(target(a.Table ?? ''))} title={`Import into ${a.Table}`}>{a.Title}</button>
          : a.Kind === 'exporter' ? <span key={a.Title} className="flex items-center gap-1">
              <Select className="bg-raised border-transparent" aria-label={`${a.Title} format`} value={fmts[a.Title] || a.format || 'csv'} onChange={(v) => setFmts({ ...fmts, [a.Title]: v })} options={EXPORT_FORMATS} />
              <button onClick={() => exportPanel(a)} title={`Exports the result of ${a.Title}`}>{a.Title}</button></span>
          : a.var ? <label key={a.Title} className="flex items-center gap-1.5 text-[12px]">{a.Title}
              {a.Kind === 'dropdown'
                ? <Select className="bg-raised border-transparent" aria-label={a.Title} value={vals[a.var] ?? ''} onChange={(x) => { setVals({ ...vals, [a.var!]: x }); setTimeout(() => runRef.current(), 0) }} options={a.options?.length ? a.options : opts[a.Title] ?? []} />
                : <input className="w-28" aria-label={a.Title} value={vals[a.var] ?? ''} onChange={(e) => setVals({ ...vals, [a.var!]: e.target.value })} onKeyDown={(e) => e.key === 'Enter' && run()} />}</label> : null)}
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
  const [p, setP] = useState<BoardPanel>({ Title: '', Kind: 'number', Query: '', Chart: '', Target: '', RefreshOn: '', Table: '', var: '', options: [], format: 'csv', default: '' })
  const [optText, setOptText] = useState('')
  const set = (k: keyof BoardPanel, v: string) => setP({ ...p, [k]: v })
  const titles = (board?.panels ?? []).filter((x) => !ACTION_KINDS.has(x.Kind)).map((x) => x.Title)
  const all = (board?.panels ?? []).map((x) => x.Title)
  const plot = p.Kind === 'bar' || p.Kind === 'line'
  const runs = !ACTION_KINDS.has(p.Kind) // panels the board itself runs
  const hasQuery = runs || p.Kind === 'exporter' || p.Kind === 'dropdown'
  const fixed = optText.split(',').map((x) => x.trim()).filter(Boolean)
  const ok = name.trim() && p.Title.trim() && (runs || p.Kind === 'exporter' ? p.Query.trim() : p.Kind === 'button' ? p.Target : p.Kind === 'import' ? p.Table?.trim() : p.Kind === 'input' ? p.var?.trim() : p.var?.trim() && (fixed.length || p.Query.trim()))
  const go = () => {
    const keep: BoardPanel = { Title: p.Title.trim(), Kind: p.Kind, Query: hasQuery ? p.Query : '', Chart: plot ? p.Chart : '', Target: p.Kind === 'button' ? p.Target : '', RefreshOn: runs ? p.RefreshOn : '', Table: p.Kind === 'import' ? p.Table?.trim() : '',
      ...(isVarPanel(p) ? { var: p.var?.trim(), default: p.default ?? '', options: p.Kind === 'dropdown' ? fixed : [] } : {}), ...(p.Kind === 'exporter' ? { format: p.format || 'csv' } : {}) }
    const b: BoardInfo = { name: name.trim(), refreshSecs: Number(secs) || 0, vars: board?.vars ?? [], panels: [...(board?.panels ?? []), keep], error: '' }
    onSave(board?.name ?? name.trim(), b, name.trim())
  }
  return (
    <Dialog title={mode === 'new' ? 'New board' : `Add a panel to ${board!.name}`} onClose={onClose}>
      <div className="grid grid-cols-[130px_1fr] gap-x-3 gap-y-2 items-center text-[12.5px] [&_label]:text-ink-2" onKeyDown={(e) => enterSubmits(e, () => ok && go())}>
        {mode === 'new' && <><label>Board name</label><input aria-label="Board name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
          <label>Refresh (seconds)</label><input type="number" min={0} value={secs} onChange={(e) => setSecs(e.target.value)} /></>}
        <label>Panel title</label><input aria-label="Panel title" autoFocus={mode === 'panel'} value={p.Title} onChange={(e) => set('Title', e.target.value)} />
        <label>Kind</label><Select aria-label="Kind" value={p.Kind} onChange={(v) => set('Kind', v)} options={KINDS} />
        {hasQuery && <><label>{p.Kind === 'dropdown' ? 'Options query' : 'Query'}</label><textarea className="font-mono min-h-[5em] bg-canvas" aria-label="Query" placeholder={p.Kind === 'dropdown' ? 'select name from regions (first column)' : 'select count(*) from orders'} value={p.Query} onChange={(e) => set('Query', e.target.value)} /></>}
        {plot && <><label>Chart columns</label><input aria-label="Chart columns" placeholder="x: day, y: orders, refunds" value={p.Chart} onChange={(e) => set('Chart', e.target.value)} /></>}
        {p.Kind === 'button' && <><label>Runs panel</label><Select aria-label="Target" value={p.Target ?? ''} onChange={(v) => set('Target', v)} options={[{ value: '', label: 'pick a panel' }, ...titles]} /></>}
        {isVarPanel(p) && <><label>Variable</label><input aria-label="Variable" placeholder="region (used as $region in queries)" value={p.var ?? ''} onChange={(e) => set('var', e.target.value)} />
          <label>Default</label><input aria-label="Default" value={p.default ?? ''} onChange={(e) => set('default', e.target.value)} /></>}
        {p.Kind === 'dropdown' && <><label>Fixed options</label><input aria-label="Fixed options" placeholder="a, b, c (empty: use the query)" value={optText} onChange={(e) => setOptText(e.target.value)} /></>}
        {p.Kind === 'exporter' && <><label>Format</label><Select aria-label="Format" value={p.format || 'csv'} onChange={(v) => set('format', v)} options={EXPORT_FORMATS} /></>}
        {p.Kind === 'import' && <><label>Import into</label><input aria-label="Table" placeholder="table name" value={p.Table ?? ''} onChange={(e) => set('Table', e.target.value)} /></>}
        {runs && all.length > 0 && <><label>Refresh on</label><Select aria-label="Refresh on" value={p.RefreshOn ?? ''} onChange={(v) => set('RefreshOn', v)} options={[{ value: '', label: 'nothing' }, ...all]} /></>}
      </div>
      <div className="flex justify-end gap-2 mt-[18px]"><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={!ok} onClick={go}>Save</button></div>
    </Dialog>
  )
}
