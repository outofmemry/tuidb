// Run: node --test tests/settings.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'

const src = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P2-43: Select takes an id and the settings enum row passes it', () => {
  assert.match(src('shell/Select.tsx'), /<button ref=\{setBtn\} id=\{id\}/)
  assert.match(src('admin/SettingsDialog.tsx'), /<Select id=\{id\}/)
})

test('P2-39: read-only settings are shown read-only and never sent', () => {
  const s = src('admin/SettingsDialog.tsx')
  assert.match(s, /f\.readOnly \?\? f\.ReadOnly/)
  assert.match(s, /!f\.ReadOnly && edits/)
})

test('settings warning wiring', () => {
  assert.match(src('admin/SettingsDialog.tsx'), /r\?\.warning/)
})
