// Run: node --test tests/boards.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { panelVals } from '../src/utils/boards.ts'

test('panelVals: panel default, then the variable default, then the first option', () => {
  const v = panelVals([{ Name: 'a', Default: '1' }, { Name: 'b', Default: '' }], [
    { Kind: 'input', var: 'a', default: '' }, { Kind: 'dropdown', var: 'b', options: ['x', 'y'] }, { Kind: 'input', var: 'c', default: '9' }, { Kind: 'number' },
  ])
  assert.deepEqual(v, { a: '1', b: 'x', c: '9' })
})
