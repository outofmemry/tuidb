// Run: npm run test:keys  (or node --test tests/keys.test.mjs)
import test from 'node:test'
import assert from 'node:assert/strict'
import { canon } from '../src/utils/keys.ts'

const ev = (o) => ({ key: '', code: '', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...o })

test('canon matches the Go keymap spelling', () => {
  for (const [e, want] of [
    [{ key: 'K', code: 'KeyK', altKey: true, shiftKey: true }, 'alt+K'],
    [{ key: 'k', code: 'KeyK', altKey: true }, 'alt+k'],
    // M13 additions: close_pane alt+q, toggle_panels alt+0, move_tab alt+{ / alt+}, jobs alt+J, lock alt+L
    [{ key: 'œ', code: 'KeyQ', altKey: true }, 'alt+q'],
    [{ key: 'º', code: 'Digit0', altKey: true }, 'alt+0'],
    [{ key: '”', code: 'BracketRight', altKey: true, shiftKey: true }, 'alt+}'],
    [{ key: 'Ô', code: 'KeyJ', altKey: true, shiftKey: true }, 'alt+J'],
    [{ key: 'Ò', code: 'KeyL', altKey: true, shiftKey: true }, 'alt+L'],
    [{ key: 'd', code: 'KeyD', ctrlKey: true }, 'ctrl+d'],
    [{ key: 'Æ', code: 'Quote', altKey: true, shiftKey: true }, 'alt+"'],
    [{ key: '≥', code: 'Period', altKey: true }, 'alt+.'],
    [{ key: '“', code: 'BracketLeft', altKey: true, shiftKey: true }, 'alt+{'],
    [{ key: 'P', code: 'KeyP', shiftKey: true }, 'P'],
    [{ key: '"', code: 'Quote', shiftKey: true }, '"'],
    [{ key: '¡', code: 'Digit1', altKey: true, shiftKey: true }, 'alt+!'],
    [{ key: '!', code: 'Digit1', ctrlKey: true, shiftKey: true }, 'ctrl+!'],
    [{ key: '1', code: 'Digit1', altKey: true }, 'alt+1'],
  ]) assert.equal(canon(ev(e)), want)
})

import { Machine, isPrintable, keysLocked } from '../src/utils/keys.ts'

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

test('P2-28: shortcuts are off while the lock screen is up', () => {
  globalThis.document = { documentElement: { dataset: {} } }
  assert.equal(keysLocked(), false)
  document.documentElement.dataset.locked = '1'
  assert.equal(keysLocked(), true)
  delete globalThis.document
})
