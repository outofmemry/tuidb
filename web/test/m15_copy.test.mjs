// Run: node --test test/m15_copy.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { cellsAs, columnText, rectOf, rectSize } from '../src/utils/m15_copy.ts'
import { fnv1a32, foldedIndexes } from '../src/utils/m15_folds.ts'

const cols = [{ name: 'id', type: 'int' }, { name: 'name', type: 'text' }, { name: 'note', type: 'text' }]
const data = [['1', 'a,b', null], ['2', 'c|d', 'x']]
const cell = (r, c) => data[r][c]
const csv = { Delimiter: ',', Quote: '"', LineBreak: '\n' }
const all = { r0: 0, r1: 1, c0: 0, c1: 2 }

test('cells as CSV, TSV (Plain), JSON, Markdown', () => {
  assert.equal(cellsAs('CSV', all, cols, cell, csv, 'postgres'), '1,"a,b",\n2,c|d,x')
  assert.equal(cellsAs('CSV with Header', all, cols, cell, csv, 'postgres'), 'id,name,note\n1,"a,b",\n2,c|d,x')
  assert.equal(cellsAs('Plain', all, cols, cell, csv, 'postgres'), '1\ta,b\tNULL\n2\tc|d\tx')
  assert.deepEqual(JSON.parse(cellsAs('JSON', all, cols, cell, csv, 'postgres')), [{ id: '1', name: 'a,b', note: null }, { id: '2', name: 'c|d', note: 'x' }])
  assert.equal(cellsAs('Markdown Table', all, cols, cell, csv, 'postgres'), '| id | name | note |\n| --- | --- | --- |\n| 1 | a,b | NULL |\n| 2 | c\\|d | x |')
})

test('a sub-rectangle keeps only its columns; INSERT and HTML work too', () => {
  const r = { r0: 1, r1: 1, c0: 1, c1: 2 }
  assert.equal(cellsAs('CSV with Header', r, cols, cell, csv, 'postgres'), 'name,note\nc|d,x')
  assert.equal(cellsAs('INSERT Statement', r, cols, cell, csv, 'postgres', 't'), `INSERT INTO "t" ("name", "note") VALUES ('c|d', 'x');`)
  assert.match(cellsAs('HTML', r, cols, cell, csv, 'postgres'), /<td>c\|d<\/td>/)
})

test('rectOf and columnText', () => {
  assert.deepEqual(rectOf(null, 3, 2), { r0: 3, r1: 3, c0: 2, c1: 2 })
  assert.equal(rectSize(all), 6)
  assert.equal(columnText(2, 2, cell), 'NULL\nx')
})

test('folds follow the statement text', () => {
  assert.equal(fnv1a32(''), '811c9dc5')
  assert.equal(fnv1a32('a'), 'e40c292c')
  const before = ['select 1;', 'select 2;']
  const saved = [before[1]].map((t) => fnv1a32(t.trim()))
  assert.deepEqual(foldedIndexes(saved, ['select 0;', ...before]), [2]) // a statement added above: the fold stays on "select 2;"
  assert.deepEqual(foldedIndexes(saved, ['select 1;', 'select 22;']), []) // the folded statement was edited: it unfolds
})
