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
// move: the Tab key saved the cell and wants the editor on the next (1) or previous (-1) cell
export function CellEditor({ col, value, readOnly, onDone, imagePreview = true, decodeSpatial = true }: { col: string; value: string | null; readOnly?: boolean; onDone: (v: Val | undefined, move?: 1 | -1) => void; imagePreview?: boolean; decodeSpatial?: boolean }) {
  const [forceBin, setForceBin] = useState(false) // a binary file was loaded into a cell that held text
  const origBin = isBin(value), bin = origBin || forceBin
  const orig = value === null ? new Uint8Array() : origBin ? bytesOf(value!.slice(2))! : new TextEncoder().encode(value)
  const start = value === null ? '' : origBin ? '' : pretty(value)
  const [text, setText] = useState(start)
  const media = useMemo(() => (origBin && imagePreview ? sniff(orig) : null), [value, imagePreview]) // eslint-disable-line react-hooks/exhaustive-deps
  const geo = useMemo(() => (decodeSpatial ? geomOf(value) : null), [value, decodeSpatial])
  const drawing = useMemo(() => (geo ? draw(geo.geom) : null), [geo])
  const [mode, setMode] = useState<'text' | 'hex' | 'tree' | 'base64' | 'image' | 'wkt' | 'map'>(media?.kind === 'image' ? 'image' : drawing ? 'map' : 'text')
  const [imgUrl, setImgUrl] = useState('')
  useEffect(() => { // the image is a blob URL made from the cell's bytes, revoked when the dialog goes
    if (media?.kind !== 'image') return
    const u = URL.createObjectURL(new Blob([orig as BlobPart], { type: media.mime })); setImgUrl(u)
    return () => URL.revokeObjectURL(u)
  }, [media]) // eslint-disable-line react-hooks/exhaustive-deps
  const openPdf = () => { const u = URL.createObjectURL(new Blob([orig as BlobPart], { type: 'application/pdf' })); window.open(u, '_blank'); setTimeout(() => URL.revokeObjectURL(u), 60_000) }
  const isJson = !bin && /^\s*[{[]/.test(text) && (() => { try { JSON.parse(text); return true } catch { return false } })()
  const [enc, setEnc] = useState('utf-8')
  const [hex, setHex] = useState(hexOf(orig))
  const hexBytes = bytesOf(hex)
  const hexDirty = hex !== hexOf(orig) || forceBin
  const decoded = (b: Uint8Array) => { try { return new TextDecoder(enc).decode(b) } catch { return '' } }
  const minify = (t: string) => { try { return JSON.stringify(JSON.parse(t)) } catch { return t } }
  const save = (move?: 1 | -1) => {
    if (mode === 'hex') {
      if (!hexBytes) return
      if (!hexDirty) return onDone(undefined, move)
      return onDone(bin ? '\\x' + Array.from(hexBytes, (x) => x.toString(16).padStart(2, '0')).join('') : new TextDecoder().decode(hexBytes), move)
    }
    onDone(text === start ? undefined : text, move)
  }
  const preview = enc !== 'utf-8' || origBin // a decoded view of bytes: read-only
  const shown = mode === 'hex' ? hex : mode === 'base64' ? base64Of(hexBytes ?? orig) : mode === 'wkt' ? (geo ? wkt(geo) : '') : preview ? decoded(origBin ? orig : new TextEncoder().encode(text)) : text
  const readOnlyView = !!readOnly || mode === 'base64' || mode === 'wkt' || (mode === 'text' && preview)
  const toHex = (b: Uint8Array) => hexOf(b)
  const load = async (f?: File) => { // "Load from file": text replaces the text, anything else becomes bytes
    if (!f) return
    const b = new Uint8Array(await f.arrayBuffer())
    try { setText(new TextDecoder('utf-8', { fatal: true }).decode(b)); setMode('text'); setEnc('utf-8'); setForceBin(false) } catch {
      if (b.length > 8 << 20) return
      setForceBin(true); setHex(toHex(b)); setMode('hex')
    }
  }
  const saveFile = async () => {
    const bytes = mode === 'hex' ? hexBytes : bin ? orig : new TextEncoder().encode(text)
    if (!bytes) return
    const n = await ask('Save as', bin || mode === 'hex' ? 'cell.bin' : isJson ? 'cell.json' : 'cell.txt')
    if (n) saveBlob(new Blob([bytes as BlobPart]), n)
  }
  return (
    <Dialog title={readOnly ? col : `Edit ${col}`} onClose={() => onDone(undefined)}>
      {value === null && <p className="text-ink-2" style={{ margin: '0 0 6px' }}>This cell is NULL.</p>}
      <div className="flex items-center gap-2 mb-2 flex-wrap">
        <div role="group" aria-label="View" className="flex gap-1">
          <button className={mode === 'text' ? 'btn-on' : ''} onClick={() => setMode('text')}>{isJson ? 'Raw' : 'Text'}</button>
          {isJson && <button className={mode === 'tree' ? 'btn-on' : ''} onClick={() => setMode('tree')}>Tree</button>}
          <button className={mode === 'hex' ? 'btn-on' : ''} onClick={() => setMode('hex')}>Hex</button>
          {bin && <button className={mode === 'base64' ? 'btn-on' : ''} onClick={() => setMode('base64')}>Base64</button>}
          {media?.kind === 'image' && <button className={mode === 'image' ? 'btn-on' : ''} onClick={() => setMode('image')}>Image</button>}
          {geo && <button className={mode === 'wkt' ? 'btn-on' : ''} onClick={() => setMode('wkt')}>WKT</button>}
          {drawing && <button className={mode === 'map' ? 'btn-on' : ''} onClick={() => setMode('map')}>Map</button>}
        </div>
        {media?.kind === 'pdf' && <button onClick={openPdf}>Open in viewer</button>}
        {mode === 'text' && isJson && !readOnly && <div role="group" aria-label="JSON" className="flex gap-1">
          <button onClick={() => setText(pretty(text))}>PRETTY</button><button onClick={() => setText(minify(text))}>MINIFY</button></div>}
        {mode === 'text' && <><span className="text-ink-2">View using encoding</span><Select aria-label="Encoding" value={enc} onChange={setEnc} options={ENCODINGS} /></>}
      </div>
      {mode === 'tree'
        ? <div className="min-h-[40vh] max-h-[60vh] overflow-auto bg-canvas border border-rule px-3 py-2.5 text-[12.5px]"><Tree docs={[text]} /></div>
        : mode === 'image' && imgUrl
          ? <div className="min-h-[40vh] max-h-[60vh] overflow-auto bg-canvas border border-rule p-3 grid place-items-center"><img src={imgUrl} alt={`${col} (${media?.mime})`} className="max-w-full max-h-[56vh] object-contain" /></div>
          : mode === 'map' && drawing
            ? <div className="min-h-[40vh] max-h-[60vh] bg-canvas border border-rule p-3 grid place-items-center">
              <svg role="img" aria-label={`Map of ${col}`} viewBox={drawing.viewBox} className="w-full h-[52vh]" preserveAspectRatio="xMidYMid meet">
                {drawing.polys.map((d, i) => <path key={'p' + i} d={d} fillRule="evenodd" fill="color-mix(in srgb, var(--accent) 25%, transparent)" stroke="var(--accent)" strokeWidth={1.5} vectorEffect="non-scaling-stroke" />)}
                {drawing.lines.map((d, i) => <path key={'l' + i} d={d} fill="none" stroke="var(--accent)" strokeWidth={2} vectorEffect="non-scaling-stroke" />)}
                {drawing.points.map(([x, y], i) => <circle key={'c' + i} cx={x} cy={y} r={drawing.r} fill="var(--accent)" />)}
              </svg></div>
            : <textarea className="font-mono w-full min-h-[40vh] max-h-[60vh] resize-y text-[12.5px] leading-[1.45] px-3 py-2.5 bg-canvas" value={shown} readOnly={readOnlyView} autoFocus spellCheck={false}
              onChange={(e) => (mode === 'hex' ? setHex(e.target.value) : setText(e.target.value))}
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && !readOnly) { e.preventDefault(); save() }
                // shortcut: Tab always moves to the neighbouring cell, so a literal tab is typed by pasting it
                else if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey && !readOnlyView) { e.preventDefault(); save(e.shiftKey ? -1 : 1) }
              }} />}
      {mode === 'hex' && !hexBytes && <div className="text-del">Hex needs pairs of 0-9 a-f.</div>}
      <div className="flex justify-end gap-2 mt-[18px] flex-wrap">
        <button onClick={saveFile}>Save to file…</button>
        <button className="mr-auto" onClick={() => openInWindow(col, mode === 'tree' ? text : shown)} title="Show this value in a window of its own">Open in window</button>
        {!readOnly && <label className="inline-flex"><input type="file" className="sr-only peer" aria-label="Load from file" onChange={(e) => { load(e.target.files?.[0]); e.target.value = '' }} /><span className="border border-rule-2 rounded-[2px] px-[11px] py-1 leading-5 cursor-pointer hover:bg-raised peer-focus-visible:outline">Load from file…</span></label>}
        {!readOnly && <>
          <span className="flex-1 min-w-0 text-left text-ink-2">{mode === 'hex' ? `${hexBytes?.length ?? 0} bytes` : `${text.length} characters`}</span>
          <button onClick={() => onDone({ $: 'null' })}>Set NULL</button>
          <button onClick={() => onDone({ $: 'default' })}>Set DEFAULT</button>
          <button onClick={() => onDone({ $: 'empty' })}>Set empty</button>
        </>}
        <button onClick={() => onDone(undefined)}>{readOnly ? 'Close' : 'Cancel'}</button>
        {!readOnly && <button className="btn-primary" onClick={() => save()} title="Ctrl+Enter, Tab saves and moves to the next cell">Save</button>}
      </div>
    </Dialog>
  )
}
