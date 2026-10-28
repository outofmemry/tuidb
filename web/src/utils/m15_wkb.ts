// (E)WKB -> geometry -> WKT, for grid cells and the quick look map (M15.35). Point, LineString, Polygon, Multi*, GeometryCollection,
// both byte orders, the EWKB SRID flag and Z / M, plus the ISO 1000/2000/3000 type codes.
// shortcut: no curve / surface types (CircularString, TIN...), those cells stay hex, add them to the switch in read().
export type Geom =
  | { type: 'Point'; c: number[] }
  | { type: 'LineString'; c: number[][] }
  | { type: 'Polygon'; c: number[][][] }
  | { type: 'MultiPoint' | 'MultiLineString' | 'MultiPolygon' | 'GeometryCollection'; parts: Geom[] }
export type Parsed = { geom: Geom; srid?: number; dims: number } // dims: 2, 3 (Z or M) or 4

const NAMES = ['', 'Point', 'LineString', 'Polygon', 'MultiPoint', 'MultiLineString', 'MultiPolygon', 'GeometryCollection'] as const

class Reader {
  o = 0
  b: Uint8Array
  v: DataView
  constructor(b: Uint8Array) { this.b = b; this.v = new DataView(b.buffer, b.byteOffset, b.byteLength) }
  need(n: number) { if (this.o + n > this.b.length) throw new Error('short') }
  u8() { this.need(1); return this.v.getUint8(this.o++) }
  u32(le: boolean) { this.need(4); const x = this.v.getUint32(this.o, le); this.o += 4; return x }
  f64(le: boolean) { this.need(8); const x = this.v.getFloat64(this.o, le); this.o += 8; return x }
}

function read(r: Reader, dimsOut: { n: number; srid?: number }, top: boolean): Geom {
  const le = r.u8() === 1
  let t = r.u32(le), dims = 2
  if (t & 0x80000000) dims++
  if (t & 0x40000000) dims++
  if (t & 0x20000000) { const s = r.u32(le); if (top) dimsOut.srid = s }
  t &= 0x0fffffff
  if (t >= 1000 && t < 4000) { dims = t >= 3000 ? 4 : 3; t %= 1000 } // ISO: 1000 Z, 2000 M, 3000 ZM
  if (top) dimsOut.n = dims
  const name = NAMES[t]
  if (!name) throw new Error('type')
  const pt = () => Array.from({ length: dims }, () => r.f64(le))
  const count = () => { const n = r.u32(le); if (n > 5_000_000) throw new Error('count'); return n }
  const pts = () => Array.from({ length: count() }, pt)
  switch (name) {
    case 'Point': return { type: name, c: pt() }
    case 'LineString': return { type: name, c: pts() }
    case 'Polygon': return { type: name, c: Array.from({ length: count() }, pts) }
    default: return { type: name, parts: Array.from({ length: count() }, () => read(r, dimsOut, false)) }
  }
}

export function parseWkb(b: Uint8Array): Parsed | null {
  if (b.length < 9 || (b[0] !== 0 && b[0] !== 1)) return null
  try {
    const o: { n: number; srid?: number } = { n: 2 }
    const r = new Reader(b)
    const geom = read(r, o, true)
    if (r.o !== b.length) return null // trailing bytes: not (only) a geometry
    return { geom, srid: o.srid, dims: o.n }
  } catch { return null }
}

