// Run: node --test tests/chats.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { chatTitle } from '../src/utils/chats.ts'

test('chatTitle', () => {
  assert.equal(chatTitle([{ role: 'assistant', content: 'x' }, { role: 'user', content: '  hello   world ' }]), 'hello world')
  assert.equal(Array.from(chatTitle([{ role: 'user', content: 'é'.repeat(100) }])).length, 60)
})
