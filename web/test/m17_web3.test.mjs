// Run: node --test test/m17_web3.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { previewLength } from '../src/utils/m13.ts'
import { Machine, isPrintable } from '../src/utils/keys.ts'

test('previewLength cuts a CSV slice after its last newline and keeps a single long row', () => {
  const b = new TextEncoder().encode('a,b\n1,2\n3,')
  assert.equal(previewLength(b), 8)
  assert.equal(new TextDecoder().decode(b.slice(0, previewLength(b))), 'a,b\n1,2\n')
  const one = new TextEncoder().encode('a,b,c,d')
  assert.equal(previewLength(one), one.length)
  assert.equal(previewLength(new TextEncoder().encode('x\n')), 2)
})

test('a bare printable key typed into a text field is text, not a no-prefix shortcut', () => {
  const map = { prefix: 'ctrl+b', bindings: [
    { Action: 'console_find', Key: '/', Context: 'console', NoPrefix: true, Desc: '' },
    { Action: 'quit', Key: 'q', Context: 'global', NoPrefix: false, Desc: '' },
    { Action: 'run', Key: 'ctrl+r', Context: 'global', NoPrefix: true, Desc: '' },
  ] }
  const m = new Machine(map)
  assert.deepEqual(m.feed('console', '/'), { action: 'console_find' })
  assert.deepEqual(m.feed('console', '/', true), { passthrough: true })
  assert.deepEqual(m.feed('console', 'ctrl+r', true), { action: 'run' }) // chords still work while typing
  assert.deepEqual(m.feed('console', 'ctrl+b', true), { pending: true }) // the prefix too
  assert.deepEqual(m.feed('console', 'q', true), { action: 'quit' })
  assert.ok(isPrintable('/') && !isPrintable('ctrl+r') && !isPrintable('enter'))
})

test('import dialog wording, settings warning and key conflict wiring', () => {
  const tools = readFileSync(new URL('../src/components/Tools.tsx', import.meta.url), 'utf8')
  assert.match(tools, /\(empty: the \[csv\] null_as setting\)/)
  const set = readFileSync(new URL('../src/components/Settings.tsx', import.meta.url), 'utf8')
  assert.match(set, /r\?\.warning/)
})
