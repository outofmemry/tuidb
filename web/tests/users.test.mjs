// Run: node --test tests/users.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const src = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P2-05/69: an unreadable limits column cannot be saved over', () => {
  assert.match(src('admin/UsersDialog.tsx'), /!u\.Limits\) return setErr\(LIMITS_UNREADABLE\)/)
})
