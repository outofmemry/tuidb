import { useEffect, useRef, useState, type CSSProperties } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { api, type Connection, type Meta } from './api'
import { Dialog } from './Dialogs'
import { LABELS, Logo } from './logos'

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
    onError: (e) => setMsg(e.message),
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

  if (conns.error) return <div className="start"><header><h1>tuidb</h1></header><p className="lede err">{conns.error.message}</p><footer>Open the URL printed by <code>tuidb --frontend</code>. It carries the access token.</footer></div>
  return (
    <div className="start">
      <header><h1 className="grow">Connections</h1><button className="primary" onClick={() => setEdit({ c: blank(), idx: -1 })}>Add connection</button></header>
      <p className="lede">Pick a database to browse and edit. Each connection's safe mode decides what needs a confirmation or a password before it is sent.</p>
      <form className="paste" onSubmit={(e) => { e.preventDefault(); fromUrl() }}>
        <input aria-label="Connection URL" placeholder="Paste a connection URL" value={url} onChange={(e) => setUrl(e.target.value)} autoComplete="off" spellCheck={false} />
        <button type="submit" disabled={!url.trim()}>Continue</button>
      </form>
      {msg && <p className="err" role="alert">{msg}</p>}
      <div className="engines" aria-label="Supported engines">
        {(meta.data?.drivers ?? []).map((d) => <button key={d} className="engine" title={`New ${LABELS[d] ?? d} connection`} onClick={() => setEdit({ c: blank(d), idx: -1 })}><Logo driver={d} size={18} /><span>{LABELS[d] ?? d}</span></button>)}
      </div>
      {conns.isLoading && <p className="empty">Loading connections…</p>}
      {list.length === 0 && !conns.isLoading && <p className="empty">No connections yet. Paste a URL above, or pick an engine.</p>}
      <div className="conns">
        {groups.map((g) => (
          <div key={g}>
            {g && <div className="group-title">{g}</div>}
            {list.map((c, idx) => (c.Group ?? '') === g && (
              <div className="conn" key={c.Name} style={{ '--c': c.Color || undefined } as CSSProperties}>
                <div className="sw" aria-hidden />
                <Logo driver={c.Driver} size={28} />
                <div className="grow"><span className="nm">{c.Name}</span>{c.Tag && <span className="tag">{c.Tag}</span>}
                  <div className="where"><b>{LABELS[c.Driver] ?? c.Driver}</b>{FILE_BASED.includes(c.Driver) ? c.Database : `${c.Host}${c.Port ? ':' + c.Port : ''}${c.Database ? '/' + c.Database : ''}`}</div></div>
                <div className="acts">
                  <button className="ghost" onClick={() => setEdit({ c: structuredClone(c), idx })}>Edit</button>
                  <button className="ghost danger" onClick={() => window.confirm(`Delete ${c.Name}?`) && save.mutate(list.filter((_, i) => i !== idx))}>Delete</button>
                  <button className="primary" disabled={connect.isPending} onClick={() => { setMsg(''); connect.mutate(c.Name) }}>Connect</button>
                </div>
              </div>
            ))}
          </div>
        ))}
      </div>
      {edit && meta.data && <ConnForm meta={meta.data} init={edit.c} onClose={() => setEdit(null)} onSave={(c) => upsert(c, edit.idx)} />}
    </div>
  )
}

