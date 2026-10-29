// Run: node --test tests/folds.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { fnv1a32, foldedIndexes } from '../src/utils/folds.ts'

test('folds follow the statement text', () => {
  assert.equal(fnv1a32(''), '811c9dc5')
  assert.equal(fnv1a32('a'), 'e40c292c')
  const before = ['select 1;', 'select 2;']
  const saved = [before[1]].map((t) => fnv1a32(t.trim()))
  assert.deepEqual(foldedIndexes(saved, ['select 0;', ...before]), [2]) // a statement added above: the fold stays on "select 2;"
  assert.deepEqual(foldedIndexes(saved, ['select 1;', 'select 22;']), []) // the folded statement was edited: it unfolds
})
