import { useEffect, useMemo, useState } from 'react'
import type { Val } from '../utils/api'
import { saveBlob } from '../utils/api'
import { Dialog, ask } from './Dialogs'
import { Select } from './Select'
import { Tree } from './Extras'
import { base64Of, openInWindow, sniff } from '../utils/m15_bin'
import { draw, geomOf, wkt } from '../utils/m15_wkb'

export type CellTarget = { row: string; col: string; readOnly?: boolean }

const pretty = (t: string) => {
  const s = t.trim()
  if (s.length > 1 && (s[0] === '{' || s[0] === '[')) { try { return JSON.stringify(JSON.parse(s), null, 2) } catch { /* not JSON */ } }
  return t
}

// Binary cells arrive as \x<hex> (db.FormatValue); everything else is text.
const isBin = (v: string | null) => v !== null && /^\\x([0-9a-f]{2})*$/i.test(v)
const hexOf = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join(' ')
const bytesOf = (hex: string) => { const h = hex.replace(/\s+/g, ''); return /^([0-9a-f]{2})*$/i.test(h) ? Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16)) : null }
const ENCODINGS = ['utf-8', 'iso-8859-1', 'windows-1252', 'utf-16le', 'utf-16be', 'shift_jis', 'euc-kr', 'gb18030', 'big5']

// CellEditor is the popover for one cell: long text and JSON documents do not fit in a grid cell.
// Ctrl/Cmd+Enter saves, Esc cancels. JSON opens pretty-printed; saving unchanged text is a no-op.
// Text / Hex switches the view (hex is editable); "View using encoding" decodes the same bytes with another charset.
export function CellEditor({ col, value, readOnly, onDone }: { col: string; value: string | null; readOnly?: boolean; onDone: (v: Val | undefined) => void }) {
  const bin = isBin(value)
  const orig = value === null ? new Uint8Array() : bin ? bytesOf(value!.slice(2))! : new TextEncoder().encode(value)
  const start = value === null ? '' : bin ? '' : pretty(value)
  const [text, setText] = useState(start)
  const [mode, setMode] = useState<'text' | 'hex'>('text')
  const [enc, setEnc] = useState('utf-8')
  const [hex, setHex] = useState(hexOf(orig))
  const hexBytes = bytesOf(hex)
  const hexDirty = hex !== hexOf(orig)
  const decoded = (b: Uint8Array) => { try { return new TextDecoder(enc).decode(b) } catch { return '' } }
  const save = () => {
    if (mode === 'hex') {
      if (!hexBytes) return
      if (!hexDirty) return onDone(undefined)
      return onDone(bin ? '\\x' + Array.from(hexBytes, (x) => x.toString(16).padStart(2, '0')).join('') : new TextDecoder().decode(hexBytes))
    }
    onDone(text === start ? undefined : text)
  }
  const preview = enc !== 'utf-8' || bin // a decoded view of bytes: read-only
  const shown = mode === 'hex' ? hex : preview ? decoded(bin ? orig : new TextEncoder().encode(text)) : text
  return (
    <Dialog title={readOnly ? col : `Edit ${col}`} onClose={() => onDone(undefined)}>
      {value === null && <p className="text-ink-2" style={{ margin: '0 0 6px' }}>This cell is NULL.</p>}
      <div className="flex items-center gap-2 mb-2">
        <div role="group" aria-label="View" className="flex gap-1"><button className={mode === 'text' ? 'btn-on' : ''} onClick={() => setMode('text')}>Text</button><button className={mode === 'hex' ? 'btn-on' : ''} onClick={() => setMode('hex')}>Hex</button></div>
        {mode === 'text' && <><span className="text-ink-2">View using encoding</span><Select aria-label="Encoding" value={enc} onChange={setEnc} options={ENCODINGS} /></>}
      </div>
      <textarea className="font-mono w-full min-h-[40vh] max-h-[60vh] resize-y text-[12.5px] leading-[1.45] px-3 py-2.5 bg-canvas" value={shown} readOnly={readOnly || (mode === 'text' && preview)} autoFocus spellCheck={false}
        onChange={(e) => (mode === 'hex' ? setHex(e.target.value) : setText(e.target.value))}
        onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !readOnly) { e.preventDefault(); save() } }} />
      {mode === 'hex' && !hexBytes && <div className="text-del">Hex needs pairs of 0-9 a-f.</div>}
      <div className="flex justify-end gap-2 mt-[18px]">
        {bin && <button className="mr-auto" onClick={async () => { const n = await ask('Save the bytes as', 'cell.bin'); if (n) saveBlob(new Blob([orig as BlobPart]), n) }}>Save as…</button>}
        {!readOnly && <>
          <span className="flex-1 min-w-0 text-left text-ink-2">{mode === 'hex' ? `${hexBytes?.length ?? 0} bytes` : `${text.length} characters`}</span>
          <button onClick={() => onDone({ $: 'null' })}>Set NULL</button>
          <button onClick={() => onDone({ $: 'default' })}>Set DEFAULT</button>
          <button onClick={() => onDone({ $: 'empty' })}>Set empty</button>
        </>}
        <button onClick={() => onDone(undefined)}>{readOnly ? 'Close' : 'Cancel'}</button>
        {!readOnly && <button className="btn-primary" onClick={save} title="Ctrl+Enter">Save</button>}
      </div>
    </Dialog>
  )
}