function ConnForm({ meta, init, onSave, onClose }: { meta: Meta; init: Connection; onSave: (c: Connection) => void; onClose: () => void }) {
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
  const parseUrl = async () => {
    if (!dirty.current) return
    if (!url.trim()) { dirty.current = false; return }
    try {
      const p = await api<Connection>('POST', '/connections/parse', { url })
      dirty.current = false
      setUrlErr('')
      setC((x) => ({ ...x, Driver: p.Driver, Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database, SSLMode: p.SSLMode, Options: p.Options,
        Name: x.Name || p.Name, Tag: p.Tag || x.Tag, Color: p.Color || x.Color }))
    } catch (e) { setUrlErr(`That URL isn't valid: ${e instanceof Error ? e.message : e}`) }
  }
  const set = <K extends keyof Connection>(k: K, v: Connection[K]) => setC((x) => ({ ...x, [k]: v }))
  const file = FILE_BASED.includes(c.Driver)
  const clean = (): Connection => ({ ...c, Port: c.Port ? Number(c.Port) : undefined, SSH: c.SSH?.Host ? c.SSH : null })
  const field = (label: string, k: keyof Connection, type = 'text') => (
    <>
      <label>{label}</label>
      <input type={type} value={(c[k] as string | number | undefined) ?? ''} onChange={(e) => set(k, (type === 'number' ? Number(e.target.value) : e.target.value) as never)} />
    </>
  )
  return (
    <Dialog title={init.Name ? `Edit ${init.Name}` : 'New connection'} onClose={onClose}>
      <div className="form">
        <label>Connection URL</label>
        <div className="urlrow">
          <input className="mono" type={showUrl ? 'text' : 'password'} autoComplete="off" spellCheck={false} placeholder="postgresql://user:password@host:5432/database" value={url}
            onChange={(e) => { dirty.current = true; setUrl(e.target.value) }} onBlur={parseUrl} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); parseUrl() } }} />
          <button type="button" onClick={() => setShowUrl(!showUrl)}>{showUrl ? 'Hide' : 'Show'}</button>
        </div>
        {urlErr && <p className="err" style={{ gridColumn: '1 / -1', margin: 0 }}>{urlErr}</p>}
        {field('Name', 'Name')}
        <label>Driver</label>
        <select value={c.Driver} onChange={(e) => set('Driver', e.target.value)}>{meta.drivers.map((d) => <option key={d} value={d}>{LABELS[d] ?? d}</option>)}</select>
        {file ? field('File path', 'Database') : <>
          {field('Host', 'Host')}{field('Port', 'Port', 'number')}{field('User', 'User')}{field('Password', 'Password', 'password')}
          {field('Password command', 'PasswordCmd')}{field('Database', 'Database')}
          <label>SSL mode</label>
          <select value={c.SSLMode ?? ''} onChange={(e) => set('SSLMode', e.target.value)}><option value="">default</option>{meta.sslModes.map((m) => <option key={m}>{m}</option>)}</select>
        </>}
        {field('Group', 'Group')}{field('Tag', 'Tag')}{field('Color', 'Color')}
        <label>Safe mode</label>
        <select value={c.SafeMode ?? ''} onChange={(e) => set('SafeMode', e.target.value)}><option value="">default</option>{meta.safeModes.map((m) => <option key={m}>{m}</option>)}</select>
        {!file && <>
          <label>SSH tunnel host</label>
          <input value={c.SSH?.Host ?? ''} onChange={(e) => set('SSH', { Host: e.target.value, Port: c.SSH?.Port ?? 22, User: c.SSH?.User ?? '', Key: c.SSH?.Key ?? '', Password: c.SSH?.Password ?? '' })} />
          {c.SSH?.Host && <>
            <label>SSH user</label><input value={c.SSH.User} onChange={(e) => set('SSH', { ...c.SSH!, User: e.target.value })} />
            <label>SSH key file</label><input value={c.SSH.Key} onChange={(e) => set('SSH', { ...c.SSH!, Key: e.target.value })} />
          </>}
        </>}
      </div>
      <div className="actions">
        {test && <span className={`grow ${test.startsWith('Connected') || test.startsWith('Testing') ? 'dim' : 'err'}`}>{test}</span>}
        <button onClick={async () => { setTest('Testing…'); try { const r = await api<{ serverInfo: string }>('POST', '/connections/test', clean()); setTest('Connected: ' + r.serverInfo) } catch (e) { setTest(String(e instanceof Error ? e.message : e)) } }}>Test</button>
        <button onClick={onClose}>Cancel</button>
        <button className="primary" disabled={!c.Name} onClick={() => onSave(clean())}>Save</button>
      </div>
    </Dialog>
  )
}
