// Result chart: bars, line and pie over the result columns.
import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, saveBlob } from '../../utils/api'
import { ask } from '../shell/Dialogs'
import { msg } from '../shell/ui'

export type Series = { kind: string; labels: string[]; values: number[]; series?: { name: string; values: number[] }[] }

export function ChartView({ stmt, columns, cells, notify }: { stmt: string; columns: string[]; cells: (string | null)[][]; notify?: (m: string) => void }) {
  const [kind, setKind] = useState('')
  const q = useQuery({ queryKey: ['chart', stmt, columns, cells, kind], queryFn: () => api<Series>('POST', '/chart', { Stmt: stmt, Kind: kind, Columns: columns, Cells: cells }), gcTime: 0 })
  const s = q.data
  const kinds = ['bar', 'line', 'pie']
  const addToBoard = async () => {
    const name = await ask('Metrics board name'); if (!name) return
    try { await api('POST', '/boards', { Name: name, Query: stmt, Kind: kind || s?.kind }); notify?.(`Added to board ${name}`) } catch (e) { notify?.(msg(e)) }
  }
  const box = useRef<HTMLDivElement>(null)
  // PNG: the SVG is drawn into a canvas with the CSS variables resolved (an image cannot see the page's :root); a pie is a list of bars, so it is drawn directly.
  const savePng = async () => {
    if (!s) return
    const cs = getComputedStyle(document.documentElement), c = document.createElement('canvas'), x = c.getContext('2d')!
    const css = (n: string) => cs.getPropertyValue(n).trim() || '#888'
    try {
      const svg = box.current?.querySelector('svg')
      if (svg) {
        const img = new Image(), url = URL.createObjectURL(new Blob([new XMLSerializer().serializeToString(svg).replace(/var\((--[\w-]+)\)/g, (_, n) => css(n))], { type: 'image/svg+xml' }))
        await new Promise((ok, no) => { img.onload = ok; img.onerror = no; img.src = url })
        c.width = W * 2; c.height = H * 2; x.fillStyle = css('--canvas'); x.fillRect(0, 0, c.width, c.height); x.drawImage(img, 0, 0, c.width, c.height)
        URL.revokeObjectURL(url)
      } else {
        const max = Math.max(...s.values, 1), total = s.values.reduce((a, b) => a + Math.max(b, 0), 0) || 1
        c.width = 1280; c.height = 40 + s.values.length * 36; x.fillStyle = css('--canvas'); x.fillRect(0, 0, c.width, c.height); x.font = '22px monospace'
        s.values.forEach((v, i) => { const y = 36 + i * 36; x.fillStyle = css('--ink-2'); x.fillText(cut(s.labels[i], 22), 20, y); x.fillStyle = css('--accent'); x.fillRect(340, y - 20, (Math.max(v, 0) / max) * 600, 22); x.fillStyle = css('--ink'); x.fillText(`${fmt(v)} (${((Math.max(v, 0) / total) * 100).toFixed(1)}%)`, 960, y) })
      }
      c.toBlob((b) => { if (b) saveBlob(b, 'chart.png'); else notify?.('Could not render the chart') })
    } catch (e) { notify?.(msg(e)) }
  }
  return (
    <div className="flex-1 min-h-0 overflow-auto p-3" ref={box}>
      <div className="flex gap-1.5 mb-2" role="group" aria-label="Chart type">{kinds.map((k) => <button key={k} className={(kind || s?.kind) === k ? 'btn-on' : ''} onClick={() => setKind(k)}>{k}</button>)}
        {s && s.values.length > 0 && <><button className="ml-auto" onClick={savePng}>Save as PNG</button><button onClick={addToBoard}>Add to Metrics Board</button></>}</div>
      {q.error && <div className="text-del text-[12.5px]">{q.error.message}</div>}
      {s && s.values.length === 0 && <div className="text-ink-2 text-[12.5px]">Nothing to draw: the result has no numeric column.</div>}
      {s && s.values.length > 0 && <Plot s={s} />}
    </div>
  )
}

export function Plot({ s }: { s: Series }) { return s.kind === 'pie' ? <Bars s={s} /> : s.kind === 'line' ? <Line s={s} /> : <Columns s={s} /> }

const fmt = (n: number) => (Number.isInteger(n) ? String(n) : n.toFixed(2))
const cut = (l: string, n = 12) => (l.length > n ? l.slice(0, n - 1) + '…' : l)
const W = 640, H = 260, PAD = 34
// several y columns: the first keeps the accent, the others step around the hue wheel
const hueOf = (i: number) => (i === 0 ? 'var(--accent)' : `hsl(${(i * 67 + 200) % 360} 55% 58%)`)
const many = (s: Series) => (s.series && s.series.length > 1 ? s.series : [{ name: '', values: s.values }])

