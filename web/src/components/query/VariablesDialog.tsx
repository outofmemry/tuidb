// Query variables: values for the parameters of a query before it runs.
import { useState } from 'react'
import { Dialog } from '../shell/Dialogs'
import { Select } from '../shell/Select'
import { ACTIONS, ROW, msg } from '../shell/ui'

export const PARAM_FORMATS: [string, string][] = [['colon', ':name'], ['dollar', '$name'], ['at', '@name'], ['brace', '${name}'], ['mustache', '{{name}}'], ['question', '? (by position)']]

// format picker: overrides [editor] param_format for this run; changing it asks the server which parameters the text has in that spelling.
export function VariablesDialog({ names: first, init, format: first_fmt, onFormat, onRun, onClose }: { names: string[]; init: Record<string, string>; format: string; onFormat: (f: string) => Promise<string[]>; onRun: (v: Record<string, string>, format: string) => void; onClose: () => void }) {
  const [names, setNames] = useState(first), [format, setFormat] = useState(first_fmt), [err, setErr] = useState('')
  const [vals, setVals] = useState<Record<string, string>>(Object.fromEntries(first.map((n) => [n, init[n] ?? ''])))
  const pick = async (f: string) => {
    setFormat(f); setErr('')
    try { const n = await onFormat(f); setNames(n); setVals((v) => Object.fromEntries(n.map((x) => [x, v[x] ?? init[x] ?? '']))) } catch (e) { setErr(msg(e)) }
  }
  return (
    <Dialog title="Query parameters" onClose={onClose}>
      <form onSubmit={(e) => { e.preventDefault(); onRun(vals, format) }}>
        <div className={ROW}><label className="w-32 text-ink-2">Format</label><Select aria-label="Parameter format" value={format} onChange={pick} options={PARAM_FORMATS.map(([value, label]) => ({ value, label }))} /></div>
        {names.length === 0 && <div className="text-ink-2 text-[12.5px]">The text has no parameters in this format.</div>}
        {names.map((n, i) => (
          <div key={n} className={ROW}><label className="w-32 text-ink-2 truncate" title={n}>{n}</label><input autoFocus={i === 0} className="flex-1 min-w-0" aria-label={n} value={vals[n] ?? ''} onChange={(e) => setVals({ ...vals, [n]: e.target.value })} /></div>
        ))}
        <div className="text-ink-2 text-[12px]">Numbers and NULL are used as they are; anything else is quoted.</div>
        {err && <div className="text-del mt-1">{err}</div>}
        <div className={ACTIONS}><button type="button" onClick={onClose}>Cancel</button><button type="submit" className="btn-primary" disabled={names.length === 0}>Run</button></div>
      </form>
    </Dialog>
  )
}
