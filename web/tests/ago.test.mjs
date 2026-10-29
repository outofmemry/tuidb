// Run: node --test tests/ago.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { ago, recentRank } from '../src/utils/ago.ts'
test('ago and the recent sort', () => {
  const now = Date.parse('2026-10-28T12:00:00Z')
  assert.equal(ago('2026-10-28T11:59:40Z', now), 'just now')
  assert.equal(ago('2026-10-28T11:00:00Z', now), '1 hour ago')
  assert.equal(ago('2026-10-26T12:00:00Z', now), '2 days ago')
  assert.equal(ago('', now), '')
  assert.ok(recentRank('2026-10-28T11:00:00Z') > recentRank('2026-10-01T00:00:00Z'))
  assert.equal(recentRank(''), 0)
})
