// Run: node --test tests/danger.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { enterAnswers, isDanger, typedOk } from '../src/utils/prompts.ts'

test('destructive confirm buttons make a red dialog (Cancel is focused, Enter cancels)', () => {
  for (const ok of ['Delete', 'Drop with dependents', 'Truncate with dependents', 'Discard', 'Discard and load', 'Kill', 'Clear', 'Roll back and close', 'Overwrite', 'Replace', 'Close', 'Open and run'])
    assert.ok(isDanger(ok), ok)
  for (const ok of [undefined, 'Continue', 'OK', 'Switch', 'Copy', 'Use it', 'Reconnect', 'Structure and rows']) assert.ok(!isDanger(ok), String(ok))
})

test('a typed-name prompt only answers when the text matches exactly', () => {
  assert.equal(typedOk('', 'users'), false)
  assert.equal(typedOk('user', 'users'), false)
  assert.equal(typedOk('Users', 'users'), false)
  assert.equal(typedOk('users', 'users'), true)
  assert.equal(enterAnswers('prompt', 'use', 'users'), false)
  assert.equal(enterAnswers('prompt', 'users', 'users'), true)
  assert.equal(typedOk('3', '3'), true) // the number of marked tables
  assert.equal(typedOk('2', '3'), false)
})

test('a plain prompt needs any non-blank text; Enter never answers a confirm itself', () => {
  assert.equal(typedOk('  ', undefined), false)
  assert.equal(typedOk('x', undefined), true)
  assert.equal(enterAnswers('confirm', '', undefined), false)
})
