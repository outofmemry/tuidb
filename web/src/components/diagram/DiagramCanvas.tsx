// The ER diagram canvas: an SVG you pan (drag the background), zoom (wheel or the buttons) and rearrange (drag a table's header).
// Boxes are coloured by foreign-key cluster; positions are remembered per database in localStorage. Drag from the dot at the end of a
// column onto another table to add a foreign key (the caller opens the add-foreign-key flow).
import { useEffect, useMemo, useRef, useState } from 'react'
import type { DiagramTable, ObjRef } from '../../utils/api'
import { validColour } from '../../utils/diagram'
import { copyText } from '../../utils/copy'

const BW = 230, HEAD = 28, ROWH = 20, GAP = 60
type Pos = { x: number; y: number }
type View = { x: number; y: number; k: number }
type Drag =
  | { kind: 'pan'; sx: number; sy: number; vx: number; vy: number }
  | { kind: 'box'; key: string; ox: number; oy: number }
  | { kind: 'link'; table: DiagramTable; col: string }

const tk = (r: ObjRef) => r.Schema + '.' + r.Name
const boxH = (t: DiagramTable) => HEAD + t.columns.length * ROWH + 6
const hue = (t: DiagramTable) => (t.cluster * 67) % 360
// a colour the user picked for a table (right-click) beats the cluster tint
const edge = (t: DiagramTable, c?: string) => c ?? `hsl(${hue(t)} 55% 55%)`

// tables of one FK cluster sit together, in rows of about sqrt(n) boxes
function autoLayout(tables: DiagramTable[]): Record<string, Pos> {
  const sorted = [...tables].sort((a, b) => a.cluster - b.cluster || a.ref.Name.localeCompare(b.ref.Name))
  const per = Math.max(1, Math.ceil(Math.sqrt(sorted.length))), out: Record<string, Pos> = {}
  let y = 0, rowMax = 0
  sorted.forEach((t, i) => {
    if (i && i % per === 0) { y += rowMax + GAP; rowMax = 0 }
    out[tk(t.ref)] = { x: (i % per) * (BW + GAP), y }
    rowMax = Math.max(rowMax, boxH(t))
  })
  return out
}

const load = (k: string): Record<string, Pos> => { try { return JSON.parse(localStorage.getItem(k) ?? '{}') } catch { return {} } }

type Ctx = { x: number; y: number; t: DiagramTable; col?: string }

