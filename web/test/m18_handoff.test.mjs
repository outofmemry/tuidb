// Run: node --test test/m18_handoff.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { handoffPut, handoffTake, HANDOFF_KEY } from '../src/utils/links.ts'

const store = () => { const m = new Map(); return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, v), removeItem: (k) => m.delete(k), m } }

test('WU-5: the token is handed over once, through storage, and expires', () => {
  const s = store()
  handoffPut(s, 'T', 1000)
  assert.ok(s.m.has(HANDOFF_KEY))
  assert.equal(handoffTake(s, 2000), 'T')
  assert.equal(s.m.has(HANDOFF_KEY), false, 'taken once')
  assert.equal(handoffTake(s, 3000), '')
  handoffPut(s, 'T', 1000)
  assert.equal(handoffTake(s, 1000 + 20_000), '', 'stale entry ignored')
  const bad = store(); bad.setItem(HANDOFF_KEY, '{nope')
  assert.equal(handoffTake(bad), '')
})
