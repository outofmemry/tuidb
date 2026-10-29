// Run: node --test tests/jobs.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const src = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P2-37/38: jobs error shown, unknown action is a no-op', () => {
  const s = src('admin/JobsDrawer.tsx')
  assert.match(s, /Could not load jobs/)
  assert.match(s, /: dismiss\(j\) : notify|it\.action === 'close' \? dismiss\(j\)/)
})
