// Run: node --test test/m15_links.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { linkWithToken, tableLink, queryLink, parseFilter } from '../src/utils/links.ts'

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
