// Run: node --test test/m15_wkb.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { parseWkb, wkt, cellWkt, hexBytes, draw } from '../src/utils/m15_wkb.ts'
import { sniff, base64Of, bytesOfBase64 } from '../src/utils/m15_bin.ts'

test('little endian point', () => {
  const p = parseWkb(hexBytes('0101000000000000000000F03F0000000000000040'))
  assert.equal(wkt(p), 'POINT (1 2)')
})

test('big endian polygon with a hole', () => {
  const ring = (pts) => '00000005'.slice(0, 0) + pts.length.toString(16).padStart(8, '0') + pts.map(([x, y]) => f64(x) + f64(y)).join('')
  const f64 = (n) => { const b = Buffer.alloc(8); b.writeDoubleBE(n); return b.toString('hex') }
  const hex = '0000000003' + '00000002' + ring([[0, 0], [4, 0], [4, 4], [0, 0]]) + ring([[1, 1], [2, 1], [2, 2], [1, 1]])
  assert.equal(wkt(parseWkb(hexBytes(hex))), 'POLYGON ((0 0, 4 0, 4 4, 0 0), (1 1, 2 1, 2 2, 1 1))')
})

test('EWKB with SRID is a prefix on the WKT; PostGIS hex text decodes in a cell', () => {
  const hex = '0101000020E6100000000000000000F03F0000000000000040'
  assert.equal(cellWkt(hex), 'SRID=4326;POINT (1 2)')
  assert.equal(cellWkt('\\x' + hex), 'SRID=4326;POINT (1 2)')
})

test('MySQL 4 byte SRID prefix, Z flag, multipoint', () => {
  assert.equal(cellWkt('\\x00000000' + '0101000000000000000000F03F0000000000000040'), 'POINT (1 2)')
  const z = Buffer.alloc(1 + 4 + 24); z[0] = 1; z.writeUInt32LE(0x80000001, 1); z.writeDoubleLE(1, 5); z.writeDoubleLE(2, 13); z.writeDoubleLE(3, 21)
  assert.equal(wkt(parseWkb(new Uint8Array(z))), 'POINT Z (1 2 3)')
  const mp = '0104000000' + '02000000' + '0101000000000000000000F03F0000000000000040' + '010100000000000000000008400000000000001040'
  assert.equal(wkt(parseWkb(hexBytes(mp))), 'MULTIPOINT ((1 2), (3 4))')
})

test('not geometry', () => {
  assert.equal(cellWkt('hello world, not hex'), null)
  assert.equal(cellWkt('\\x89504e470d0a1a0a0000000d49484452'), null)
  assert.equal(parseWkb(hexBytes('0101000000')), null) // truncated
})

test('draw fits a viewBox', () => {
  const d = draw(parseWkb(hexBytes('010200000002000000000000000000000000000000000000000000000000000040000000000000F03F')).geom)
  assert.equal(d.lines.length, 1)
  assert.ok(d.viewBox.split(' ').length === 4)
})

test('sniff and base64', () => {
  assert.equal(sniff(Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0])).mime, 'image/png')
  assert.equal(sniff(Uint8Array.from([0xff, 0xd8, 0xff, 0xe0])).mime, 'image/jpeg')
  assert.equal(sniff(new TextEncoder().encode('RIFF\0\0\0\0WEBPVP8 ')).mime, 'image/webp')
  assert.equal(sniff(new TextEncoder().encode('%PDF-1.7')).kind, 'pdf')
  assert.equal(sniff(new TextEncoder().encode('plain')), null)
  assert.deepEqual(bytesOfBase64(base64Of(Uint8Array.from([1, 2, 255]))), Uint8Array.from([1, 2, 255]))
})