export function DiagramCanvas({ tables, storageKey, query, centerOn, canLink, colors = {}, onColor, onDelete, onNote, onStructure, onLink }: {
  tables: DiagramTable[]; storageKey: string; query: string; centerOn?: string; canLink: boolean
  colors?: Record<string, string>; onColor?: (table: string, colour: string) => void; onDelete?: (t: DiagramTable) => void; onNote?: (m: string) => void
  onStructure: (r: ObjRef) => void; onLink: (from: DiagramTable, col: string, to: DiagramTable) => void
}) {
  const [ctx, setCtx] = useState<Ctx | null>(null)
  const key = 'webtuidb-er:' + storageKey
  const [pos, setPos] = useState<Record<string, Pos>>(() => ({ ...autoLayout(tables), ...load(key) }))
  const [view, setView] = useState<View>({ x: 20, y: 20, k: 1 })
  const [ghost, setGhost] = useState<Pos | null>(null) // the rubber band of a foreign key being dragged
  const svg = useRef<SVGSVGElement>(null), drag = useRef<Drag | null>(null), moved = useRef(false)
  const byKey = useMemo(() => new Map(tables.map((t) => [tk(t.ref), t])), [tables])

  // tables that appeared since the layout was saved get a place; saved places are kept
  useEffect(() => { setPos((p) => { const a = autoLayout(tables); const n: Record<string, Pos> = {}; for (const t of tables) n[tk(t.ref)] = p[tk(t.ref)] ?? a[tk(t.ref)]; return n }) }, [tables])
  useEffect(() => { try { localStorage.setItem(key, JSON.stringify(pos)) } catch { /* private mode: the layout lasts until reload */ } }, [pos, key])

  const world = (e: { clientX: number; clientY: number }): Pos => {
    const r = svg.current!.getBoundingClientRect()
    return { x: (e.clientX - r.left - view.x) / view.k, y: (e.clientY - r.top - view.y) / view.k }
  }
  const zoomAt = (f: number, cx?: number, cy?: number) => setView((v) => {
    const r = svg.current?.getBoundingClientRect(), mx = cx ?? (r?.width ?? 0) / 2, my = cy ?? (r?.height ?? 0) / 2
    const k = Math.min(2.5, Math.max(0.2, v.k * f))
    return { k, x: mx - (mx - v.x) * (k / v.k), y: my - (my - v.y) * (k / v.k) }
  })
  // a React onWheel is passive and cannot stop the page from scrolling
  useEffect(() => {
    const el = svg.current; if (!el) return
    const on = (e: WheelEvent) => { e.preventDefault(); const r = el.getBoundingClientRect(); zoomAt(Math.exp(-e.deltaY * 0.0015), e.clientX - r.left, e.clientY - r.top) }
    el.addEventListener('wheel', on, { passive: false })
    return () => el.removeEventListener('wheel', on)
  }, [])
  const fit = () => {
    const r = svg.current?.getBoundingClientRect(); if (!r || !tables.length) return
    const xs = tables.map((t) => pos[tk(t.ref)]).filter(Boolean)
    const x0 = Math.min(...xs.map((p) => p.x)), y0 = Math.min(...xs.map((p) => p.y))
    const x1 = Math.max(...tables.map((t) => (pos[tk(t.ref)]?.x ?? 0) + BW)), y1 = Math.max(...tables.map((t) => (pos[tk(t.ref)]?.y ?? 0) + boxH(t)))
    const k = Math.min(1.2, Math.max(0.2, Math.min((r.width - 40) / (x1 - x0), (r.height - 40) / (y1 - y0))))
    setView({ k, x: 20 - x0 * k + (r.width - 40 - (x1 - x0) * k) / 2, y: 20 - y0 * k })
  }
  useEffect(() => { fit() }, [tables.length]) // eslint-disable-line react-hooks/exhaustive-deps
  // opened from a table's menu: that table sits in the middle of the canvas at full size
  useEffect(() => {
    const t = centerOn && byKey.get(centerOn), p = t && pos[tk(t.ref)], r = svg.current?.getBoundingClientRect()
    if (t && p && r) setView({ k: 1, x: r.width / 2 - (p.x + BW / 2), y: r.height / 2 - (p.y + boxH(t) / 2) })
  }, [centerOn, tables.length]) // eslint-disable-line react-hooks/exhaustive-deps

  const down = (e: React.PointerEvent) => {
    const t = e.target as Element
    if (t.closest('[data-open]')) return // the Open button is a plain click
    const link = canLink ? t.closest<SVGElement>('[data-link]') : null, head = t.closest<SVGElement>('[data-head]')
    moved.current = false
    if (link) { const b = byKey.get(link.dataset.t!); if (b) drag.current = { kind: 'link', table: b, col: link.dataset.link! } }
    else if (head) { const k = head.dataset.head!, w = world(e); drag.current = { kind: 'box', key: k, ox: w.x - pos[k].x, oy: w.y - pos[k].y } }
    else drag.current = { kind: 'pan', sx: e.clientX, sy: e.clientY, vx: view.x, vy: view.y }
    svg.current!.setPointerCapture(e.pointerId)
  }
  const move = (e: React.PointerEvent) => {
    const d = drag.current; if (!d) return
    moved.current = true
    if (d.kind === 'pan') setView((v) => ({ ...v, x: d.vx + e.clientX - d.sx, y: d.vy + e.clientY - d.sy }))
    else if (d.kind === 'box') { const w = world(e); setPos((p) => ({ ...p, [d.key]: { x: w.x - d.ox, y: w.y - d.oy } })) }
    else setGhost(world(e))
  }
  const up = (e: React.PointerEvent) => {
    const d = drag.current; drag.current = null; setGhost(null)
    if (d?.kind === 'link' && moved.current) {
      const w = world(e)
      const hit = tables.find((t) => { const p = pos[tk(t.ref)]; return p && w.x >= p.x && w.x <= p.x + BW && w.y >= p.y && w.y <= p.y + boxH(t) })
      if (hit && hit !== d.table) onLink(d.table, d.col, hit)
    }
  }

  const q = query.trim().toLowerCase()
  const tint = (t: DiagramTable) => validColour(colors[tk(t.ref)])
  const anchor = (t: DiagramTable, col: string | undefined, side: 'l' | 'r'): Pos => {
    const p = pos[tk(t.ref)] ?? { x: 0, y: 0 }, i = col ? t.columns.findIndex((c) => c.name === col) : -1
    return { x: p.x + (side === 'r' ? BW : 0), y: p.y + (i >= 0 ? HEAD + i * ROWH + ROWH / 2 : HEAD / 2) }
  }
  const edges = tables.flatMap((t) => t.fks.map((f, i) => {
    const to = byKey.get(f.refSchema + '.' + f.refTable) ?? tables.find((x) => x.ref.Name === f.refTable); if (!to) return null
    const a = pos[tk(t.ref)], b = pos[tk(to.ref)]; if (!a || !b) return null
    const right = a.x + BW / 2 <= b.x + BW / 2 && to !== t // leave by the side that faces the other table
    const s = anchor(t, f.columns[0], right ? 'r' : 'l'), e = anchor(to, f.refColumns[0], right ? 'l' : 'r'), dx = (right ? 1 : -1) * Math.max(40, Math.abs(e.x - s.x) / 2)
    return { id: tk(t.ref) + i, d: `M${s.x},${s.y} C${s.x + dx},${s.y} ${e.x - dx},${e.y} ${e.x},${e.y}`, stroke: edge(t, tint(t)), dim: !!q && !t.ref.Name.toLowerCase().includes(q) && !to.ref.Name.toLowerCase().includes(q) }
  })).filter((x) => x !== null)

  return (
    <div className="flex-1 min-h-0 relative bg-canvas" data-ctx="diagram">
      <div className="absolute right-3 bottom-3 z-[2] flex gap-1">
        <button onClick={() => zoomAt(1.25)} aria-label="Zoom in">+</button><button onClick={() => zoomAt(0.8)} aria-label="Zoom out">−</button>
        <button onClick={fit}>Fit</button>
        <button onClick={() => { setPos(autoLayout(tables)); setTimeout(fit, 0) }} title="Forget the arrangement and lay the tables out again">Reset layout</button>
      </div>
      <svg ref={svg} className="w-full h-full touch-none cursor-grab active:cursor-grabbing select-none font-mono" role="img" aria-label="Entity relationship diagram: drag the background to pan, the wheel to zoom, a table's header to move it"
        onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerCancel={up}
        onContextMenu={(e) => {
          const el = (e.target as Element).closest<SVGElement>('[data-tbl]'), t = el && byKey.get(el.dataset.tbl!); if (!t) return
          e.preventDefault(); setCtx({ x: e.clientX, y: e.clientY, t, col: (e.target as Element).closest<SVGElement>('[data-col]')?.dataset.col })
        }}>
        <defs><marker id="er-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto"><path d="M0,1 L10,5 L0,9 z" fill="var(--ink-2)" /></marker></defs>
        <g transform={`translate(${view.x} ${view.y}) scale(${view.k})`}>
          {edges.map((e) => <path key={e.id} d={e.d} fill="none" stroke={e.stroke} strokeWidth={1.5} markerEnd="url(#er-arrow)" opacity={e.dim ? 0.2 : 0.85} />)}
          {tables.map((t) => {
            const k = tk(t.ref), p = pos[k]; if (!p) return null
            const fkCols = new Set(t.fks.flatMap((f) => f.columns)), match = !q || t.ref.Name.toLowerCase().includes(q)
            return (
              <g key={k} data-tbl={k} transform={`translate(${p.x} ${p.y})`} opacity={match ? 1 : 0.3}>
                <rect width={BW} height={boxH(t)} rx={2} fill="var(--panel)" stroke={tint(t) ?? `hsl(${hue(t)} 40% 50% / .7)`} />
                <rect width={BW} height={HEAD} rx={2} fill={tint(t) ?? `hsl(${hue(t)} 45% 50% / .28)`} fillOpacity={tint(t) ? 0.3 : 1} data-head={k} className="cursor-move"><title>Drag to move</title></rect>
                <g data-open transform={`translate(${BW - 52} 5)`} className="cursor-pointer" role="button" aria-label={`Open the structure of ${t.ref.Name}`} onClick={() => onStructure(t.ref)}>
                  <rect width={46} height={18} rx={2} fill="var(--canvas)" stroke="var(--rule-2)" /><text x={23} y={12.5} fontSize={10.5} textAnchor="middle" fill="var(--ink-2)">Open</text>
                </g>
                <text x={10} y={18} fontSize={12.5} fontWeight={600} fill="var(--ink)" pointerEvents="none">{t.ref.Name.length > 20 ? t.ref.Name.slice(0, 19) + '…' : t.ref.Name}</text>
                {t.columns.map((c, i) => (
                  <g key={c.name} data-col={c.name} transform={`translate(0 ${HEAD + i * ROWH})`}>
                    <text x={10} y={14} fontSize={11.5} fill={c.pk ? 'var(--accent)' : 'var(--ink)'} pointerEvents="none">{c.pk ? '# ' : fkCols.has(c.name) ? '→ ' : '  '}{c.name.length > 16 ? c.name.slice(0, 15) + '…' : c.name}</text>
                    <text x={BW - 22} y={14} fontSize={10.5} fill="var(--ink-3)" textAnchor="end" pointerEvents="none">{c.type.length > 12 ? c.type.slice(0, 11) + '…' : c.type}</text>
                    {canLink && <circle cx={BW - 10} cy={10} r={4.5} data-link={c.name} data-t={k} className="cursor-crosshair" fill="var(--canvas)" stroke={edge(t, tint(t))}><title>Drag onto another table to add a foreign key from {c.name}</title></circle>}
                  </g>
                ))}
              </g>
            )
          })}
          {ghost && drag.current?.kind === 'link' && (() => { const t = (drag.current as Extract<Drag, { kind: 'link' }>); const s = anchor(t.table, t.col, 'r'); return <line x1={s.x} y1={s.y} x2={ghost.x} y2={ghost.y} stroke="var(--accent)" strokeWidth={1.5} strokeDasharray="5 4" /> })()}
        </g>
      </svg>
      {ctx && <>
        <div className="fixed inset-0 z-40" onMouseDown={() => setCtx(null)} onContextMenu={(e) => { e.preventDefault(); setCtx(null) }} />
        <div className="popover fixed z-50 min-w-[190px] py-1 flex flex-col motion-safe:animate-pop" style={{ left: Math.min(ctx.x, window.innerWidth - 210), top: Math.min(ctx.y, window.innerHeight - 190) }} role="menu" aria-label={`Menu for ${ctx.t.ref.Name}`}
          onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); setCtx(null) } }}>
          {ctx.col && <button className="menu-item" role="menuitem" autoFocus onClick={() => { const c = ctx.col!; setCtx(null); copyText(c).then(() => onNote?.(`Copied ${c}`), (er) => onNote?.(String(er))) }}>Copy column name</button>}
          <button className="menu-item" role="menuitem" autoFocus={!ctx.col} onClick={() => { const r = ctx.t.ref; setCtx(null); onStructure(r) }}>Open structure</button>
          {onColor && <label className="menu-item flex items-center gap-2 cursor-pointer">Colour
            <input type="color" aria-label={`Colour of ${ctx.t.ref.Name}`} className="w-8 h-5 p-0 border border-rule-2 bg-transparent" value={tint(ctx.t) ?? '#7aa2f7'} onChange={(e) => onColor(tk(ctx.t.ref), e.target.value)} /></label>}
          {onColor && tint(ctx.t) && <button className="menu-item" role="menuitem" onClick={() => { onColor(tk(ctx.t.ref), ''); setCtx(null) }}>Reset colour</button>}
          {onDelete && <button className="menu-item btn-danger" role="menuitem" onClick={() => { const t = ctx.t; setCtx(null); onDelete(t) }}>Delete table…</button>}
        </div>
      </>}
    </div>
  )
}
