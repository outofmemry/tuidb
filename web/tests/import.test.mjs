// Run: node --test tests/import.test.mjs
// autoMap is pure; the test strips the types so node can load the function from ImportDialog.tsx.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const src = readFileSync(new URL('../src/components/tools/ImportDialog.tsx', import.meta.url), 'utf8')
const body = src.match(/export function autoMap[\s\S]*?\n}\n/)[0]
  .replace('export function autoMap(file: string[], table: string[], match: string, create: boolean): Record<string, string>', 'function autoMap(file, table, match, create)')
  .replace('const out: Record<string, string> = {}', 'const out = {}')
  .replace('new Set<string>()', 'new Set()')
const autoMap = new Function(body + '; return autoMap')()
test('autoMap', () => {
  assert.deepEqual(autoMap(['ID', 'x'], ['id', 'y'], 'name', false), { ID: 'id', x: '' })
  assert.deepEqual(autoMap(['ID', 'x'], ['id', 'y'], 'both', false), { ID: 'id', x: 'y' })
  assert.deepEqual(autoMap(['a', 'b'], ['p', 'q'], 'order', false), { a: 'p', b: 'q' })
  assert.deepEqual(autoMap(['a'], [], 'both', true), { a: 'a' })
})

const dialog = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P2-48/59: the import dialog sends NULL as, Empty is NULL, Keep formula apostrophe to preview and import', () => {
  const s = dialog('tools/ImportDialog.tsx')
  assert.equal((s.match(/null_as: nullAs \|\| undefined, empty_is_null: emptyNull, keep_formula_quote: keepQuote/g) ?? []).length, 2)
})

test('import dialog wording', () => {
  assert.match(dialog('tools/ImportDialog.tsx'), /\(empty: the \[csv\] null_as setting\)/)
})
