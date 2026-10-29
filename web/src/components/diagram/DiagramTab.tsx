// ER diagram tab.
import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type DiagramTable, type ObjRef, type Plan, type SessionInfo } from '../../utils/api'
import { DiagramCanvas } from './DiagramCanvas'
import { FkDialog, dropTable } from './ForeignKeyDialog'
import { validColour } from '../../utils/diagram'
import { Select } from '../shell/Select'

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
