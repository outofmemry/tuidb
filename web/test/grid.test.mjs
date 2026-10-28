// Run: node --test test/grid.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { cellMatches, findNext, replaceIn } from '../src/utils/gridfind.ts'
import { matchName, buildKeyTree, flattenKeys } from '../src/utils/matchname.ts'
import { draftOf, newDef, oldDef, dirty } from '../src/utils/tabledef.ts'
import { menuItems, listOf, columnTip, columnDef } from '../src/utils/m15_menus.ts'

const o = (p) => ({ text: 'ab', col: '', op: 'Contains', matchCase: false, ...p })

test('find operators and match case', () => {
  assert.ok(cellMatches('xABy', o()))
  assert.ok(!cellMatches('xABy', o({ matchCase: true })))
  assert.ok(cellMatches('xy', o({ op: 'Not contains' })))
  assert.ok(!cellMatches(null, o({ op: 'Not contains' })))
  assert.ok(cellMatches('abc', o({ op: 'Has prefix' })) && !cellMatches('cab', o({ op: 'Has prefix' })))
  assert.ok(cellMatches('cab', o({ op: 'Has suffix' })))
})

test('findNext wraps and honours the column', () => {
  const g = [['a', 'ab'], ['ab', 'z']]
  const cell = (r, c) => g[r][c]
  assert.deepEqual(findNext(cell, 2, ['x', 'y'], o(), { r: 0, c: 1 }, 1), { r: 1, c: 0 })
  assert.deepEqual(findNext(cell, 2, ['x', 'y'], o({ col: 'y' }), { r: 0, c: 1 }, 1), { r: 0, c: 1 })
  assert.equal(findNext(cell, 2, ['x', 'y'], o({ text: 'q' }), { r: 0, c: 0 }, 1), null)
  assert.deepEqual(findNext(cell, 2, ['x', 'y'], o(), { r: 0, c: 0 }, -1), { r: 1, c: 0 })
})

test('replaceIn', () => {
  assert.equal(replaceIn('abXAB', o(), '-'), '-X-')
  assert.equal(replaceIn('abc', o({ op: 'Has prefix' }), 'Z'), 'Zc')
  assert.equal(replaceIn('cab', o({ op: 'Has suffix' }), 'Z'), 'cZ')
  assert.equal(replaceIn('xyz', o(), '-'), null)
  assert.equal(replaceIn('İab', o(), '-'), 'İ-') // lowercasing 'İ' changes its length; slicing must not shift
  assert.equal(replaceIn('a.b', o({ text: '.' }), '-'), 'a-b')
})

test('matchName anchors, fuzzy and contains', () => {
  assert.deepEqual(matchName('^us', 'Users', true), [10, true])
  assert.deepEqual(matchName('rs$', 'Users', true), [10, true])
  assert.equal(matchName('^rs', 'Users', true)[1], false)
  assert.equal(matchName('usr', 'users', true)[1], true)
  assert.equal(matchName('usr', 'users', false)[1], false)
  // the length penalty (floor(len/8)) ranks the shorter name first, as the TUI does
  assert.ok(matchName('usr', 'users', true)[0] > matchName('usr', 'user_sessions_archive', true)[0])
  assert.equal(matchName('usr', 'users', true)[0], 8)
})

test('redis key folders merge single-child chains', () => {
  const t = buildKeyTree([{ Name: 'user:1:name' }, { Name: 'user:2:name' }, { Name: 'user:2:age' }, { Name: 'solo' }])
  const rows = flattenKeys(t, new Set(['user', 'user:2']))
  assert.deepEqual(rows.map((r) => [r.depth, r.seg]), [[0, 'solo'], [0, 'user'], [1, '1:name'], [1, '2'], [2, 'age'], [2, 'name']])
})

test('table def: rename column keeps OrigName; validation', () => {
  const base = draftOf('t', [{ Name: 'id', Type: 'int', Nullable: false, Default: '', HasDef: false, PK: true, Extra: '' }], [{ Name: 'i1', Columns: ['id'] }], [])
  const d = structuredClone(base)
  assert.ok(!dirty(d, base))
  d.cols[0].name = 'pk'; d.idx[0].cols = 'pk'
  const n = newDef('', d)
  assert.equal(n.Columns[0].OrigName, 'id')
  assert.deepEqual(n.PrimaryKey, ['pk'])
  assert.equal(oldDef('', base).Columns[0].OrigName, 'id')
  d.cols.push({ orig: '', name: 'PK', type: 'int', nullable: true, def: '', pk: false, extra: '' })
  assert.throws(() => newDef('', d), /Duplicate/)
})

test('menus: the editable predicate filters result menus; header keeps the list order', () => {
  const result = [{ label: 'Quick look', action: 'quick_look' }, { label: 'Set value', action: 'set_value', when: 'editable' }, { label: 'Add row', action: 'add_row', when: 'editable' }, { label: 'Copy rows', action: 'copy_rows' }, { label: 'Copy column', action: 'copy_column' }]
  const ro = menuItems(listOf({ result }, 'result'), { editable: false }).map((i) => i.action)
  assert.ok(!ro.includes('set_value') && !ro.includes('add_row') && ro.includes('quick_look') && ro.includes('copy_column'))
  const rw = menuItems(listOf({ result }, 'result'), { editable: true }).map((i) => i.action)
  assert.ok(rw.includes('set_value') && rw.indexOf('add_row') < rw.indexOf('copy_rows'))
  assert.deepEqual(listOf(undefined, 'header'), []) // menus always come from meta; there is no hand-copied fallback
  // a list from /api/meta wins over the fallback; terminal-only entries stay but greyed out with the reason
  const own = menuItems(listOf({ header: [{ label: 'A', action: 'a' }, { label: 'B', action: 'b', ui: 'tui', why: 'terminal only' }] }, 'header'), {})
  assert.deepEqual(own.map((i) => [i.action, !!i.disabled]), [['a', false], ['b', true]])
})

test('header tooltip and column definition', () => {
  const c = { Name: 'id', Type: 'int', Nullable: false, Default: '', HasDef: false, PK: true }
  assert.equal(columnTip(c), 'id\nint · not null · no default · primary key')
  assert.equal(columnTip({ ...c, PK: false, Nullable: true, HasDef: true, Default: '0', FK: { RefSchema: 's', RefTable: 'u', RefColumns: ['id'] } }), 'id\nint · nullable · default 0 · foreign key to u(id)')
  assert.equal(columnDef(c), 'id int NOT NULL PRIMARY KEY')
})
