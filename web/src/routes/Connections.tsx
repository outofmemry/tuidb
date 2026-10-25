import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, type Connection, type Meta } from '../utils/api'
import { Dialog, confirm } from '../components/Dialogs'
import { Select } from '../components/Select'
import { LABELS, Logo } from '../components/logos'

const blank = (driver = 'postgres'): Connection => ({ Name: '', Driver: driver, Host: '127.0.0.1', Database: '', SafeMode: '' })
const FILE_BASED = ['sqlite', 'duckdb']

export function Connections() {
  const qc = useQueryClient()
  const nav = useNavigate()
  const conns = useQuery({ queryKey: ['conns'], queryFn: () => api<Connection[]>('GET', '/connections') })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  const [edit, setEdit] = useState<{ c: Connection; idx: number } | null>(null)
  const [msg, setMsg] = useState('')
  const [url, setUrl] = useState('')

  const save = useMutation({
    mutationFn: (list: Connection[]) => api('PUT', '/connections', list),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['conns'] }); setEdit(null) },
  })
  const connect = useMutation({
    mutationFn: (name: string) => api<{ id: string }>('POST', '/sessions', { Name: name }),
    onSuccess: (r) => nav({ to: '/s/$sessionId', params: { sessionId: r.id } }),
    onError: (e) => setMsg(e.message),
  })

  const list = conns.data ?? []
  const groups = [...new Set(list.map((c) => c.Group ?? ''))].sort()
  const upsert = (c: Connection, idx: number) => save.mutate(idx < 0 ? [...list, c] : list.map((x, i) => (i === idx ? c : x)))
  // Paste a URL on the start page: the server parses it and the form opens filled in.
  const fromUrl = async () => {
    if (!url.trim()) return
    try { setEdit({ c: { ...blank(), ...(await api<Connection>('POST', '/connections/parse', { url })) }, idx: -1 }); setUrl(''); setMsg('') } catch (e) { setMsg(`That URL isn't valid: ${e instanceof Error ? e.message : e}`) }
  }

  if (conns.error) return <div className="min-h-full bg-[radial-gradient(var(--rule)_1px,transparent_1px)] bg-[size:22px_22px]"><div className="max-w-[920px] mx-auto pt-10 px-7 pb-20 max-[760px]:pt-7 max-[760px]:px-4 max-[760px]:pb-12"><header className="flex items-baseline gap-4"><h1 className="m-0 text-[36px] leading-none tracking-[-.03em] font-medium font-mono max-[760px]:text-[32px] after:content-[''] after:inline-block after:w-[.5em] after:h-[.85em] after:ml-[.12em] after:align-[-.08em] after:bg-accent after:animate-blink motion-reduce:after:animate-none">webtuidb</h1></header><p className="mt-[10px] mb-0 max-w-[56ch] text-[15px] text-del">{conns.error.message}</p><footer className="mt-16 text-ink-3 text-[12px]">Open the URL printed by <code>webtuidb --frontend</code>. It carries the access token.</footer></div></div>
  return (
    <div className="min-h-full bg-[radial-gradient(var(--rule)_1px,transparent_1px)] bg-[size:22px_22px]"><div className="max-w-[920px] mx-auto pt-10 px-7 pb-20 max-[760px]:pt-7 max-[760px]:px-4 max-[760px]:pb-12">
      <header className="flex items-baseline gap-4"><h1 className="flex-1 min-w-0 m-0 text-[36px] leading-none tracking-[-.03em] font-medium font-mono max-[760px]:text-[32px] after:content-[''] after:inline-block after:w-[.5em] after:h-[.85em] after:ml-[.12em] after:align-[-.08em] after:bg-accent after:animate-blink motion-reduce:after:animate-none">Connections</h1><button className="btn-primary" onClick={() => setEdit({ c: blank(), idx: -1 })}>Add connection</button></header>
      <p className="mt-[10px] mb-0 max-w-[56ch] text-[15px] text-ink-2">Pick a database to browse and edit. Each connection's safe mode decides what needs a confirmation or a password before it is sent.</p>
      <form className="flex items-center gap-[10px] mt-6 pb-2 border-b border-accent before:content-['>'] before:text-accent before:[font:500_16px_var(--font-mono)]" onSubmit={(e) => { e.preventDefault(); fromUrl() }}>
        <input className="flex-1 min-w-0 bg-transparent border-0 px-0 py-1.5 text-[16px] leading-[1.3] focus-visible:outline-0" aria-label="Connection URL" placeholder="Paste a connection URL" value={url} onChange={(e) => setUrl(e.target.value)} autoComplete="off" spellCheck={false} />
        <button type="submit" disabled={!url.trim()}>Continue</button>
      </form>
      {msg && <p className="my-[1em] text-del" role="alert">{msg}</p>}
      <div className="flex flex-wrap gap-1 mt-[10px]" aria-label="Supported engines">
        {(meta.data?.drivers ?? []).map((d) => <button key={d} className="flex items-center gap-[7px] py-1 pr-[10px] pl-[7px] bg-transparent text-ink-2 text-[12px] hover:text-ink hover:bg-raised" title={`New ${LABELS[d] ?? d} connection`} onClick={() => setEdit({ c: blank(d), idx: -1 })}><Logo driver={d} size={18} /><span>{LABELS[d] ?? d}</span></button>)}
      </div>
      {conns.isLoading && <p className="mt-7 mb-0 p-0 text-ink-2 text-[15px] max-w-[48ch]">Loading connections…</p>}
      {list.length === 0 && !conns.isLoading && <p className="mt-7 mb-0 p-0 text-ink-2 text-[15px] max-w-[48ch]">No connections yet. Paste a URL above, or pick an engine.</p>}
      <div className="mt-7">
        {groups.map((g) => (
          <div key={g}>
            {g && <div className="mt-7 pb-1.5 text-[12px] text-ink-2 border-b border-rule font-mono">{g}</div>}
            {list.map((c, idx) => (c.Group ?? '') === g && (
              <div className="group grid grid-cols-[8px_28px_1fr_auto] gap-x-4 items-center p-3 -mx-3 border-b border-rule first:border-t hover:bg-panel max-[760px]:grid-cols-[8px_28px_1fr] max-[760px]:px-2 max-[760px]:-mx-2" key={c.Name} style={{ '--c': c.Color || undefined } as CSSProperties}>
                <div className="w-2 h-10 rounded-[1px] bg-[var(--c,var(--raised))] max-[760px]:h-full" aria-hidden />
                <Logo driver={c.Driver} size={28} className="text-ink-2 group-hover:text-ink" />
                <div className="flex-1 min-w-0"><span className="text-[20px] leading-[1.15] font-medium tracking-[-.015em]">{c.Name}</span>{c.Tag && <span className="ml-[10px] align-[3px] px-1.5 rounded-[2px] border border-rule-2 text-[11.5px] font-medium text-ink-2 font-mono">{c.Tag}</span>}
                  <div className="mt-0.5 font-mono text-[12px] leading-normal text-ink-2 [overflow-wrap:anywhere]"><b className="font-normal text-ink mr-3">{LABELS[c.Driver] ?? c.Driver}</b>{FILE_BASED.includes(c.Driver) ? c.Database : `${c.Host}${c.Port ? ':' + c.Port : ''}${c.Database ? '/' + c.Database : ''}`}</div></div>
                <div className="flex gap-1.5 max-[760px]:col-[2/-1] max-[760px]:mt-[10px]">
                  <button className="btn-ghost opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 max-[760px]:opacity-100" onClick={() => setEdit({ c: structuredClone(c), idx })}>Edit</button>
                  <button className="btn-ghost btn-danger opacity-0 group-hover:opacity-100 group-focus-within:opacity-100 max-[760px]:opacity-100" onClick={async () => (await confirm(`Delete ${c.Name}?`)) && save.mutate(list.filter((_, i) => i !== idx))}>Delete</button>
                  <button className="btn-primary" disabled={connect.isPending} onClick={() => { setMsg(''); connect.mutate(c.Name) }}>Connect</button>
                </div>
              </div>
            ))}
          </div>
        ))}
      </div>
      {edit && meta.data && <ConnForm meta={meta.data} init={edit.c} error={save.error?.message} onClose={() => { save.reset(); setEdit(null) }} onSave={(c) => upsert(c, edit.idx)} />}
    </div></div>
  )
}