function Legend({ list }: { list: { name: string }[] }) {
  if (list.length < 2) return null
  return <div className="flex flex-wrap gap-x-4 gap-y-1 mb-1.5 text-[12px] font-mono text-ink-2" aria-label="Legend">{list.map((x, i) => <span key={i} className="flex items-center gap-1.5"><span className="inline-block w-3 h-3 rounded-[1px]" style={{ background: hueOf(i) }} />{x.name}</span>)}</div>
}

function Columns({ s }: { s: Series }) {
  const list = many(s), n = s.labels.length || s.values.length
  const max = Math.max(...list.flatMap((x) => x.values), 0) || 1, gw = (W - PAD) / n, bw = Math.max((gw - 4) / list.length, 1)
  return (
    <div><Legend list={list} />
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full max-w-[760px]" role="img" aria-label="Bar chart">
      {s.labels.map((label, i) => (
        <g key={i}>
          {list.map((ser, k) => {
            const v = ser.values[i] ?? 0, h = (Math.max(v, 0) / max) * (H - PAD - 14), x = PAD + i * gw + 2 + k * bw
            return <g key={k}><title>{`${label}${ser.name ? ' / ' + ser.name : ''}: ${fmt(v)}`}</title>
              <rect x={x} y={H - PAD - h} width={bw} height={h} fill={hueOf(k)} />
              {list.length === 1 && <text x={x + bw / 2} y={H - PAD - h - 3} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{fmt(v)}</text>}</g>
          })}
          <text x={PAD + i * gw + gw / 2} y={H - 14} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{cut(label, Math.max(3, Math.floor(gw / 6)))}</text>
        </g>
      ))}
      <line x1={PAD} x2={W} y1={H - PAD} y2={H - PAD} stroke="var(--rule-2)" />
    </svg></div>
  )
}

function Line({ s }: { s: Series }) {
  const list = many(s), all = list.flatMap((x) => x.values), n = s.labels.length || s.values.length
  const max = Math.max(...all, 0) || 1, min = Math.min(...all, 0), span = max - min || 1
  const step = n > 1 ? (W - PAD - 10) / (n - 1) : 0
  const pt = (v: number, i: number) => [PAD + i * step, H - PAD - ((v - min) / span) * (H - PAD - 14)]
  return (
    <div><Legend list={list} />
    <svg viewBox={`0 0 ${W} ${H}`} className="w-full max-w-[760px]" role="img" aria-label="Line chart">
      <line x1={PAD} x2={W} y1={H - PAD} y2={H - PAD} stroke="var(--rule-2)" />
      {list.map((ser, k) => <g key={k}>
        <polyline fill="none" stroke={hueOf(k)} strokeWidth="2" points={ser.values.map((v, i) => pt(v, i).join(',')).join(' ')} />
        {ser.values.map((v, i) => { const [x, y] = pt(v, i); return <g key={i}><title>{`${s.labels[i]}${ser.name ? ' / ' + ser.name : ''}: ${fmt(v)}`}</title><circle cx={x} cy={y} r="3" fill={hueOf(k)} /></g> })}</g>)}
      {s.labels.map((l, i) => (n <= 12 || i % Math.ceil(n / 12) === 0) && <text key={i} x={pt(0, i)[0]} y={H - 14} textAnchor="middle" fontSize="9" fill="var(--ink-2)">{cut(l, 8)}</text>)}
      <text x="2" y="12" fontSize="9" fill="var(--ink-2)">{fmt(max)}</text><text x="2" y={H - PAD} fontSize="9" fill="var(--ink-2)">{fmt(min)}</text>
    </svg></div>
  )
}

// a pie is a sorted bar list: the server sorts it, the share is the label
function Bars({ s }: { s: Series }) {
  const total = s.values.reduce((a, b) => a + Math.max(b, 0), 0) || 1, max = Math.max(...s.values) || 1
  return (
    <div className="font-mono text-[12px] max-w-[760px]">
      {s.values.map((v, i) => (
        <div key={i} className="flex items-center gap-2 py-0.5">
          <span className="w-40 truncate text-ink-2" title={s.labels[i]}>{s.labels[i]}</span>
          <span className="flex-1 h-3 bg-raised"><span className="block h-full bg-accent" style={{ width: `${(Math.max(v, 0) / max) * 100}%` }} /></span>
          <span className="w-28 text-right">{fmt(v)} ({((Math.max(v, 0) / total) * 100).toFixed(1)}%)</span>
        </div>
      ))}
    </div>
  )
}
