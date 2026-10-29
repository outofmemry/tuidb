// Run: node --test tests/cell_editor.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

// Component behaviour that needs React; asserted on the source so it cannot silently regress.
const src = (f) => readFileSync(new URL('../src/' + f, import.meta.url), 'utf8')

test('P1-02: both tabs key the cell editor by row and column', () => {
  for (const f of ['components/grid/DataTab.tsx', 'components/query/QueryTab.tsx']) assert.match(src(f), /<CellEditor key=\{`\$\{cell\.row\}:\$\{cell\.col\}`\}/, f)
})
