// Run: node --test tests/workspaces.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'

const store = () => { const m = new Map(); return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) } }
globalThis.sessionStorage = store()
globalThis.localStorage = store()
const { openSessions, loadTabs, lastWorkspaces, remember } = await import('../src/utils/workspaces.ts')

test('P2-29: valid JSON of the wrong shape reads like nothing stored', () => {
  for (const bad of ['5', '[]', '"x"', 'true']) {
    sessionStorage.setItem('webtuidb-open', bad); localStorage.setItem('webtuidb-last', bad)
    assert.deepEqual(openSessions(), {}); assert.deepEqual(lastWorkspaces(), {})
    assert.doesNotThrow(() => remember('a', 's1'))
    sessionStorage.removeItem('webtuidb-open')
  }
  for (const bad of ['{}', '[]', '5', '{"tabs":3,"active":[0,0]}', '{"tabs":[],"active":"x"}']) {
    sessionStorage.setItem('webtuidb-tabs-s1', bad)
    assert.equal(loadTabs('s1'), null, bad)
  }
  sessionStorage.setItem('webtuidb-tabs-s1', '{"tabs":[{"kind":"query"},null],"active":[1,0],"split":false}')
  assert.deepEqual(loadTabs('s1'), { tabs: [{ kind: 'query' }], active: [1, 0], split: false })
})
