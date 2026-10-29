// Run: node --test tests/matchname.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { matchName } from '../src/utils/matchname.ts'

test('P2-40: shorter names rank higher', () => {
  assert.ok(matchName('usr', 'users', true)[0] > matchName('usr', 'user_sessions_archive', true)[0])
})
