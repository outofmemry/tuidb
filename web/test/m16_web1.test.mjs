// Run: node --test test/m16_web1.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const store = () => { const m = new Map(); return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, String(v)), removeItem: (k) => m.delete(k) } }
globalThis.sessionStorage = store()
globalThis.localStorage = store()
const { openSessions, loadTabs, lastWorkspaces, remember } = await import('../src/utils/workspaces.ts')

test('P2-29: valid JSON of the wrong shape reads like nothing stored', () => {
  for (const bad of ['5', '[]', '"x"', 'true']) {
    sessionStorage.setItem('webtuidb-open', bad); localStorage.setItem('webtuidb-last', bad)
    assert.deepEqual(openSessions(), {}); assert.deepEqual(lastWorkspaces(), {})
    assert.doesNotThrow(() => remember('a', 's1'))
    sessionStorage.removeItem('webtuidb-open')
  }
  for (const bad of ['{}', '[]', '5', '{"tabs":3,"active":[0,0]}', '{"tabs":[],"active":"x"}']) {
    sessionStorage.setItem('webtuidb-tabs-s1', bad)
    assert.equal(loadTabs('s1'), null, bad)
  }
  sessionStorage.setItem('webtuidb-tabs-s1', '{"tabs":[{"kind":"query"},null],"active":[1,0],"split":false}')
  assert.deepEqual(loadTabs('s1'), { tabs: [{ kind: 'query' }], active: [1, 0], split: false })
})

import { keysLocked } from '../src/utils/keys.ts'
test('P2-28: shortcuts are off while the lock screen is up', () => {
  globalThis.document = { documentElement: { dataset: {} } }
  assert.equal(keysLocked(), false)
  document.documentElement.dataset.locked = '1'
  assert.equal(keysLocked(), true)
  delete globalThis.document
})

import { forThisTab } from '../src/utils/links.ts'
test('P2-36: the filter-column event is answered only by its own tab', () => {
  assert.equal(forThisTab({ sid: 's1', key: 'public.a' }, 's1', 'public.a'), true)
  assert.equal(forThisTab({ sid: 's1', key: 'public.a' }, 's1', 'public.b'), false)
  assert.equal(forThisTab({ sid: 's2', key: 'public.a' }, 's1', 'public.a'), false)
  assert.equal(forThisTab(undefined, 's1', 'public.a'), false)
})

import { tableStateBody } from '../src/utils/tablestate.ts'
test('P2-34: the default sort is not saved unless the user chose a sort', () => {
  const base = { hidden: [], filters: {}, showBar: false, sort: [{ Column: 'id', Desc: false }], widths: { a: 3 }, sortChosen: false, widthsChosen: false }
  assert.deepEqual(Object.keys(tableStateBody(base)).sort(), ['filters', 'hidden', 'show_filters'])
  const b = tableStateBody({ ...base, sortChosen: true, widthsChosen: true })
  assert.deepEqual(b.sort, base.sort); assert.deepEqual(b.widths, base.widths)
})

// Component behaviour that needs React; asserted on the source so it cannot silently regress.
const src = (f) => readFileSync(new URL('../src/' + f, import.meta.url), 'utf8')
test('P1-02: both tabs key the cell editor by row and column', () => {
  for (const f of ['components/DataTab.tsx', 'components/QueryTab.tsx']) assert.match(src(f), /<CellEditor key=\{`\$\{cell\.row\}:\$\{cell\.col\}`\}/, f)
})
test('P2-33: the shift anchor is cleared when the row order changes', () => {
  assert.match(src('components/Grid.tsx'), /setAnchor\(null\) \}, \[order, columns\.length\]/)
})
test('P2-35: the structure editor reports clean when it unmounts', () => {
  assert.match(src('components/StructureEditor.tsx'), /useEffect\(\(\) => \(\) => onDirty\?\.\(false\), \[\]\)/)
})
test('P2-32/P2-31: copy_link has no token, reconnect confirms', () => {
  const w = src('routes/Workspace.tsx')
  assert.doesNotMatch(w, /copyText\(location\.origin \+ withToken/)
  assert.match(w, /case 'reconnect': return void confirm\(/)
})
test('P2-72: the console actions are supported and answered by the console pane', () => {
  const w = src('routes/Workspace.tsx'), x = src('components/Extras.tsx')
  for (const a of ['console_find', 'console_next', 'console_prev', 'console_copy', 'console_errors', 'console_menu']) {
    assert.ok(w.includes(`'${a}'`), a + ' in SUPPORTED'); assert.ok(x.includes(`case '${a}'`), a + ' in ConsolePane')
  }
})
test('P2-14: the result menu can pin a result', () => assert.match(src('components/QueryTab.tsx'), /load_more: loadMore, pin: /))
