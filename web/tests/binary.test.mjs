// Run: node --test tests/binary.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { sniff, base64Of, bytesOfBase64 } from '../src/utils/binary.ts'

test('sniff and base64', () => {
  assert.equal(sniff(Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0])).mime, 'image/png')
  assert.equal(sniff(Uint8Array.from([0xff, 0xd8, 0xff, 0xe0])).mime, 'image/jpeg')
  assert.equal(sniff(new TextEncoder().encode('RIFF\0\0\0\0WEBPVP8 ')).mime, 'image/webp')
  assert.equal(sniff(new TextEncoder().encode('%PDF-1.7')).kind, 'pdf')
  assert.equal(sniff(new TextEncoder().encode('plain')), null)
  assert.deepEqual(bytesOfBase64(base64Of(Uint8Array.from([1, 2, 255]))), Uint8Array.from([1, 2, 255]))
})
