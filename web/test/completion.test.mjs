// Run: node --test test/completion.test.mjs  (ranking and alias rules mirror internal/ui/util.go fuzzy and RankCandidates)
import test from 'node:test'
import assert from 'node:assert/strict'
import { aliasesIn, fuzzy, rank, statementAt, tablesRead } from '../src/components/Completion.ts'

test('fuzzy is a case-insensitive subsequence match', () => {
  assert.equal(fuzzy('', 'x'), 0)
  assert.equal(fuzzy('zz', 'users'), null)
  assert.ok(fuzzy('usr', 'users') !== null)
})

test('a prefix match outranks a scattered one', () => {
  const out = rank('us', [{ text: 'status', kind: 'column' }, { text: 'users', kind: 'table' }, { text: 'xyz', kind: 'column' }])
  assert.deepEqual(out.map((c) => c.text), ['users', 'status'])
})

test('aliases and the tables a statement reads', () => {
  assert.deepEqual(aliasesIn('select * from public.customers c join orders as o on 1=1 where x'), { customers: 'customers', c: 'customers', orders: 'orders', o: 'orders' })
  assert.deepEqual(tablesRead('update a set x=1; insert into b select * from c'), ['a', 'b', 'c'])
  assert.equal(statementAt('select 1; select 2; select 3', 12).trim(), 'select 2')
})
