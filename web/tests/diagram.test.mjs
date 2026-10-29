// Run: node --test tests/diagram.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { withForeignKey, validColour } from '../src/utils/diagram.ts'

const def = { Schema: 's', Name: 'orders', Columns: [{ Name: 'id' }, { Name: 'user_id' }], PrimaryKey: ['id'], Indexes: [], ForeignKeys: [] }
const fk = { Name: 'fk', Columns: ['user_id'], RefSchema: 's', RefTable: 'users', RefColumns: ['id'], OnUpdate: 'NO ACTION', OnDelete: 'CASCADE' }

test('withForeignKey adds the key to a copy and checks the columns', () => {
  const n = withForeignKey(def, fk)
  assert.equal(n.ForeignKeys.length, 1)
  assert.equal(def.ForeignKeys.length, 0)
  assert.throws(() => withForeignKey(def, { ...fk, Columns: ['nope'] }), /no column nope/)
  assert.throws(() => withForeignKey(def, { ...fk, RefColumns: ['a', 'b'] }), /cannot reference/)
})

test('validColour only accepts a hex colour', () => {
  assert.equal(validColour('#aabbcc'), '#aabbcc')
  assert.equal(validColour('red'), undefined)
})
