// Run: node --test test/pending.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { emptyPend, pendCount, setEdit, editedMap, toggleDelete, dupVals, planBody } from '../src/utils/pending.ts'

const cols = [{ name: 'id', pk: true }, { name: 'name' }]
const cells = [['1', 'a'], ['2', null]]
const show = (v, s) => (v === undefined ? s : v === null ? null : typeof v === 'string' ? v : v.$ === 'null' ? null : '')

test('edit, delete, insert count and body', () => {
  let p = setEdit(emptyPend(), 'r1', 'name', 'x')
  p = toggleDelete(p, 'r0')
  p = { ...p, inserts: [{ id: 1, vals: { name: 'n' } }] }
  p = setEdit(p, 'n1', 'name', 'm')
  assert.equal(pendCount(p), 3)
  assert.equal(editedMap(p).n1.name, 'm')
  const b = planBody('rid', cols, cells, p)
  assert.deepEqual(b.updates, [{ row: 1, set: { name: 'x' }, key: { id: '2' } }])
  assert.deepEqual(b.deletes, [0]); assert.deepEqual(b.deleteKeys, [{ id: '1' }])
  assert.deepEqual(b.inserts, [{ name: 'm' }]); assert.equal(b.resultId, 'rid')
})
test('a deleted row drops its edits; deleting twice restores; new rows are removed', () => {
  let p = toggleDelete(setEdit(emptyPend(), 'r0', 'name', 'x'), 'r0')
  assert.equal(planBody('r', cols, cells, p).updates.length, 0)
  assert.equal(toggleDelete(p, 'r0').deletes.size, 0)
  p = { ...emptyPend(), inserts: [{ id: 2, vals: {} }] }
  assert.equal(toggleDelete(p, 'n2').inserts.length, 0)
})
test('dupVals skips the key and keeps NULL', () => {
  assert.deepEqual(dupVals(cols, cells[1], undefined, show), { name: { $: 'null' } })
  assert.deepEqual(dupVals(cols, cells[0], { name: 'z' }, show), { name: 'z' })
})
test('setEdit does not mutate', () => {
  const p = emptyPend(); setEdit(p, 'r0', 'name', 'x'); assert.equal(pendCount(p), 0)
})
