// M15.30: the settings page. A form over GET /settings/schema grouped by section, and a keymap editor that writes [keys].
// The server validates (PUT /settings answers with the message of the first bad value, PUT /settings/keys answers 409 with the conflict).
import { useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ApiError, api, type Meta } from '../utils/api'
import { Dialog } from './Dialogs'
import { Select } from './Select'
import { copyText } from '../utils/copy'

// The Go struct has no json tags yet, so both spellings are read.
type Field = { Key: string; Type: string; Enum?: string[]; Doc?: string; Value: string; Section: string; ReadOnly?: boolean }
const norm = (f: any): Field => ({ Key: f.Key ?? f.key, Type: f.Type ?? f.type, Enum: f.Enum ?? f.enum, Doc: f.Doc ?? f.doc, Value: String(f.Value ?? f.value ?? ''), Section: f.Section ?? f.section ?? '', ReadOnly: !!(f.readOnly ?? f.ReadOnly) })
const KEYMAP = '\x00keymap'
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))
const ACTIONS = 'flex justify-end gap-2 mt-[18px]'

// keyText / parseKeys: one input per action, keys separated by spaces, "-n" before a key that needs no prefix (the [keys] spelling).
export const keyText = (bs: { Key: string; NoPrefix: boolean }[]) => bs.map((b) => (b.NoPrefix ? '-n ' : '') + b.Key).join(' ')
export function parseKeys(t: string): string[] {
  const out: string[] = [], tok = t.trim().split(/\s+/).filter(Boolean)
  for (let i = 0; i < tok.length; i++) out.push(tok[i] === '-n' && i + 1 < tok.length ? `-n ${tok[++i]}` : tok[i])
  return out
}

export function SettingsDialog({ onClose, notify }: { onClose: () => void; notify: (m: string) => void }) {
  const qc = useQueryClient()
  const q = useQuery({ queryKey: ['settings-schema'], queryFn: () => api<{ fields: any[]; path?: string }>('GET', '/settings/schema'), gcTime: 0 })
  const meta = qc.getQueryData<Meta>(['meta'])
  const fields = useMemo(() => (q.data?.fields ?? []).map(norm), [q.data])
  const sections = useMemo(() => [...new Set(fields.map((f) => f.Section))], [fields])
  const [sec, setSec] = useState('')
  const [edits, setEdits] = useState<Record<string, string>>({})
  const [err, setErr] = useState<{ key?: string; text: string } | null>(null)
  const [restart, setRestart] = useState(false)
  const [busy, setBusy] = useState(false)
  const cur = sec || sections[0] || ''
  const val = (f: Field) => edits[f.Key] ?? f.Value
  const changed = fields.filter((f) => !f.ReadOnly && edits[f.Key] !== undefined && edits[f.Key] !== f.Value)
  const save = async () => {
    setBusy(true); setErr(null)
    try {
      const r = await api<{ restart?: boolean; restartRequired?: boolean; warning?: string }>('PUT', '/settings', { values: Object.fromEntries(changed.map((f) => [f.Key, edits[f.Key]])) })
      setEdits({}); await q.refetch(); qc.invalidateQueries({ queryKey: ['meta'] })
      // the theme is applied to the page at once; other settings may need the restart the server asks for
      if ((r?.restart || r?.restartRequired) && changed.some((f) => !f.Key.startsWith('theme.'))) setRestart(true); else notify('Settings saved')
      if (r?.warning) notify(r.warning)
    } catch (e) {
      const text = msg(e), body = e instanceof ApiError ? e.body : {}
      const key = body?.field ?? body?.key ?? fields.map((f) => f.Key).filter((k) => text.includes(k)).sort((a, b) => b.length - a.length)[0]
      setErr({ key, text })
      const f = fields.find((x) => x.Key === key); if (f) setSec(f.Section)
    } finally { setBusy(false) }
  }
  return (
    <Dialog title="Settings" onClose={onClose} wide>
      {q.isLoading && <div className="text-ink-2">Loading…</div>}
      {q.error && <div className="text-del" role="alert">{q.error.message}</div>}
      {q.data && <div className="grid grid-cols-[170px_1fr] gap-x-5 max-[760px]:grid-cols-1">
        <nav className="flex flex-col gap-0.5 font-mono text-[12.5px] max-[760px]:flex-row max-[760px]:flex-wrap max-[760px]:mb-3" aria-label="Settings sections">
          {[...sections, KEYMAP].map((s) => <button key={s} className={`menu-item ${s === (cur === '' ? KEYMAP : cur) ? 'btn-on' : ''}`} aria-current={s === cur || undefined} onClick={() => setSec(s)}>{s === KEYMAP ? 'Keymap' : s || 'General'}</button>)}
        </nav>
        <div className="min-w-0">
          {cur === KEYMAP ? <KeymapEditor meta={meta} notify={notify} /> : <>
            {restart && <p className="mt-0 text-edit" role="status">Saved. Restart webtuidb for these to apply.</p>}
            {err && <p className="mt-0 text-del" role="alert">{err.text}</p>}
            <div className="grid grid-cols-[minmax(0,13em)_1fr] gap-x-3 gap-y-2.5 items-start max-[760px]:grid-cols-1 max-[760px]:gap-y-1">
              {fields.filter((f) => f.Section === cur).map((f) => (
                <FieldRow key={f.Key} f={f} value={val(f)} bad={err?.key === f.Key ? err.text : ''} onChange={(v) => { setEdits({ ...edits, [f.Key]: v }); if (err?.key === f.Key) setErr(null) }} />
              ))}
            </div>
          </>}
        </div>
      </div>}
      <div className={ACTIONS}>
        {q.data?.path && <button className="mr-auto btn-ghost" onClick={() => copyText(q.data!.path!).then(() => notify('Path copied'), (e) => notify(msg(e)))} title={q.data.path}>Copy config path</button>}
        <button onClick={onClose}>Close</button>
        {cur !== KEYMAP && <button className="btn-primary" disabled={!changed.length || busy} onClick={save}>{busy ? 'Saving…' : `Save${changed.length ? ` ${changed.length}` : ''}`}</button>}
      </div>
    </Dialog>
  )
}

