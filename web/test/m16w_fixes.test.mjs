// Run: node --test test/m16w_fixes.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createTemplate } from '../src/utils/templates.ts'
import { draftOf, newDef } from '../src/utils/tabledef.ts'
import { limitHint } from '../src/utils/m13.ts'
import { matchName } from '../src/utils/matchname.ts'

const src = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P1-09: expression index entries and untouched rows do not block a structure edit', () => {
  const cols = [{ Name: 'email', Type: 'text', Nullable: false, Default: '', HasDef: false, PK: false, Extra: '' }]
  const base = draftOf('users', cols, [{ Name: 'users_email_lower', Columns: ['lower(email)'], Unique: true }], [])
  const d = structuredClone(base)
  assert.equal(newDef('public', d, base).Indexes[0].Columns[0], 'lower(email)')
  assert.equal(newDef('public', d).Indexes[0].Columns[0], 'lower(email)') // an expression, even without the base
  d.idx.push({ orig: '', name: 'bad', cols: 'nope', unique: false, algorithm: '', condition: '' })
  assert.throws(() => newDef('public', d, base), /names a column that is not in the table/)
})

test('P2-41: the MySQL function template is one statement', () => {
  const t = createTemplate('function', 'mysql', 'app')
  assert.match(t, /DETERMINISTIC\nRETURN 0;\n$/)
  assert.ok(!/\bBEGIN\b|\bEND\b/.test(t))
})

test('P2-42: every dialect prefixes the schema', () => {
  for (const d of ['mysql', 'mariadb', 'clickhouse']) assert.match(createTemplate('view', d, 'app'), /`app`\.`name`/)
})

test('P2-40: shorter names rank higher', () => {
  assert.ok(matchName('usr', 'users', true)[0] > matchName('usr', 'user_sessions_archive', true)[0])
})

test('P2-05/69: per-key limit hints', () => {
  assert.equal(limitHint('MAX_USER_CONNECTIONS', false), '0 = the server default')
  assert.equal(limitHint('MAX_QUERIES_PER_HOUR', false), '0 = unlimited')
  assert.equal(limitHint('CONNECTION_LIMIT', true), '-1 = no limit')
  assert.match(src('Admin.tsx'), /!u\.Limits\) return setErr\(LIMITS_UNREADABLE\)/)
})

test('P2-43: Select takes an id and the settings enum row passes it', () => {
  assert.match(src('Select.tsx'), /<button ref=\{setBtn\} id=\{id\}/)
  assert.match(src('Settings.tsx'), /<Select id=\{id\}/)
})

test('P2-39: read-only settings are shown read-only and never sent', () => {
  const s = src('Settings.tsx')
  assert.match(s, /f\.readOnly \?\? f\.ReadOnly/)
  assert.match(s, /!f\.ReadOnly && edits/)
})

test('P2-37/38: jobs error shown, unknown action is a no-op', () => {
  const s = src('Jobs.tsx')
  assert.match(s, /Could not load jobs/)
  assert.match(s, /: dismiss\(j\) : notify|it\.action === 'close' \? dismiss\(j\)/)
})

test('P2-26: backup options come from the server, the static copy is the fallback', () => {
  const s = src('Admin.tsx')
  assert.match(s, /backup\/options\?restore=\$\{restore \? 1 : 0\}/)
  assert.match(s, /extra: \[\] \}/)
})

test('P2-48/59: the import dialog sends NULL as, Empty is NULL, Keep formula apostrophe to preview and import', () => {
  const s = src('Tools.tsx')
  assert.equal((s.match(/null_as: nullAs \|\| undefined, empty_is_null: emptyNull, keep_formula_quote: keepQuote/g) ?? []).length, 2)
})
