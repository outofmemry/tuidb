// Run: npm run test:keys
import test from 'node:test'
import assert from 'node:assert/strict'
import { canon } from '../src/utils/keys.ts'

const ev = (o) => ({ key: '', code: '', ctrlKey: false, altKey: false, shiftKey: false, metaKey: false, ...o })

test('canon matches the Go keymap spelling', () => {
  for (const [e, want] of [
    [{ key: 'K', code: 'KeyK', altKey: true, shiftKey: true }, 'alt+K'],
    [{ key: 'k', code: 'KeyK', altKey: true }, 'alt+k'],
    [{ key: 'd', code: 'KeyD', ctrlKey: true }, 'ctrl+d'],
    [{ key: 'Æ', code: 'Quote', altKey: true, shiftKey: true }, 'alt+"'],
    [{ key: '≥', code: 'Period', altKey: true }, 'alt+.'],
    [{ key: '“', code: 'BracketLeft', altKey: true, shiftKey: true }, 'alt+{'],
    [{ key: 'P', code: 'KeyP', shiftKey: true }, 'P'],
    [{ key: '"', code: 'Quote', shiftKey: true }, '"'],
  ]) assert.equal(canon(ev(e)), want)
})
