// Run: node --test test/results.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { normItem, stmtAt } from '../src/utils/results.ts'

test('W-P1-01: a failed statement gets empty cells and columns', () => {
  const x = normItem({ stmt: 'select x from nope', error: 'ERROR', cells: null, columns: null })
  assert.deepEqual(x.cells, []); assert.deepEqual(x.columns, [])
  assert.equal(x.error, 'ERROR')
  const ok = normItem({ cells: [['1']], columns: [{ name: 'a' }] })
  assert.deepEqual(ok.cells, [['1']])
})

test('W-P2-18: the statement around the cursor, like db.StatementAt', () => {
  const r = [[0, 9], [10, 18]]
  assert.deepEqual(stmtAt(r, 12), [10, 18])
  assert.deepEqual(stmtAt(r, 9), [0, 9])
  assert.deepEqual(stmtAt(r, 30), [10, 18])
  assert.equal(stmtAt([], 3), null)
})
