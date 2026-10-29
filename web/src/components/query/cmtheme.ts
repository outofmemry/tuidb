import { EditorView } from '@codemirror/view'
import { HighlightStyle, syntaxHighlighting } from '@codemirror/language'
import { tags as t } from '@lezer/highlight'

// The editor's theme reads the page's palette, so the SQL editor, the read-only SQL viewer and the grid share
// one set of colours (and follow the [theme] overrides, including the keyword / string / number / comment keys).
const colours = HighlightStyle.define([
  { tag: [t.keyword, t.operatorKeyword, t.modifier, t.controlKeyword, t.definitionKeyword], color: 'var(--syn-keyword)' },
  { tag: [t.string, t.special(t.string), t.character], color: 'var(--syn-string)' },
  { tag: [t.number, t.integer, t.float, t.bool, t.null], color: 'var(--syn-number)' },
  { tag: [t.comment, t.lineComment, t.blockComment], color: 'var(--syn-comment)', fontStyle: 'italic' },
  { tag: [t.function(t.variableName), t.function(t.propertyName), t.macroName], color: 'var(--syn-fn)' },
  { tag: [t.typeName, t.className, t.namespace], color: 'var(--syn-type)' },
  { tag: [t.operator, t.punctuation, t.bracket, t.separator], color: 'var(--ink-2)' },
  { tag: [t.variableName, t.propertyName, t.name], color: 'var(--ink)' },
  { tag: t.invalid, color: 'var(--del)' },
])

const chrome = EditorView.theme({
  '&': { color: 'var(--ink)', backgroundColor: 'var(--canvas)' },
  '.cm-content': { caretColor: 'var(--ink)', padding: '6px 0' },
  '.cm-line': { padding: '0 8px' },
  '.cm-gutters': { backgroundColor: 'var(--canvas)', color: 'var(--ink-3)', borderRight: '1px solid var(--rule)' },
  '.cm-lineNumbers .cm-gutterElement': { padding: '0 10px 0 12px', minWidth: '3.2em' },
  '.cm-activeLine': { backgroundColor: 'var(--panel)' },
  '.cm-activeLineGutter': { backgroundColor: 'var(--panel)', color: 'var(--ink-2)' },
  '.cm-matchingBracket, &.cm-focused .cm-matchingBracket': { backgroundColor: 'color-mix(in srgb, var(--accent) 22%, transparent)', outline: '1px solid color-mix(in srgb, var(--accent) 50%, transparent)' },
  '.cm-nonmatchingBracket, &.cm-focused .cm-nonmatchingBracket': { backgroundColor: 'color-mix(in srgb, var(--del) 25%, transparent)' },
  '.cm-selectionMatch': { backgroundColor: 'color-mix(in srgb, var(--accent) 16%, transparent)' },
  '.cm-foldGutter .cm-gutterElement': { color: 'var(--ink-3)' },
  '.cm-foldGutter .cm-gutterElement:hover': { color: 'var(--ink)' },
  '.cm-highlightSpace': { backgroundImage: 'radial-gradient(circle at 50% 55%, var(--rule-2) 20%, transparent 22%)' },
  '.cm-highlightTab': { backgroundImage: 'linear-gradient(to right, var(--rule-2) 1px, transparent 1px)' },
  '.cm-tooltip': { backgroundColor: 'var(--panel)', border: '1px solid var(--rule-2)', color: 'var(--ink)' },
  '.cm-completionLabel': { fontFamily: 'var(--font-mono)' },
  '.cm-completionDetail': { color: 'var(--ink-3)', fontStyle: 'normal', marginLeft: '8px' },
  '.cm-completionIcon': { color: 'var(--ink-3)' },
  '.cm-completionMatchedText': { textDecoration: 'none', color: 'var(--accent)', fontWeight: '600' },
  '.cm-placeholder': { color: 'var(--ink-3)' },
})

// cmTheme is the one extension both editors pass as `theme`; dark and light come from the page's variables.
export const cmTheme = [chrome, syntaxHighlighting(colours)]
