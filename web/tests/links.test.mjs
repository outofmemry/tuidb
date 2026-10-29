// Run: node --test tests/m15_links.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { linkWithToken, tableLink, queryLink, parseFilter, forThisTab, handoffPut, handoffTake, HANDOFF_KEY } from '../src/utils/links.ts'

test('withToken adds the fragment and keeps an existing one', () => {
  assert.equal(linkWithToken('/s/a', 'T'), '/s/a#token=T')
  assert.equal(linkWithToken('/s/a?x=1', 'T'), '/s/a?x=1#token=T')
  assert.equal(linkWithToken('/s/a#token=T', 'T'), '/s/a#token=T')
  assert.equal(linkWithToken('/s/a#t=1', 'T'), '/s/a#t=1&token=T')
  assert.equal(linkWithToken('/s/a', ''), '/s/a')
})

test('deep links', () => {
  assert.equal(tableLink('s1', 'public', 'users'), '/s/s1/t/public/users')
  assert.equal(tableLink('s1', '', 'my table'), '/s/s1/t/-/my%20table')
  const f = { Filters: [{ Column: 'id', Op: '=', Value: '1', Enabled: true }], Or: false }
  assert.deepEqual(parseFilter(new URL('http://x' + tableLink('s1', 'p', 't', f)).searchParams.get('filter')), f)
  assert.equal(queryLink('s1', 'reports/daily sales.sql'), '/s/s1/q/reports/daily%20sales.sql')
})

test('parseFilter ignores junk', () => {
  assert.equal(parseFilter('not json'), undefined)
  assert.equal(parseFilter('{"Filters":[]}'), undefined)
  assert.equal(parseFilter('{"Filters":[{"Column":1}]}'), undefined)
  assert.deepEqual(parseFilter({ Filters: [{ Column: 'a', Op: '=', Value: 5 }], Or: true }), { Filters: [{ Column: 'a', Op: '=', Value: '5', Enabled: true }], Or: true })
})

test('P2-36: the filter-column event is answered only by its own tab', () => {
  assert.equal(forThisTab({ sid: 's1', key: 'public.a' }, 's1', 'public.a'), true)
  assert.equal(forThisTab({ sid: 's1', key: 'public.a' }, 's1', 'public.b'), false)
  assert.equal(forThisTab({ sid: 's2', key: 'public.a' }, 's1', 'public.a'), false)
  assert.equal(forThisTab(undefined, 's1', 'public.a'), false)
})

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
