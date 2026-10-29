// Run: node --test tests/menus.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { filterMenu, whyNot } from '../src/utils/menus.ts'

const list = [
  { label: 'Open', action: 'o' },
  { label: 'Structure', action: 's', when: 'tableish' },
  { label: 'Triggers', action: 't', when: 'table;cap:Triggers' },
  { label: 'Edit definition', action: 'e', when: 'definition' },
  { label: 'Remove', action: 'r', when: 'group' },
  { label: 'Reveal', action: 'v', ui: 'tui', why: 'the browser downloads' },
]

test('filterMenu follows menus.Item.Holds', () => {
  const l = (env) => filterMenu(list, env).map((i) => i.action).join('')
  assert.equal(l({ kind: 'table', caps: { Triggers: true } }), 'ostv')
  assert.equal(l({ kind: 'table', caps: {} }), 'osv')
  assert.equal(l({ kind: 'view' }), 'osev')
  assert.equal(l({ kind: 'function', inGroup: true }), 'oerv')
})

test('tui-only entries are greyed with the reason', () => {
  assert.equal(whyNot(list[5]), 'the browser downloads')
  assert.equal(whyNot(list[0]), undefined)
})
