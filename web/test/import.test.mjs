// autoMap is pure; the test strips the types so node can load the function from Tools.tsx.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
const src = readFileSync(new URL('../src/components/Tools.tsx', import.meta.url), 'utf8')
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
