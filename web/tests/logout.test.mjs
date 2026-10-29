// Run: node --test tests/logout.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { logoutSteps } from '../src/utils/logout.ts'

const run = async (call, sessions = ['s1', 's2']) => {
  const log = []
  await logoutSteps({ sessions, call: async (m, p) => { log.push(`${m} ${p}`); return call(m, p) }, forget: () => log.push('forget'), go: () => log.push('go') })
  return log
}

test('logout closes the sessions, expires the login cookie, forgets the token, then goes to the start', async () => {
  const log = await run(async () => ({}))
  assert.deepEqual(log, ['DELETE /sessions/s1', 'DELETE /sessions/s2', 'POST /logout', 'forget', 'go'])
})

test('logout still forgets the token when the server cannot be reached', async () => {
  const log = await run(async () => { throw new Error('offline') })
  assert.equal(log.at(-2), 'forget'); assert.equal(log.at(-1), 'go')
})
