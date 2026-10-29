// Run: node --test tests/m15_carets.test.mjs
// select_next_occurrence must ADD a caret (CodeMirror's selectNextOccurrence keeps the old ranges), not move the selection.
import test from 'node:test'
import assert from 'node:assert/strict'
import { EditorState } from '@codemirror/state'
import { selectNextOccurrence } from '@codemirror/search'

test('select_next_occurrence adds a selection range', () => {
  const view = { state: EditorState.create({ doc: 'foo bar foo baz foo', selection: { anchor: 0, head: 3 }, extensions: [EditorState.allowMultipleSelections.of(true)] }), dispatch: (spec) => { view.state = (spec.state ? spec : view.state.update(spec)).state } }
  assert.ok(selectNextOccurrence(view))
  assert.deepEqual(view.state.selection.ranges.map((r) => [r.from, r.to]), [[0, 3], [8, 11]])
  selectNextOccurrence(view)
  assert.equal(view.state.selection.ranges.length, 3)
})
