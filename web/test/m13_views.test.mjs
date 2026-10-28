// Run: node --test test/m13_views.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { withForeignKey, panelVals, sortProcs, moveItem, validColour, chatTitle, extraArgs, commonBackupOptions, enterSubmits } from '../src/utils/m13.ts'

const def = { Schema: 's', Name: 'orders', Columns: [{ Name: 'id' }, { Name: 'user_id' }], PrimaryKey: ['id'], Indexes: [], ForeignKeys: [] }
const fk = { Name: 'fk', Columns: ['user_id'], RefSchema: 's', RefTable: 'users', RefColumns: ['id'], OnUpdate: 'NO ACTION', OnDelete: 'CASCADE' }

test('withForeignKey adds the key to a copy and checks the columns', () => {
  const n = withForeignKey(def, fk)
  assert.equal(n.ForeignKeys.length, 1)
  assert.equal(def.ForeignKeys.length, 0)
  assert.throws(() => withForeignKey(def, { ...fk, Columns: ['nope'] }), /no column nope/)
  assert.throws(() => withForeignKey(def, { ...fk, RefColumns: ['a', 'b'] }), /cannot reference/)
})

test('panelVals: panel default, then the variable default, then the first option', () => {
  const v = panelVals([{ Name: 'a', Default: '1' }, { Name: 'b', Default: '' }], [
    { Kind: 'input', var: 'a', default: '' }, { Kind: 'dropdown', var: 'b', options: ['x', 'y'] }, { Kind: 'input', var: 'c', default: '9' }, { Kind: 'number' },
  ])
  assert.deepEqual(v, { a: '1', b: 'x', c: '9' })
})

test('sortProcs sorts numbers numerically and flips', () => {
  const l = [{ id: '10', user: 'b', elapsedMs: 5 }, { id: '9', user: 'a', elapsedMs: 50 }]
  assert.deepEqual(sortProcs(l, 'id', false).map((p) => p.id), ['9', '10'])
  assert.deepEqual(sortProcs(l, 'elapsed', true).map((p) => p.id), ['9', '10'])
  assert.deepEqual(sortProcs(l, 'user', true).map((p) => p.user), ['b', 'a'])
})

test('moveItem, validColour, chatTitle', () => {
  assert.deepEqual(moveItem(['a', 'b', 'c'], 0, 2), ['b', 'c', 'a'])
  assert.deepEqual(moveItem(['a', 'b', 'c'], 2, 0), ['c', 'a', 'b'])
  assert.equal(validColour('#aabbcc'), '#aabbcc')
  assert.equal(validColour('red'), undefined)
  assert.equal(chatTitle([{ role: 'assistant', content: 'x' }, { role: 'user', content: '  hello   world ' }]), 'hello world')
  assert.equal(Array.from(chatTitle([{ role: 'user', content: 'é'.repeat(100) }])).length, 60)
})

test('extraArgs keeps ticked flags first, splits text, groups quotes, drops repeats', () => {
  assert.deepEqual(extraArgs(['--clean'], '--no-comments "--x=a b" --clean'), ['--clean', '--no-comments', '--x=a b'])
  assert.deepEqual(extraArgs([], '  '), [])
  assert.ok(commonBackupOptions('postgres', false).some((o) => o.Flag === '--no-owner'))
  assert.deepEqual(commonBackupOptions('redis', true), [])
})

test('enterSubmits only fires for Enter in a text input', () => {
  let n = 0
  const ev = (key, tagName, type) => ({ key, target: { tagName, type }, preventDefault() {} })
  assert.ok(enterSubmits(ev('Enter', 'INPUT', 'text'), () => n++))
  assert.ok(!enterSubmits(ev('Enter', 'INPUT', 'checkbox'), () => n++))
  assert.ok(!enterSubmits(ev('Enter', 'TEXTAREA', ''), () => n++))
  assert.ok(!enterSubmits(ev('a', 'INPUT', 'text'), () => n++))
  assert.equal(n, 1)
})
