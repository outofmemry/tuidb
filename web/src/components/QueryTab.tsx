import { useEffect, useRef, useState } from 'react'
import CodeMirror from '@uiw/react-codemirror'
import { MySQL, PostgreSQL, StandardSQL, sql } from '@codemirror/lang-sql'
import { Prec } from '@codemirror/state'
import { keymap, type EditorView } from '@codemirror/view'
import { api, ApiError, type ExecItem, type Gate, type SessionInfo } from '../utils/api'
import { Dialog, GateFields, ask } from './Dialogs'
import { Select, type SelectHandle } from './Select'
import { Grid, type GridRow } from './Grid'
import { CellEditor, type CellTarget } from './CellEditor'

type Pending = { body: Record<string, unknown>; gate: Gate; statements: string[]; label: string; error?: string }

export function QueryTab({ sid, info, initial, notify, shown }: { sid: string; info: SessionInfo; initial: string; notify: (m: string) => void; shown?: boolean }) {
  const [text, setText] = useState(initial)
  const [results, setResults] = useState<ExecItem[]>([])
  const [active, setActive] = useState(0)
  const [error, setError] = useState('')
  const [running, setRunning] = useState(false)
  const [limit, setLimit] = useState(0)
  const [gate, setGate] = useState<Pending | null>(null)
  const [password, setPassword] = useState('')
  const [json, setJson] = useState(false)
  const [sel, setSel] = useState<string | null>(null)
  const [cell, setCell] = useState<CellTarget | null>(null)
  const view = useRef<EditorView | null>(null)
  const ctrl = useRef<AbortController | null>(null)

  const send = async (body: Record<string, unknown>) => {
    if (running) return
    const mine = ctrl.current = new AbortController()
    setRunning(true); setError(''); setGate(null)
    const t0 = performance.now()
    try {
      const r = await api<{ results: ExecItem[] }>('POST', `/sessions/${sid}/exec`, { ...body, limit }, mine.signal)
      setResults(r.results); setJson(false); setActive(Math.max(0, r.results.findIndex((x) => x.error))); setPassword('')
      if (!r.results.length) setError('Nothing ran. Type a statement first.')
    } catch (e) {
      if (e instanceof DOMException && e.name === 'AbortError') setError(`Cancelled after ${Math.round(performance.now() - t0)} ms.`)
      else if (e instanceof ApiError && e.status === 409 && e.body.gate) {
        setGate({ body, gate: e.body.gate, statements: e.body.statements ?? [], label: e.body.level ?? '', error: password ? e.message : undefined })
      } else setError(e instanceof Error ? e.message : String(e))
    } finally { setRunning(false) }
  }
  useEffect(() => () => ctrl.current?.abort(), [])

  const run = (all: boolean) => {
    const v = view.current; if (!v) return
    const doc = v.state.doc.toString(), sel = v.state.selection.main
    if (!sel.empty) return send({ text: v.state.sliceDoc(sel.from, sel.to) })
    if (all) return send({ text: doc })
    // the server cuts statements at byte offsets, so convert the cursor from UTF-16 units
    send({ text: doc, current: true, offset: new TextEncoder().encode(doc.slice(0, sel.head)).length })
  }
  const runRef = useRef(run); runRef.current = run
  const limitBox = useRef<SelectHandle>(null)
  useEffect(() => {
    if (!shown) return
    const acts: Record<string, () => void> = {
      run_current: () => run(false), run_all: () => run(true), cancel_query: () => ctrl.current?.abort(),
      add_to_queries: () => { if (text.trim()) save() }, set_limit: () => limitBox.current?.open(),
    }
    const h = (e: Event) => { const f = acts[(e as CustomEvent).detail.action]; if (f) { e.preventDefault(); f() } }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  })

  const save = async () => {
    const name = await ask('Save query as'); if (!name) return
    try { await api('POST', '/queries', { Name: name, SQL: text }); notify('Saved to Queries') } catch (e) { notify(String(e)) }
  }

  const dialect = info.caps.Dialect
  const ext = [
    Prec.highest(keymap.of([
      { key: 'Mod-Enter', run: () => { runRef.current(false); return true } },
      { key: 'Mod-Shift-Enter', run: () => { runRef.current(true); return true } },
    ])),
    ...(info.caps.Editor === 'sql' ? [sql({ dialect: dialect === 'postgres' ? PostgreSQL : dialect === 'mysql' ? MySQL : StandardSQL })] : []),
  ]
  const cur = results[active]

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      <div className="toolbar bg-canvas">
        <button className="btn-primary" disabled={running} onClick={() => run(false)} title="Ctrl/Cmd+Enter">Run</button>
        <button disabled={running} onClick={() => run(true)} title="Ctrl/Cmd+Shift+Enter">Run all</button>
        <button disabled={!running} onClick={() => ctrl.current?.abort()}>Cancel</button>
        <Select ref={limitBox} className="bg-raised border-transparent" title="row limit" aria-label="Row limit" value={String(limit)} onChange={(v) => setLimit(Number(v))}
          options={[{ value: '0', label: `Limit ${info.rowLimit}` }, '100', '1000', '10000', { value: '-1', label: 'No limit' }]} />
        <span className="flex-1 min-w-0" />
        <button onClick={save} disabled={!text.trim()}>Save query</button>
      </div>
      <div className="border-b border-rule" data-ctx="editor">
        <CodeMirror value={text} height="220px" theme={isDark() ? 'dark' : 'light'} extensions={ext} onChange={setText} onCreateEditor={(v) => { view.current = v }}
          basicSetup={{ lineNumbers: true, foldGutter: false }} />
      </div>
      <div className="flex h-8 text-[12px] border-b border-rule overflow-x-auto [scrollbar-width:none] bg-canvas font-mono">
        {results.map((r, i) => (
          <div key={i} className={`flex items-center gap-2 pl-3.5 pr-2.5 cursor-pointer whitespace-nowrap border-r border-rule ${i === active ? 'text-ink bg-panel shadow-[inset_0_-2px_0_var(--accent)]' : 'text-ink-2 hover:text-ink hover:bg-panel'}`} onClick={() => setActive(i)} title={r.stmt}>
            <span className={r.error ? 'text-del' : ''}>{r.error ? '✗' : '✓'}</span>{i + 1}: {r.stmt.replace(/\s+/g, ' ').slice(0, 28)}
          </div>
        ))}
        {cur?.docs?.length ? <button className="btn-ghost" onClick={() => setJson(!json)}>{json ? 'Rows' : 'JSON'}</button> : null}
      </div>
      {error && <div className="p-2.5 text-[12.5px] text-del">{error}</div>}
      {running && <><div className="relative h-[2px] -mb-[2px] z-[4] overflow-hidden bg-[color-mix(in_srgb,var(--accent)_20%,transparent)] after:absolute after:inset-0 after:w-[40%] after:bg-accent after:animate-slide motion-reduce:after:animate-none motion-reduce:after:w-full motion-reduce:after:opacity-60" role="progressbar" aria-label="Running" /><div className="p-2.5 text-[12.5px] text-ink-2">Running…</div></>}
      {cur && (cur.error ? <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch] text-del">{cur.error}</pre>
        : cur.hasRows ? (
          json && cur.docs ? <pre className="flex-1 overflow-auto m-0 px-4 py-3 text-[12px] max-w-[100ch]">{cur.docs.map(pretty).join('\n')}</pre> : <>
            <Grid ctx="results" columns={cur.columns} rows={cur.cells.map((cells, i): GridRow => ({ key: 'r' + i, cells, state: 'clean' }))} selected={sel} onSelect={setSel} onOpen={setCell} />
            {cell && <CellEditor col={cell.col} readOnly value={cur.cells[Number(cell.row.slice(1))]?.[cur.columns.findIndex((c) => c.name === cell.col)] ?? null} onDone={() => setCell(null)} />}
            <div className="toolbar bg-canvas text-ink-2">{cur.cells.length} rows in {cur.durationMs} ms{cur.truncated ? '. Stopped at the row limit.' : ''}</div>
          </>
        ) : <div className="p-2.5 text-[12.5px]">{cur.message || `${cur.affected} rows affected`} <span className="text-ink-2">in {cur.durationMs} ms</span></div>)}
      {gate && (
        <Dialog title="Run these statements?" onClose={() => setGate(null)}>
          <pre>{gate.statements.join('\n')}</pre>
          <GateFields gate={gate.gate} label={gate.label} password={password} setPassword={setPassword} />
          {gate.error && <div className="text-del" style={{ marginTop: 8 }}>{gate.error}</div>}
          <div className="flex justify-end gap-2 mt-[18px]">
            <button onClick={() => setGate(null)}>Cancel</button>
            <button className="btn-primary" disabled={gate.gate.password && !password} onClick={() => send({ ...gate.body, confirmed: true, password })}>Run</button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

const isDark = () => {
  const t = document.documentElement.dataset.theme
  return t === 'dark' || (t !== 'light' && matchMedia('(prefers-color-scheme: dark)').matches)
}
const pretty = (d: string) => { try { return JSON.stringify(JSON.parse(d), null, 2) } catch { return d } }
