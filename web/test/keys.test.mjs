// Run: npm run test:keys
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
