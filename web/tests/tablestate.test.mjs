// Run: node --test tests/tablestate.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { tableStateBody } from '../src/utils/tablestate.ts'

test('P2-34: the default sort is not saved unless the user chose a sort', () => {
  const base = { hidden: [], filters: {}, showBar: false, sort: [{ Column: 'id', Desc: false }], widths: { a: 3 }, sortChosen: false, widthsChosen: false }
  assert.deepEqual(Object.keys(tableStateBody(base)).sort(), ['filters', 'hidden', 'show_filters'])
  const b = tableStateBody({ ...base, sortChosen: true, widthsChosen: true })
  assert.deepEqual(b.sort, base.sort); assert.deepEqual(b.widths, base.widths)
})
