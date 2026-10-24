import { useEffect, useMemo, useRef, useState } from 'react'
import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { api, ApiError, clip, qs, recentKey, type Column, type FilterSet, type Meta, type ObjRef, type Plan, type ResultData, type SessionInfo, type Sort, type Val } from './api'
import { PlanDialog } from './Dialogs'
import { FilterBar } from './FilterBar'
import { Grid, show, type GridRow } from './Grid'
import { CellEditor, type CellTarget } from './CellEditor'

type Edits = Record<string, Record<string, Val>>
type Insert = { id: number; vals: Record<string, Val> }
type View = 'data' | 'structure' | 'json'

export function DataTab({ sid, info, meta, obj, notify, active }: { sid: string; info: SessionInfo; meta: Meta; obj: ObjRef; notify: (m: string) => void; active?: boolean }) {
  const base = `/sessions/${sid}`
  const refQ = qs({ schema: obj.Schema, name: obj.Name, kind: obj.Kind })
  const stateQ = qs({ conn: info.name, db: info.database, table: recentKey(obj) })

  const [view, setView] = useState<View>('data')
  const [filters, setFilters] = useState<FilterSet>({ Filters: [], Or: false })
  const [sort, setSort] = useState<Sort[]>([])
  const [hidden, setHidden] = useState<string[]>([])
  const [showBar, setShowBar] = useState(false)
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(info.rowLimit)
  const [nonce, setNonce] = useState(0)
  const [exact, setExact] = useState(false)
  const [ready, setReady] = useState(false)

  const [edits, setEdits] = useState<Edits>({})
  const [deletes, setDeletes] = useState<Set<string>>(new Set())
  const [inserts, setInserts] = useState<Insert[]>([])
  const [selected, setSelected] = useState<string | null>(null)
  const [detail, setDetail] = useState(false)
  const [cell, setCell] = useState<CellTarget | null>(null)
  const [trayOpen, setTrayOpen] = useState(true)
  const [plan, setPlan] = useState<Plan | null>(null)
  const nextId = useRef(1)
  const colsMenu = useRef<HTMLDetailsElement>(null)

  // restore what the TUI (or an earlier visit) remembered for this table
  const saved = useQuery({ queryKey: ['tstate', sid, obj], queryFn: () => api<any>('GET', '/state/table' + stateQ), staleTime: Infinity, gcTime: 0 })
  useEffect(() => {
    if (saved.isSuccess && !ready) {
      const s = saved.data
      setFilters({ Filters: s.filters?.Filters ?? [], Or: !!s.filters?.Or })
      setSort(s.sort ?? []); setHidden(s.hidden ?? []); setShowBar(!!s.show_filters); setReady(true)
    }
    if (saved.isError && !ready) setReady(true)
  }, [saved.isSuccess, saved.isError, saved.data, ready])
  useEffect(() => {
    if (!ready) return
    const t = setTimeout(() => api('PUT', '/state/table' + stateQ, { hidden, filters, show_filters: showBar, sort }).catch(() => {}), 500)
    return () => clearTimeout(t)
  }, [ready, hidden, filters, showBar, sort, stateQ])

  const cols = useQuery({ queryKey: ['cols', sid, obj], queryFn: () => api<Column[]>('GET', `${base}/object/columns${refQ}`), gcTime: 0 })
  const colNames = useMemo(() => (cols.data ?? []).map((c) => c.Name), [cols.data])
  const visible = useMemo(() => colNames.filter((n) => !hidden.includes(n)), [colNames, hidden])
  const hasActive = filters.Filters.some((f) => f.Enabled)

  const rowsQ = useQuery({
    queryKey: ['rows', sid, obj, filters, sort, hidden, offset, limit, nonce],
    queryFn: ({ signal }) => api<ResultData>('POST', `${base}/rows`, { ref: obj, filters, sort, columns: hidden.length ? visible : [], limit, offset }, signal),
    enabled: ready && cols.isSuccess, placeholderData: keepPreviousData, gcTime: 0,
  })
  const countQ = useQuery({
    queryKey: ['count', sid, obj, filters, nonce, exact],
    queryFn: () => api<{ n: number; exact: boolean }>('POST', `${base}/count`, { ref: obj, filters, estimate: !exact && !hasActive }),
    enabled: ready && cols.isSuccess, gcTime: 0,
  })

  const res = rowsQ.data
  const pending = Object.keys(edits).length + deletes.size + inserts.length
  const clearPending = () => { setEdits({}); setDeletes(new Set()); setInserts([]); setSelected(null) }
  const guard = () => pending === 0 || window.confirm('Discard pending changes?')
  const reload = () => { if (guard()) { clearPending(); setNonce((n) => n + 1) } }
  const change = <T,>(set: (v: T) => void) => (v: T) => { if (guard()) { clearPending(); setOffset(0); set(v) } }

  const rows: GridRow[] = useMemo(() => {
    if (!res) return []
    const server = res.cells.map((cells, i): GridRow => ({ key: 'r' + i, cells, state: deletes.has('r' + i) ? 'deleted' : 'clean' }))
    const fresh = inserts.map((n): GridRow => ({ key: 'n' + n.id, cells: res.columns.map(() => ''), state: 'new' }))
    return [...server, ...fresh]
  }, [res, deletes, inserts])
  const editedAll: Edits = useMemo(() => {
    const m: Edits = { ...edits }
    for (const n of inserts) m['n' + n.id] = n.vals
    return m
  }, [edits, inserts])

  // every pending change as a diff line, so nothing is sent unseen
  type Mark = { id: string; kind: 'update' | 'delete' | 'insert'; where: string; col?: string; from?: string | null; to?: string | null; detail?: string; revert: () => void }
  const marks: Mark[] = useMemo(() => {
    if (!res) return []
    const pkIdx = res.columns.map((c, i) => (c.pk ? i : -1)).filter((i) => i >= 0)
    const at = (key: string) => {
      const n = Number(key.slice(1))
      const cells = res.cells[n] ?? []
      return pkIdx.length ? pkIdx.map((i) => `${res.columns[i].name} ${cells[i] ?? 'NULL'}`).join(', ') : `row ${n + 1}`
    }
    const out: Mark[] = []
    for (const key of deletes) out.push({ id: 'd' + key, kind: 'delete', where: at(key), revert: () => setDeletes((s) => { const x = new Set(s); x.delete(key); return x }) })
    for (const [key, set] of Object.entries(edits)) {
      if (deletes.has(key)) continue
      const cells = res.cells[Number(key.slice(1))] ?? []
      for (const [col, v] of Object.entries(set)) {
        const ci = res.columns.findIndex((c) => c.name === col)
        out.push({
          id: `u${key}.${col}`, kind: 'update', where: at(key), col, from: cells[ci] ?? null, to: show(v, null),
          revert: () => setEdits((e) => { const { [col]: _gone, ...rest } = e[key]; const n = { ...e }; if (Object.keys(rest).length) n[key] = rest; else delete n[key]; return n }),
        })
      }
    }
    for (const n of inserts) {
      const vals = Object.entries(n.vals).map(([c, v]) => `${c} = ${show(v, null) ?? 'NULL'}`)
      out.push({ id: 'n' + n.id, kind: 'insert', where: 'new row', detail: vals.length ? vals.join(', ') : 'column defaults', revert: () => setInserts((l) => l.filter((x) => x.id !== n.id)) })
    }
    return out
  }, [res, edits, deletes, inserts])

  const onEdit = (key: string, col: string, v: Val) => {
    if (key[0] === 'n') setInserts((l) => l.map((n) => ('n' + n.id === key ? { ...n, vals: { ...n.vals, [col]: v } } : n)))
    else setEdits((e) => ({ ...e, [key]: { ...e[key], [col]: v } }))
  }
  const onSort = (col: string, additive: boolean) => change<Sort[]>(setSort)((() => {
    const cur = sort.find((s) => s.Column === col)
    const rest = additive ? sort.filter((s) => s.Column !== col) : []
    if (!cur) return [...rest, { Column: col, Desc: false }]
    return cur.Desc ? rest : [...rest, { Column: col, Desc: true }]
  })())

  const addRow = () => { const id = nextId.current++; setInserts((l) => [...l, { id, vals: {} }]); setSelected('n' + id) }
  const duplicate = () => {
    if (!selected || !res) return
    const src = rows.find((r) => r.key === selected); if (!src) return
    const pk = new Set(res.columns.filter((c) => c.pk).map((c) => c.name))
    const vals: Record<string, Val> = {}
    res.columns.forEach((c, i) => {
      if (pk.has(c.name)) return
      const v = show(editedAll[src.key]?.[c.name], src.cells[i])
      vals[c.name] = v === null ? { $: 'null' } : v
    })
    const id = nextId.current++; setInserts((l) => [...l, { id, vals }]); setSelected('n' + id)
  }
  const del = () => {
    if (!selected) return
    if (selected[0] === 'n') { setInserts((l) => l.filter((n) => 'n' + n.id !== selected)); setSelected(null); return }
    setDeletes((d) => { const x = new Set(d); x.has(selected) ? x.delete(selected) : x.add(selected); return x })
  }

  const body = () => ({
    resultId: res?.resultId,
    updates: Object.entries(edits).filter(([k]) => !deletes.has(k)).map(([k, set]) => ({ row: Number(k.slice(1)), set })),
    deletes: [...deletes].map((k) => Number(k.slice(1))),
    inserts: inserts.map((n) => n.vals),
  })
  const makePlan = async (): Promise<Plan | null> => {
    try { return await api<Plan>('POST', `${base}/plan`, body()) } catch (e) { notify(e instanceof Error ? e.message : String(e)); return null }
  }
  const applied = () => { setPlan(null); clearPending(); setNonce((n) => n + 1); notify('Changes sent') }
  const previewSql = async () => { const p = await makePlan(); if (p) setPlan(p) }
  const commit = async () => {
    const p = await makePlan(); if (!p) return
    if (p.gate.confirm || p.gate.password) { setPlan(p); return }
    try { await api('POST', `${base}/apply`, { planId: p.planId, confirmed: true }); applied() } catch (e) { notify(e instanceof ApiError ? e.message : String(e)) }
  }

  const total = countQ.data
  const from = res && res.cells.length ? offset + 1 : 0
  const to = offset + (res?.cells.length ?? 0)
  const ops = meta.operators[info.caps.Dialect] ?? meta.operators.postgres

  // prefix-key actions, answered only while this tab is the visible one
  const selRow = rows.find((r) => r.key === selected)
  const keyActions: Record<string, () => void> = {
    reload_tab: reload, filters: () => setShowBar(!showBar), columns: () => { if (colsMenu.current) colsMenu.current.open = !colsMenu.current.open },
    add_row: addRow, duplicate_rows: duplicate, delete_rows: del, commit, preview: previewSql, discard: clearPending,
    undo: () => marks[marks.length - 1]?.revert(),
    next_page: () => { if (res && res.cells.length >= limit) change<number>(setOffset)(offset + limit) },
    prev_page: () => { if (offset > 0) change<number>(setOffset)(Math.max(0, offset - limit)) },
    first_row: () => rows[0] && setSelected(rows[0].key), last_row: () => rows.length && setSelected(rows[rows.length - 1].key),
    count_exact: () => setExact(true), show_data: () => setView('data'), show_structure: () => setView('structure'),
    show_json: () => info.caps.Document && setView('json'),
    toggle_row_detail: () => setDetail(!detail),
    quick_look: () => { if (selRow) setCell({ row: selRow.key, col: res!.columns[0].name, readOnly: true }) },
  }
  useEffect(() => {
    if (!active) return
    const h = (e: Event) => { const f = keyActions[(e as CustomEvent).detail.action]; if (f) { e.preventDefault(); f() } }
    window.addEventListener('tuidb:action', h)
    return () => window.removeEventListener('tuidb:action', h)
  })

  return (
    <div className="pane">
      <div className="toolbar">
        <button onClick={reload}>Reload</button>
        <button onClick={() => setShowBar(!showBar)}>{showBar ? 'Hide filters' : 'Filters'}{hasActive ? ' (on)' : ''}</button>
        <ColumnsMenu all={colNames} hidden={hidden} onChange={change<string[]>(setHidden)} menuRef={colsMenu} />
        <select value={view} onChange={(e) => setView(e.target.value as View)} aria-label="View">
          <option value="data">Data</option><option value="structure">Structure</option>
          {info.caps.Document && <option value="json">JSON</option>}
        </select>
        <span className="grow" />
        {view === 'data' && res?.editable && <>
          <button onClick={addRow}>Add row</button>
          <button onClick={duplicate} disabled={!selected}>Duplicate row</button>
          <button onClick={del} disabled={!selected}>{selected && deletes.has(selected) ? 'Keep row' : 'Delete row'}</button>
        </>}
      </div>
      {showBar && view === 'data' && <FilterBar value={filters} columns={colNames} operators={ops} onApply={change<FilterSet>(setFilters)} />}
      {(rowsQ.isFetching || cols.isLoading) && <div className="progress" role="progressbar" aria-label="Loading rows" />}
      {!res && !rowsQ.error && <div className="note dim">Loading rows…</div>}
      {rowsQ.error && <div className="err note">{rowsQ.error.message}</div>}
      {view === 'data' && res && res.cells.length === 0 && inserts.length === 0 && (
        <div className="empty">{hasActive ? 'No rows match these filters.' : 'This table has no rows yet. Add one to start.'}</div>
      )}
      {view === 'data' && res && (res.cells.length > 0 || inserts.length > 0) && (
        <div className="body">
          <Grid columns={res.columns} rows={rows} sort={sort} onSort={onSort} edited={editedAll} editable={res.editable} onOpen={setCell}
            selected={selected} onSelect={setSelected} onSpace={() => setDetail(!detail)} />
          {detail && (
            <div className="detail" role="region" aria-label="Row details">
              <div className="toolbar"><strong className="grow">Row details</strong><button className="ghost" onClick={() => setDetail(false)} aria-label="Close">✕</button></div>
              <div className="scroll">
                {!selRow && <div className="note dim">Select a row to see its fields.</div>}
                {selRow && res.columns.map((c, i) => {
                  const ed = editedAll[selRow.key]?.[c.name]
                  const val = show(ed, selRow.cells[i])
                  return (
                    <div key={c.name} className={`field ${ed !== undefined ? 'edited' : ''}`} onClick={() => setCell({ row: selRow.key, col: c.name, readOnly: !res.editable || selRow.state === 'deleted' })}
                      title={res.editable ? 'Click to edit' : undefined}>
                      <div className="name">{c.pk && <span className="pk">◆</span>}{c.name}<span className="dim"> {c.type}</span></div>
                      <div className={`val mono ${val === null ? 'null' : ''}`}>{val === null ? 'NULL' : val}</div>
                    </div>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      )}
      {cell && res && (() => {
        const r = rows.find((x) => x.key === cell.row); const ci = res.columns.findIndex((c) => c.name === cell.col)
        return r && ci >= 0 ? <CellEditor col={cell.col} readOnly={cell.readOnly} value={show(editedAll[r.key]?.[cell.col], r.cells[ci])}
          onDone={(v) => { setCell(null); if (v !== undefined) onEdit(cell.row, cell.col, v) }} /> : null
      })()}
      {view === 'json' && <pre className="docs">{(res?.docs ?? []).map((d) => { try { return JSON.stringify(JSON.parse(d), null, 2) } catch { return d } }).join('\n')}</pre>}
      {view === 'structure' && <Structure sid={sid} refQ={refQ} caps={info.caps} columns={cols.data ?? []} />}
      {view === 'data' && marks.length > 0 && (
        <div className="tray" role="region" aria-label="Staged changes">
          <div className="bar">
            <strong><b>{marks.length}</b>staged change{marks.length === 1 ? '' : 's'}</strong>
            <span className="grow dim">Not sent to the database yet.</span>
            <button onClick={() => setTrayOpen(!trayOpen)} aria-expanded={trayOpen}>{trayOpen ? 'Hide list' : 'Show list'}</button>
            <button onClick={previewSql}>Preview SQL</button>
            <button onClick={clearPending}>Discard all</button>
            <button className="primary" onClick={commit}>Send changes</button>
          </div>
          {trayOpen && (
            <div className="list mono">
              {marks.map((m) => (
                <div className="item" key={m.id}>
                  <span className="what">
                    {m.kind === 'insert' && <><span className="plus">+ </span>{clip(m.detail ?? '', 160)}</>}
                    {m.kind === 'delete' && <del>remove {m.where}</del>}
                    {m.kind === 'update' && <><span className="where">{m.col} on {m.where}:  </span><del>{clip(m.from ?? 'NULL', 80)}</del> <ins>{clip(m.to ?? 'NULL', 80)}</ins></>}
                  </span>
                  <button className="ghost" onClick={m.revert} aria-label={`Undo change to ${m.where}`}>Undo</button>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
      {view === 'data' && (
        <div className="toolbar foot">
          <button disabled={offset === 0} onClick={() => change<number>(setOffset)(Math.max(0, offset - limit))} aria-label="Previous page">Prev</button>
          <span><span className="n">{from}–{to}</span>{total ? <> of <span className="n">{total.exact ? '' : '~'}{total.n}</span></> : ''}</span>
          <button disabled={!res || res.cells.length < limit} onClick={() => change<number>(setOffset)(offset + limit)} aria-label="Next page">Next</button>
          {total && !total.exact && <button className="ghost" onClick={() => setExact(true)}>Count exactly</button>}
          <label className="dim">Rows per page <input type="number" min={1} style={{ width: 70 }} defaultValue={limit}
            onBlur={(e) => { const n = Number(e.target.value); if (n > 0 && n !== limit) change<number>(setLimit)(n) }} /></label>
          {res?.truncated && <span className="dim">Stopped at the row limit.</span>}
          {rowsQ.isFetching && <span className="dim">Loading…</span>}
        </div>
      )}
      {plan && <PlanDialog sid={sid} plan={plan} onClose={() => setPlan(null)} onApplied={applied} />}
    </div>
  )
}

function ColumnsMenu({ all, hidden, onChange, menuRef }: { all: string[]; hidden: string[]; onChange: (h: string[]) => void; menuRef?: React.Ref<HTMLDetailsElement> }) {
  return (
    <details ref={menuRef} style={{ position: 'relative' }}>
      <summary className="btnlike">Columns{hidden.length ? ` (${all.length - hidden.length}/${all.length})` : ''}</summary>
      <div className="popover">
        {all.map((c) => (
          <label key={c} className="row"><input type="checkbox" checked={!hidden.includes(c)} onChange={(e) => onChange(e.target.checked ? hidden.filter((h) => h !== c) : [...hidden, c])} />{c}</label>
        ))}
      </div>
    </details>
  )
}

function Structure({ sid, refQ, caps, columns }: { sid: string; refQ: string; caps: SessionInfo['caps']; columns: Column[] }) {
  const get = (what: string, on: boolean) => useQuery({ queryKey: ['obj', sid, what, refQ], queryFn: () => api<any[]>('GET', `/sessions/${sid}/object/${what}${refQ}`), enabled: on, gcTime: 0 })
  const idx = get('indexes', caps.Indexes), trg = get('triggers', caps.Triggers)
  return (
    <div className="structure" data-ctx="structure">
      <h4>Columns</h4>
      <table><thead><tr><th>Name</th><th>Type</th><th>Null</th><th>Default</th><th>Key</th><th>Extra</th><th>References</th></tr></thead><tbody>
        {columns.map((c) => <tr key={c.Name}><td>{c.Name}</td><td>{c.Type}</td><td>{c.Nullable ? 'yes' : 'no'}</td><td>{c.HasDef ? c.Default : ''}</td><td>{c.PK ? 'PK' : ''}</td><td>{c.Extra}</td>
          <td>{c.FK ? `${c.FK.RefTable}(${c.FK.RefColumns.join(', ')})` : ''}</td></tr>)}
      </tbody></table>
      {caps.Indexes && <><h4>Indexes</h4><table><tbody>{(idx.data ?? []).map((i) => <tr key={i.Name}><td>{i.Name}</td><td>{(i.Columns ?? []).join(', ')}</td><td>{i.Primary ? 'primary' : i.Unique ? 'unique' : ''}</td><td>{i.Algorithm}</td></tr>)}</tbody></table></>}
      {caps.Triggers && <><h4>Triggers</h4><table><tbody>{(trg.data ?? []).map((t) => <tr key={t.Name}><td>{t.Name}</td><td>{t.Timing} {t.Event}</td></tr>)}</tbody></table></>}
    </div>
  )
}
