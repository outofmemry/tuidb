// Run: node --test tests/processes.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { sortProcs } from '../src/utils/processes.ts'

test('sortProcs sorts numbers numerically and flips', () => {
  const l = [{ id: '10', user: 'b', elapsedMs: 5 }, { id: '9', user: 'a', elapsedMs: 50 }]
  assert.deepEqual(sortProcs(l, 'id', false).map((p) => p.id), ['9', '10'])
  assert.deepEqual(sortProcs(l, 'elapsed', true).map((p) => p.id), ['9', '10'])
  assert.deepEqual(sortProcs(l, 'user', true).map((p) => p.user), ['b', 'a'])
})
