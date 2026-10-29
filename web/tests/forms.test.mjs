// Run: node --test tests/forms.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { enterSubmits, limitHint, previewLength } from '../src/utils/forms.ts'

test('previewLength cuts a CSV slice after its last newline and keeps a single long row', () => {
  const b = new TextEncoder().encode('a,b\n1,2\n3,')
  assert.equal(previewLength(b), 8)
  assert.equal(new TextDecoder().decode(b.slice(0, previewLength(b))), 'a,b\n1,2\n')
  const one = new TextEncoder().encode('a,b,c,d')
  assert.equal(previewLength(one), one.length)
  assert.equal(previewLength(new TextEncoder().encode('x\n')), 2)
})

test('enterSubmits only fires for Enter in a text input', () => {
  let n = 0
  const ev = (key, tagName, type) => ({ key, target: { tagName, type }, preventDefault() {} })
  assert.ok(enterSubmits(ev('Enter', 'INPUT', 'text'), () => n++))
  assert.ok(!enterSubmits(ev('Enter', 'INPUT', 'checkbox'), () => n++))
  assert.ok(!enterSubmits(ev('Enter', 'TEXTAREA', ''), () => n++))
  assert.ok(!enterSubmits(ev('a', 'INPUT', 'text'), () => n++))
  assert.equal(n, 1)
})

test('P2-05/69: per-key limit hints', () => {
  assert.equal(limitHint('MAX_USER_CONNECTIONS', false), '0 = the server default')
  assert.equal(limitHint('MAX_QUERIES_PER_HOUR', false), '0 = unlimited')
  assert.equal(limitHint('CONNECTION_LIMIT', true), '-1 = no limit')
})
