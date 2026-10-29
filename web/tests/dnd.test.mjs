// Run: node --test tests/dnd.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { moveItem } from '../src/utils/dnd.ts'

test('moveItem moves an element to an index', () => {
  assert.deepEqual(moveItem(['a', 'b', 'c'], 0, 2), ['b', 'c', 'a'])
  assert.deepEqual(moveItem(['a', 'b', 'c'], 2, 0), ['c', 'a', 'b'])
})
