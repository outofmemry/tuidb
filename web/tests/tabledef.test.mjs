// Run: node --test tests/tabledef.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { draftOf, newDef } from '../src/utils/tabledef.ts'

test('P1-09: expression index entries and untouched rows do not block a structure edit', () => {
  const cols = [{ Name: 'email', Type: 'text', Nullable: false, Default: '', HasDef: false, PK: false, Extra: '' }]
  const base = draftOf('users', cols, [{ Name: 'users_email_lower', Columns: ['lower(email)'], Unique: true }], [])
  const d = structuredClone(base)
  assert.equal(newDef('public', d, base).Indexes[0].Columns[0], 'lower(email)')
  assert.equal(newDef('public', d).Indexes[0].Columns[0], 'lower(email)') // an expression, even without the base
  d.idx.push({ orig: '', name: 'bad', cols: 'nope', unique: false, algorithm: '', condition: '' })
  assert.throws(() => newDef('public', d, base), /names a column that is not in the table/)
})