function FieldRow({ f, value, bad, onChange }: { f: Field; value: string; bad: string; onChange: (v: string) => void }) {
  const id = 'set-' + f.Key
  return (
    <>
      <label htmlFor={id} className="text-ink-2 font-mono text-[12px] pt-1 wrap-anywhere">{f.Key.split('.').slice(1).join('.') || f.Key}</label>
      <div className="min-w-0">
        {f.ReadOnly ? <input id={id} className="w-full font-mono" type="text" value={value} readOnly aria-readonly title="Set in the config file; the web UI does not write paths" />
          : f.Type === 'bool' ? <input id={id} type="checkbox" checked={value === 'true'} onChange={(e) => onChange(e.target.checked ? 'true' : 'false')} aria-invalid={!!bad} />
          : f.Type === 'enum' ? <Select id={id} className="w-full" aria-label={f.Key} value={value} onChange={onChange} options={f.Enum ?? []} />
            : <input id={id} className="w-full font-mono" type={f.Type === 'int' ? 'number' : 'text'} value={value} placeholder={f.Type === 'list' ? 'comma separated' : undefined} aria-invalid={!!bad} aria-describedby={bad ? id + '-e' : undefined} onChange={(e) => onChange(e.target.value)} />}
        {bad && <div id={id + '-e'} className="text-del text-[12px] mt-0.5">{bad}</div>}
        {f.Doc && <div className="text-ink-3 text-[11.5px] mt-0.5 font-sans">{f.Doc}</div>}
      </div>
    </>
  )
}

// Each action with its keys; Enter or Save writes [keys] for that action and a conflict (409) is shown beside it.
function KeymapEditor({ meta, notify }: { meta?: Meta; notify: (m: string) => void }) {
  const qc = useQueryClient()
  const bs = meta?.keymap.bindings ?? []
  const actions = useMemo(() => [...new Set(bs.map((b) => b.Action))], [bs])
  const [q, setQ] = useState(''), [edits, setEdits] = useState<Record<string, string>>({}), [errs, setErrs] = useState<Record<string, string>>({})
  const keysOf = (a: string) => keyText(bs.filter((b) => b.Action === a))
  const t = q.trim().toLowerCase()
  const shown = actions.filter((a) => !t || a.includes(t.replace(/\s+/g, '_')) || (bs.find((b) => b.Action === a)?.Desc ?? '').toLowerCase().includes(t))
  const save = async (a: string) => {
    const keys = parseKeys(edits[a] ?? keysOf(a))
    if (!keys.length) return setErrs({ ...errs, [a]: 'Give the action at least one key.' })
    try {
      const r = await api<{ warning?: string } | undefined>('PUT', '/settings/keys', { keys: { [a]: keys } })
      setEdits(({ [a]: _, ...rest }) => rest); setErrs(({ [a]: _, ...rest }) => rest)
      await qc.invalidateQueries({ queryKey: ['meta'] }); notify(r?.warning ? `Key saved. ${r.warning}` : 'Key saved')
    } catch (e) { setErrs((cur) => ({ ...cur, [a]: msg(e) })) } // 409: the conflict message from keymap.Build
  }
  return (
    <div>
      <p className="mt-0 text-ink-2 text-[12px]">Keys use the config spelling: <kbd>alt+q</kbd>, <kbd>ctrl+d</kbd>, <kbd>P</kbd>; put <kbd>-n</kbd> before a key that needs no prefix. A key already taken is refused.</p>
      <input className="w-full mb-2" aria-label="Search the actions" placeholder="Search by action or description…" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="max-h-[52vh] overflow-auto">
        {shown.map((a) => (
          <div key={a} className="py-1.5 border-b border-rule">
            <div className="flex items-center gap-2 max-[760px]:flex-wrap">
              <span className="flex-1 min-w-[12em] text-[12.5px]">{bs.find((b) => b.Action === a)?.Desc}<span className="block font-mono text-ink-3 text-[11px]">{a}</span></span>
              <input className="w-[16em] font-mono text-[12px]" aria-label={`Keys for ${a}`} value={edits[a] ?? keysOf(a)} onChange={(e) => setEdits({ ...edits, [a]: e.target.value })} onKeyDown={(e) => { if (e.key === 'Enter') save(a) }} />
              <button disabled={edits[a] === undefined || edits[a] === keysOf(a)} onClick={() => save(a)}>Save</button>
            </div>
            {errs[a] && <div className="text-del text-[12px] mt-0.5" role="alert">{errs[a]}</div>}
          </div>
        ))}
      </div>
    </div>
  )
}
