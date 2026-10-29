// Run: node --test tests/components.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

// Component behaviour that needs React; asserted on the source so it cannot silently regress.
const src = (f) => readFileSync(new URL('../src/' + f, import.meta.url), 'utf8')

test('P2-33: the shift anchor is cleared when the row order changes', () => {
  assert.match(src('components/grid/Grid.tsx'), /setAnchor\(null\) \}, \[order, columns\.length\]/)
})
test('P2-35: the structure editor reports clean when it unmounts', () => {
  assert.match(src('components/grid/StructureEditor.tsx'), /useEffect\(\(\) => \(\) => onDirty\?\.\(false\), \[\]\)/)
})
test('P2-32/P2-31: copy_link has no token, reconnect confirms', () => {
  const w = src('routes/Workspace.tsx')
  assert.doesNotMatch(w, /copyText\(location\.origin \+ withToken/)
  assert.match(w, /case 'reconnect': return void confirm\(/)
})
test('P2-72: the console actions are supported and answered by the console pane', () => {
  const w = src('routes/Workspace.tsx'), x = src('components/shell/ConsolePane.tsx')
  for (const a of ['console_find', 'console_next', 'console_prev', 'console_copy', 'console_errors', 'console_menu']) {
    assert.ok(w.includes(`'${a}'`), a + ' in SUPPORTED'); assert.ok(x.includes(`case '${a}'`), a + ' in ConsolePane')
  }
})
test('P2-14: the result menu can pin a result', () => assert.match(src('components/query/QueryTab.tsx'), /load_more: loadMore, pin: /))