const num = (x: number) => String(x)
const pt = (c: number[]) => c.map(num).join(' ')
function body(g: Geom): string {
  switch (g.type) {
    case 'Point': return `(${pt(g.c)})`
    case 'LineString': return `(${g.c.map(pt).join(', ')})`
    case 'Polygon': return `(${g.c.map((ring) => `(${ring.map(pt).join(', ')})`).join(', ')})`
    case 'MultiPoint': return `(${g.parts.map((p) => (p.type === 'Point' ? `(${pt(p.c)})` : '')).join(', ')})`
    case 'MultiLineString': case 'MultiPolygon': return `(${g.parts.map(body).join(', ')})`
    case 'GeometryCollection': return `(${g.parts.map((p) => wktOf(p, 0)).join(', ')})`
  }
}
const tag = (g: Geom, dims: number) => g.type.toUpperCase() + (dims === 3 ? ' Z' : dims === 4 ? ' ZM' : '')
function wktOf(g: Geom, dims: number): string {
  const empty = g.type === 'Point' ? g.c.every((x) => Number.isNaN(x)) : g.type === 'LineString' || g.type === 'Polygon' ? g.c.length === 0 : g.parts.length === 0
  return empty ? `${tag(g, dims)} EMPTY` : `${tag(g, dims)} ${body(g)}`
}
// wkt renders the parsed geometry; an EWKB SRID becomes the usual "SRID=4326;" prefix
export const wkt = (p: Parsed) => (p.srid ? `SRID=${p.srid};` : '') + wktOf(p.geom, p.dims)

export const hexBytes = (h: string): Uint8Array | null => (h.length % 2 === 0 && /^[0-9a-f]*$/i.test(h) ? Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16)) : null)

// geomOf reads a grid cell: "\x..." binary (MySQL / SQL Server: after a 4 byte SRID prefix in MySQL's case) or the plain hex EWKB PostGIS prints.
const MAYBE = /^(\\x)?[0-9a-f]{18,}$/i
export function geomOf(v: string | null): Parsed | null {
  if (v === null || v.length < 18 || !MAYBE.test(v)) return null
  const h = v.startsWith('\\x') ? v.slice(2) : v
  const head = h.slice(0, 2), head4 = h.slice(8, 10)
  const tryAt = (hex: string) => { const b = hexBytes(hex); return b && parseWkb(b) }
  if (head === '00' || head === '01') { const p = tryAt(h); if (p) return p }
  if (head4 === '00' || head4 === '01') { // MySQL stores 4 bytes SRID (little endian) in front of the WKB
    const p = tryAt(h.slice(8)); if (p) { const b = hexBytes(h.slice(0, 8))!; return { ...p, srid: p.srid ?? new DataView(b.buffer).getUint32(0, true) } }
  }
  return null
}
export const cellWkt = (v: string | null): string | null => { const p = geomOf(v); return p ? wkt(p) : null }

// ---- map: the geometry as SVG paths in a fitted box ----------------------------------------------------------
export type Drawing = { viewBox: string; points: [number, number][]; lines: string[]; polys: string[]; r: number }
export function draw(g: Geom): Drawing | null {
  const pts: [number, number][] = [], lines: number[][][] = [], polys: number[][][][] = []
  const walk = (x: Geom) => {
    switch (x.type) {
      case 'Point': if (!x.c.some(Number.isNaN)) pts.push([x.c[0], x.c[1]]); break
      case 'LineString': if (x.c.length) lines.push(x.c); break
      case 'Polygon': if (x.c.length) polys.push(x.c); break
      default: x.parts.forEach(walk)
    }
  }
  walk(g)
  const all = [...pts, ...lines.flat().map((c) => [c[0], c[1]] as [number, number]), ...polys.flat(2).map((c) => [c[0], c[1]] as [number, number])]
  if (!all.length) return null
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity
  for (const [x, y] of all) { x0 = Math.min(x0, x); x1 = Math.max(x1, x); y0 = Math.min(y0, y); y1 = Math.max(y1, y) }
  const w = x1 - x0, h = y1 - y0, span = Math.max(w, h) || 1, pad = span * 0.08
  // y grows downwards in SVG, so latitude is flipped
  const fy = (y: number) => y1 + y0 - y
  const path = (c: number[][], close: boolean) => c.map((p, i) => `${i ? 'L' : 'M'}${p[0]} ${fy(p[1])}`).join('') + (close ? 'Z' : '')
  return {
    viewBox: `${x0 - pad} ${y0 - pad} ${w + 2 * pad || 1} ${h + 2 * pad || 1}`,
    points: pts.map(([x, y]) => [x, fy(y)]),
    lines: lines.map((c) => path(c, false)),
    polys: polys.map((rings) => rings.map((r) => path(r, true)).join('')),
    r: span * 0.015,
  }
}
