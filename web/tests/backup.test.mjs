// Run: node --test tests/backup.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { extraArgs, commonBackupOptions } from '../src/utils/backup.ts'

test('extraArgs keeps ticked flags first, splits text, groups quotes, drops repeats', () => {
  assert.deepEqual(extraArgs(['--clean'], '--no-comments "--x=a b" --clean'), ['--clean', '--no-comments', '--x=a b'])
  assert.deepEqual(extraArgs([], '  '), [])
  assert.ok(commonBackupOptions('postgres', false).some((o) => o.Flag === '--no-owner'))
  assert.deepEqual(commonBackupOptions('redis', true), [])
})

const src = (f) => readFileSync(new URL('../src/components/' + f, import.meta.url), 'utf8')

test('P2-26: backup options come from the server, the static copy is the fallback', () => {
  const s = src('admin/BackupDialog.tsx')
  assert.match(s, /backup\/options\?restore=\$\{restore \? 1 : 0\}/)
  assert.match(s, /extra: \[\] \}/)
})