function ConnForm({ meta, init, error, onSave, onClose }: { meta: Meta; init: Connection; error?: string; onSave: (c: Connection) => void; onClose: () => void }) {
  const [c, setC] = useState<Connection>(init)
  const [test, setTest] = useState('')
  const [url, setUrl] = useState('')
  const [urlErr, setUrlErr] = useState('')
  const [showUrl, setShowUrl] = useState(false)
  const dirty = useRef(false) // the URL box holds unparsed typing; don't overwrite it
  // Keep the URL in step with the fields (debounced); the server formats it so it matches the terminal UI.
  useEffect(() => {
    if (dirty.current) return
    const t = setTimeout(() => {
      api<{ url: string }>('POST', '/connections/format', { ...c, Port: c.Port ? Number(c.Port) : undefined }).then((r) => { if (!dirty.current) setUrl(r.url) }, () => {})
    }, 300)
    return () => clearTimeout(t)
  }, [c])
  // returns the connection with the URL applied, or undefined when the URL does not parse
  const parseUrl = async (): Promise<Connection | undefined> => {
    if (!dirty.current) return c
    if (!url.trim()) { dirty.current = false; return c }
    try {
      const p = await api<Connection>('POST', '/connections/parse', { url })
      dirty.current = false
      setUrlErr('')
      const merge = (x: Connection): Connection => ({ ...x, Driver: p.Driver, Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database, SSLMode: p.SSLMode, Options: p.Options,
        Name: x.Name || p.Name, Tag: p.Tag || x.Tag, Color: p.Color || x.Color })
      setC(merge)
      return merge(c)
    } catch (e) { setUrlErr(`That URL isn't valid: ${e instanceof Error ? e.message : e}`) }
  }
  const set = <K extends keyof Connection>(k: K, v: Connection[K]) => setC((x) => ({ ...x, [k]: v }))
  const file = FILE_BASED.includes(c.Driver)
  const clean = (x = c): Connection => ({ ...x, Port: x.Port ? Number(x.Port) : undefined, SSH: x.SSH?.Host ? x.SSH : null })
  const field = (label: string, k: keyof Connection, type = 'text') => (
    <>
      <label>{label}</label>
      <input type={type} value={(c[k] as string | number | undefined) ?? ''} onChange={(e) => set(k, (type === 'number' ? Number(e.target.value) : e.target.value) as never)} />
    </>
  )
  return (
    <Dialog title={init.Name ? `Edit ${init.Name}` : 'New connection'} onClose={onClose}>
      <div className="grid grid-cols-[150px_1fr] gap-x-[14px] gap-y-2 items-center [&_label]:text-ink-2 [&_label]:text-right max-[760px]:grid-cols-1 max-[760px]:gap-[3px] max-[760px]:[&_label]:mt-1.5 max-[760px]:[&_label]:text-left">
        <label>Connection URL</label>
        <div className="flex gap-1.5">
          <input className="font-mono flex-1 min-w-0" type={showUrl ? 'text' : 'password'} autoComplete="off" spellCheck={false} placeholder="postgresql://user:password@host:5432/database" value={url}
            onChange={(e) => { dirty.current = true; setUrl(e.target.value) }} onBlur={() => { parseUrl() }} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); parseUrl() } }} />
          <button type="button" onClick={() => setShowUrl(!showUrl)}>{showUrl ? 'Hide' : 'Show'}</button>
        </div>
        {urlErr && <p className="text-del" style={{ gridColumn: '1 / -1', margin: 0 }}>{urlErr}</p>}
        {field('Name', 'Name')}
        <label>Driver</label>
        <Select className="w-full" aria-label="Driver" value={c.Driver} onChange={(v) => set('Driver', v)} options={meta.drivers.map((d) => ({ value: d, label: LABELS[d] ?? d }))} />
        {file ? field('File path', 'Database') : <>
          {field('Host', 'Host')}{field('Port', 'Port', 'number')}{field('User', 'User')}{field('Password', 'Password', 'password')}
          {field('Password command', 'PasswordCmd')}{field('Database', 'Database')}
          <label>SSL mode</label>
          <Select className="w-full" aria-label="SSL mode" value={c.SSLMode ?? ''} onChange={(v) => set('SSLMode', v)} options={[{ value: '', label: 'default' }, ...meta.sslModes]} />
        </>}
        {field('Group', 'Group')}{field('Tag', 'Tag')}{field('Color', 'Color')}
        <label>Safe mode</label>
        <Select className="w-full" aria-label="Safe mode" value={c.SafeMode ?? ''} onChange={(v) => set('SafeMode', v)} options={[{ value: '', label: 'default' }, ...meta.safeModes]} />
        {!file && <>
          <label>SSH tunnel host</label>
          <input value={c.SSH?.Host ?? ''} onChange={(e) => set('SSH', { Host: e.target.value, Port: c.SSH?.Port ?? 22, User: c.SSH?.User ?? '', Key: c.SSH?.Key ?? '', Password: c.SSH?.Password ?? '' })} />
          {c.SSH?.Host && <>
            <label>SSH user</label><input value={c.SSH.User} onChange={(e) => set('SSH', { ...c.SSH!, User: e.target.value })} />
            <label>SSH key file</label><input value={c.SSH.Key} onChange={(e) => set('SSH', { ...c.SSH!, Key: e.target.value })} />
          </>}
        </>}
      </div>
      {error && <p className="my-[1em] text-del" role="alert">{error}</p>}
      <div className="flex justify-end gap-2 mt-[18px]">
        {test && <span className={`flex-1 min-w-0 text-left ${test.startsWith('Connected') || test.startsWith('Testing') ? 'text-ink-2' : 'text-del'}`}>{test}</span>}
        <button onClick={async () => { setTest('Testing…'); try { const r = await api<{ serverInfo: string }>('POST', '/connections/test', clean()); setTest('Connected: ' + r.serverInfo) } catch (e) { setTest(String(e instanceof Error ? e.message : e)) } }}>Test</button>
        <button onClick={onClose}>Cancel</button>
        <button className="btn-primary" disabled={!c.Name} onClick={async () => { const m = await parseUrl(); if (m) onSave(clean(m)) }}>Save</button>
      </div>
    </Dialog>
  )
}
