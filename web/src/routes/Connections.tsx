import { useEffect, useId, useRef, useState, type CSSProperties } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { ApiError, api, download, etags, saveBlob, signOut, type Connection, type Meta } from '../utils/api'
import { Dialog, ask, askMatch, askSecret, confirm, openSession } from '../components/shell/Dialogs'
import { RecentDialog } from '../components/shell/RecentDialog'
import { TabMenu } from '../components/shell/TabMenu'
import { SettingsDialog } from '../components/admin/SettingsDialog'
import { addViewerFile } from '../components/query/SqlViewer'
import { openRecentFile, openSqlFile } from '../utils/files'
import { useKeymap } from '../hooks/useKeymap'
import { Select } from '../components/shell/Select'
import { LABELS, Logo } from '../components/shell/Logo'
import { lastWorkspaces, markReopened, openSessions, remember, reopenDue } from '../utils/workspaces'
import { ago, recentRank } from '../utils/ago'
import { whyNot } from '../utils/menus'
import { hint } from '../utils/hint'
import { Help } from './Workspace'

// shortcut: the same four presets as config.PasswordCmdPresets, used until GET /api/meta carries passwordCmdPresets
const PRESETS = [
  { name: 'macOS Keychain', command: 'security find-generic-password -s webtuidb -a <name> -w', note: 'add it with: security add-generic-password -s webtuidb -a <name> -w' },
  { name: 'Linux secret-tool', command: 'secret-tool lookup service webtuidb account <name>', note: 'add it with: secret-tool store --label=webtuidb service webtuidb account <name>' },
  { name: 'pass', command: 'pass show webtuidb/<name>', note: 'add it with: pass insert webtuidb/<name>' },
  { name: '1Password', command: 'op read "op://Private/<name>/password"', note: 'needs the op CLI signed in' },
]

const stripLast = ({ lastConnected: _l, ...c }: Connection): Connection => c // the list's lastConnected is output only
const SORTS = [{ value: 'name', label: 'Name' }, { value: 'tag', label: 'Tag' }, { value: 'driver', label: 'Driver' }, { value: 'recent', label: 'Recent' }]
// badUrl turns the parser's complaint about a pasted URL into what to do; other parse errors keep their own text
const badUrl = (e: unknown) => { const m = e instanceof Error ? e.message : String(e); return /scheme/i.test(m) ? "That doesn't look like a database URL. It should start with postgresql://, mysql://, mongodb://, redis://…" : `That URL could not be read: ${m}` }
// colorHex: '' (no colour), a #rgb / #rrggbb code, or a CSS colour name turned into its #rrggbb code (the terminal UI draws hex only); null when it is neither
export function colorHex(v: string): string | null {
  const t = v.trim()
  if (!t || /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.test(t)) return t
  if (!/^[a-z]+$/i.test(t) || /^(transparent|currentcolor|inherit|initial|unset|revert)$/i.test(t)) return null
  const el = document.createElement('i'); el.style.color = t
  if (!el.style.color) return null // the browser did not take it: not a colour name
  document.body.appendChild(el); const m = getComputedStyle(el).color.match(/\d+/g); el.remove()
  return m && m.length >= 3 ? '#' + m.slice(0, 3).map((n) => Number(n).toString(16).padStart(2, '0')).join('') : null
}
const blank = (driver = 'postgres'): Connection => ({ Name: '', Driver: driver, Host: '127.0.0.1', Database: '', SafeMode: '' })

export function Connections() {
  const qc = useQueryClient()
  const nav = useNavigate()
  const conns = useQuery({ queryKey: ['conns'], queryFn: () => api<Connection[]>('GET', '/connections') })
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  const [edit, setEdit] = useState<{ c: Connection; idx: number } | null>(null)
  const [msg, setMsg] = useState('')
  const [url, setUrl] = useState('')
  const [io, setIo] = useState<'' | 'export' | 'import'>('')
  const [dlg, setDlg] = useState<'' | 'recent' | 'settings' | 'sort' | 'help'>('')
  const urlBox = useRef<HTMLInputElement>(null)
  const [drag, setDrag] = useState<string | null>(null) // a connection being dragged to another group
  const [rowMenu, setRowMenu] = useState<{ x: number; y: number; c: Connection; idx: number } | null>(null) // the ⋯ menu of one connection row
  const [fail, setFail] = useState<{ head: string; detail: string; c?: Connection } | null>(null) // a connection that did not open: plain words first, the driver's text behind Technical details
  const open = openSessions() // connections with a workspace open in this browser tab: Connect becomes Resume
  const sortQ = useQuery({ queryKey: ['conn-sort'], queryFn: () => api<{ sort: string; default: string }>('GET', '/state/connection-sort'), gcTime: 0 })
  const sortBy = sortQ.data?.sort || sortQ.data?.default || meta.data?.config.general.ConnectionSort || 'name'
  const fileBased = meta.data?.fileBased ?? []

  // the list's version travels back as If-Match: a stale tab gets a 409, reloads, and asks the user to redo the edit
  const save = useMutation({
    mutationFn: (list: Connection[]) => api('PUT', '/connections', list.map(stripLast), undefined, etags['/connections'] ? { 'If-Match': etags['/connections'] } : undefined),
    onSuccess: () => { qc.invalidateQueries({ queryKey: ['conns'] }); setEdit(null) },
    onError: (e) => { if (e instanceof ApiError && e.status === 409) { qc.invalidateQueries({ queryKey: ['conns'] }); setEdit(null); setMsg('The connection list changed elsewhere. It was reloaded: make your change again.') } },
  })
  const connect = useMutation({
    mutationFn: (name: string) => openSession(name),
    onMutate: (name) => { setMsg(`Connecting to ${name}…`); setFail(null) },
    onSuccess: (r) => nav({ to: '/s/$sessionId', params: { sessionId: r.id } }),
    onError: (e, name) => { setMsg(''); if (e.message === 'Cancelled') return; setFail({ head: `Could not connect to ${name}`, detail: e.message, c: conns.data?.find((c) => c.Name === name) }) },
  })

  const list = conns.data ?? []
  // [general] reopen_workspaces: on the first visit of this browser tab, the workspaces that were open last time come back, each on its database
  useEffect(() => {
    if (!meta.data || !conns.data || !meta.data.config.general.ReopenWorkspaces || !reopenDue()) return
    markReopened()
    const last = Object.entries(lastWorkspaces()).filter(([n]) => conns.data.some((c) => c.Name === n))
    if (!last.length) return
    void (async () => {
      let to = ''
      for (const [name, database] of last) {
        try {
          const r = await openSession(name)
          remember(name, r.id)
          if (database) await api('POST', `/sessions/${r.id}/database`, { Name: database }).catch(() => {})
          to = r.id
        } catch (e) { setFail({ head: `Could not reopen ${name}`, detail: e instanceof Error ? e.message : String(e), c: conns.data?.find((c) => c.Name === name) }) }
      }
      if (to) nav({ to: '/s/$sessionId', params: { sessionId: to } })
    })()
  }, [meta.data, conns.data])
  const viewFile = (f: { name: string; text: string } | null) => { if (f) { addViewerFile(f); nav({ to: '/sql' }) } }
  useKeymap(meta.data?.keymap, ['open_file', 'open_recent', 'settings', 'help'], (a) => { if (a === 'open_file') openSqlFile().then(viewFile); else if (a === 'open_recent') setDlg('recent'); else if (a === 'help') setDlg('help'); else setDlg('settings') })
  // Sort By: the order inside each group; `idx` stays the connection's place in the saved list, which Edit and Delete use
  // tag order is the tag list's, untagged last, like the terminal UI
  const tagRank = (c: Connection) => { const i = (meta.data?.tags ?? []).findIndex((t) => t.name === c.Tag); return String(c.Tag && i >= 0 ? i : 99).padStart(2, '0') }
  const key = (c: Connection) => (sortBy === 'tag' ? tagRank(c) : sortBy === 'driver' ? c.Driver : '')
  const shown = list.map((c, idx) => ({ c, idx })).sort((a, b) => (sortBy === 'recent' ? recentRank(b.c.lastConnected) - recentRank(a.c.lastConnected) : 0) || key(a.c).localeCompare(key(b.c)) || a.c.Name.localeCompare(b.c.Name, undefined, { sensitivity: 'base' }))
  const setSort = (v: string) => api('PUT', '/state/connection-sort', { Sort: v }).then(() => sortQ.refetch(), (e) => setMsg(e.message))
  const inGroup = (c: Connection, g: string) => (c.Group ?? '') === g || (c.Group ?? '').startsWith(g + '/')
  const renameGroup = async (g: string) => {
    const n = (await ask('Rename group', g))?.trim()
    if (!n || n === g) return
    if (list.some((c) => c.Source && inGroup(c, g))) return setMsg('Some connections in this group come from a connection source file. Edit that file to move them.')
    save.mutate(list.map((c) => (inGroup(c, g) ? { ...c, Group: n + (c.Group ?? '').slice(g.length) } : c)))
  }
  const deleteGroup = async (g: string) => {
    const gone = list.filter((c) => inGroup(c, g))
    if (gone.some((c) => c.Source)) return setMsg('Some connections in this group come from a connection source file. Edit that file to remove them.')
    const t = await askMatch(`Type ${g} to delete this group and its ${gone.length} connection${gone.length === 1 ? '' : 's'}`, g)
    if (t === null) return
    if (t !== g) return setMsg('The name did not match; nothing deleted.')
    save.mutate(list.filter((c) => !inGroup(c, g)))
  }
  // Duplicate: a local, editable copy ("name copy", "name copy2" when taken), as the TUI's c
  const duplicate = (c: Connection) => {
    let name = c.Name + ' copy'
    while (list.some((o) => o.Name === name)) name += '2'
    save.mutate([...list, { ...structuredClone(c), Name: name, Source: undefined }])
  }
  // Copy as URL: without the password unless asked for (clipboards are shared and synced), as the TUI's u / U
  const copyUrl = async (c: Connection, withPassword: boolean) => {
    if (withPassword && !(await confirm('Copy the URL with the password to the clipboard? Clipboard managers and synced clipboards keep what is copied.', undefined, 'Copy'))) return
    try {
      const r = await api<{ url: string }>('POST', '/connections/format', { ...c, Password: withPassword ? c.Password : '' })
      try { await navigator.clipboard.writeText(r.url) } catch { return setMsg('Could not copy to the clipboard. Select the text and copy it yourself.') }
      setMsg(`Copied ${c.Name} as URL${withPassword ? ' with its password' : ' without the password'}.`)
    } catch (e) { setMsg(`Could not copy: ${e instanceof Error ? e.message : e}`) }
  }
  const testConn = async (c: Connection) => {
    setFail(null); setMsg(`Testing ${c.Name}…`)
    const go = (askedPassword?: string) => api<{ serverInfo: string }>('POST', '/connections/test', { ...stripLast(c), Port: c.Port ? Number(c.Port) : undefined, askedPassword })
    try {
      let r
      try { r = await go() } catch (e) {
        if (!(e instanceof ApiError && e.status === 422 && e.body.passwordRequired)) throw e
        const pw = await askSecret(`Password for ${c.Name}`)
        if (pw === null) return setMsg('')
        r = await go(pw)
      }
      setMsg(`Connected to ${c.Name}: ${r.serverInfo}`)
    } catch (e) { setMsg(''); setFail({ head: `Could not connect to ${c.Name}`, detail: e instanceof Error ? e.message : String(e), c }) }
  }
  // the connection menu is meta.menus.welcome (labels and order); entries are dispatched by action name, as the terminal does
  const openConn = (c: Connection) => { setMsg(''); setFail(null); open[c.Name] ? nav({ to: '/s/$sessionId', params: { sessionId: open[c.Name] } }) : connect.mutate(c.Name) }
  const doRow = (a: string, c: Connection, idx: number) => {
    switch (a) {
      case 'connect': return openConn(c)
      case 'edit': return setEdit({ c: structuredClone(c), idx })
      case 'duplicate': return duplicate(c)
      case 'delete_item': return void confirm(`Delete ${c.Name}?`, undefined, 'Delete').then((ok) => ok && save.mutate(list.filter((_, i) => i !== idx)))
      case 'test': return void testConn(c)
      case 'copy_url': return void copyUrl(c, false)
      case 'copy_url_password': return void copyUrl(c, true)
      case 'import_url': urlBox.current?.focus(); return void setMsg('Paste a connection URL and press Enter.')
      case 'export_connections': return setIo('export')
      case 'import_connections': return setIo('import')
      case 'sort': return setDlg('sort')
    }
  }
  const rowItems = (c: Connection, idx: number): [string, () => void, string?][] => (meta.data?.menus?.welcome ?? [{ label: 'Connect', action: 'connect' }, { label: 'Edit…', action: 'edit' }, { label: 'Duplicate', action: 'duplicate' }, { label: 'Delete…', action: 'delete_item' }]).map((it) => [it.label, () => doRow(it.action, c, idx),
    c.Source && ['edit', 'delete_item'].includes(it.action) ? 'this connection comes from a connection source file: edit that file' : whyNot(it)])
  // dropping a connection on a group header (or the no-group strip) moves it there; source connections live in their own files
  const moveTo = (name: string, group: string) => {
    const c = list.find((x) => x.Name === name)
    if (!c || (c.Group ?? '') === group) return
    if (c.Source) return setMsg('That connection comes from a connection source file. Edit that file to move it.')
    save.mutate(list.map((x) => (x.Name === name ? { ...x, Group: group || undefined } : x)))
  }
  const tagColor = (c: Connection) => meta.data?.tags?.find((t) => t.name === c.Tag)?.color
  const groups = [...new Set(list.map((c) => c.Group ?? ''))].sort()
  const upsert = (c: Connection, idx: number) => save.mutate(idx < 0 ? [...list, c] : list.map((x, i) => (i === idx ? c : x)))
  // Paste a URL on the start page: the server parses it and the form opens filled in.
  const fromUrl = async () => {
    if (!url.trim()) return
    try { setEdit({ c: { ...blank(), ...(await api<Connection>('POST', '/connections/parse', { url })) }, idx: -1 }); setUrl(''); setMsg('') } catch (e) { setMsg(badUrl(e)) }
  }

  if (conns.error) return <div className="min-h-full bg-[radial-gradient(var(--rule)_1px,transparent_1px)] bg-[size:22px_22px]"><div className="max-w-[920px] mx-auto pt-10 px-7 pb-20 max-[760px]:pt-7 max-[760px]:px-4 max-[760px]:pb-12"><header className="flex items-baseline gap-4"><h1 className="m-0 text-[36px] leading-none tracking-[-.03em] font-medium font-mono max-[760px]:text-[32px] after:content-[''] after:inline-block after:w-[.5em] after:h-[.85em] after:ml-[.12em] after:align-[-.08em] after:bg-accent after:animate-blink motion-reduce:after:animate-none">webtuidb</h1></header><p className="mt-[10px] mb-0 max-w-[56ch] text-[15px] text-del">{conns.error.message}</p><footer className="mt-16 text-ink-3 text-[12px]">Open the URL printed by <code>webtuidb --frontend</code>. It carries the access token.</footer></div></div>
  return (
    <div className="min-h-full bg-[radial-gradient(var(--rule)_1px,transparent_1px)] bg-[size:22px_22px]"><div className="max-w-[920px] mx-auto pt-10 px-7 pb-20 max-[760px]:pt-7 max-[760px]:px-4 max-[760px]:pb-12">
      <header className="flex flex-wrap items-baseline gap-2.5"><h1 className="flex-1 min-w-0 m-0 text-[36px] leading-none tracking-[-.03em] font-medium font-mono max-[760px]:text-[32px] after:content-[''] after:inline-block after:w-[.5em] after:h-[.85em] after:ml-[.12em] after:align-[-.08em] after:bg-accent after:animate-blink motion-reduce:after:animate-none">Connections</h1><div className="flex flex-wrap items-center gap-1.5"><button className="btn-ghost" onClick={() => openSqlFile().then(viewFile)} title="Read a .sql file without connecting">Open file…</button><button className="btn-ghost" onClick={() => setDlg('settings')}>Settings</button><button className="btn-ghost" onClick={() => signOut()} title="Forget the access token in this browser tab">Log out</button><button className="btn-ghost" onClick={() => setIo('import')}>Import…</button><button className="btn-ghost" onClick={() => setIo('export')} disabled={!list.length}>Export…</button><button className="btn-primary ml-1" onClick={() => setEdit({ c: blank(), idx: -1 })}>Add connection</button></div></header>
      <p className="mt-[10px] mb-0 max-w-[56ch] text-[15px] text-ink-2">Pick a database to browse and edit. Each connection's safe mode decides what needs a confirmation or a password before it is sent.</p>
      <form className="flex items-center gap-[10px] mt-6 pb-2 border-b border-accent before:content-['>'] before:text-accent before:[font:500_16px_var(--font-mono)]" onSubmit={(e) => { e.preventDefault(); fromUrl() }}>
        <input ref={urlBox} className="flex-1 min-w-0 bg-transparent border-0 px-0 py-1.5 text-[16px] leading-[1.3] focus-visible:outline-0" aria-label="Connection URL" placeholder="Paste a connection URL" value={url} onChange={(e) => setUrl(e.target.value)} autoComplete="off" spellCheck={false} />
        <button type="submit" disabled={!url.trim()}>Continue</button>
      </form>
      {msg && <p className={`my-[1em] ${/^(Copied|Connected|Connecting|Testing|Paste)/.test(msg) ? 'text-ink-2' : 'text-del'}`} role={/^(Copied|Connected|Connecting|Testing|Paste)/.test(msg) ? 'status' : 'alert'}>{msg}</p>}
      {fail && (
        <div className="my-[1em] max-w-[72ch] popover border-del/50 px-3.5 py-2.5 text-[13px] motion-safe:animate-rise" role="alert">
          <div className="flex items-start gap-3"><div className="flex-1 min-w-0"><div className="text-del font-medium">{fail.head}</div><div className="text-ink-2 mt-0.5">{hint(fail.detail)}</div></div>
            <button className="btn-ghost btn-icon text-ink-3 hover:text-ink -mr-1.5 -mt-1" aria-label="Dismiss" onClick={() => setFail(null)}>✕</button></div>
          <div className="flex flex-wrap items-center gap-2 mt-2">
            {fail.c && !fail.c.Source && <button onClick={() => { const c = fail.c!; setFail(null); setEdit({ c, idx: list.findIndex((o) => o.Name === c.Name) }) }}>Edit connection…</button>}
            {fail.c && <button className="btn-ghost" onClick={() => connect.mutate(fail.c!.Name)}>Try again</button>}
            <details className="text-[12px] font-mono"><summary className="cursor-pointer text-ink-3 hover:text-ink select-none">Technical details</summary><pre className="m-0 mt-1.5 whitespace-pre-wrap wrap-anywhere text-ink-2 max-h-[30vh] overflow-auto">{fail.detail}</pre></details>
          </div>
        </div>
      )}
      <div className="flex flex-wrap gap-1 mt-[10px]" aria-label="Supported engines">
        {(meta.data?.drivers ?? []).map((d) => <button key={d} className="flex items-center gap-[7px] py-1 pr-[10px] pl-[7px] bg-transparent text-ink-2 text-[12px] hover:text-ink hover:bg-raised" title={`New ${LABELS[d] ?? d} connection`} onClick={() => setEdit({ c: blank(d), idx: -1 })}><Logo driver={d} size={18} /><span>{LABELS[d] ?? d}</span></button>)}
      </div>
      {conns.isLoading && <p className="mt-7 mb-0 p-0 text-ink-2 text-[15px] max-w-[48ch]">Loading connections…</p>}
      {list.length === 0 && !conns.isLoading && <p className="mt-7 mb-0 p-0 text-ink-2 text-[15px] max-w-[48ch]">No connections yet. Paste a URL above, or pick an engine.</p>}
      {list.length > 1 && <div className="flex items-center gap-2 mt-6 text-[12px] text-ink-2 font-mono"><span className="flex-1">{list.length} connections</span><label className="flex items-center gap-2">Sort by<Select className="bg-raised border-transparent" aria-label="Sort by" value={sortBy} onChange={setSort} options={SORTS} /></label></div>}
      <div className="mt-7">
        {groups.map((g) => (
          <div key={g}>
            {!g && drag && <div className="mt-3 px-3 py-2 text-[12px] text-ink-2 border border-dashed border-accent font-mono" onDragOver={(e) => e.preventDefault()} onDrop={(e) => { e.preventDefault(); moveTo(drag, ''); setDrag(null) }}>Drop here to take it out of its group</div>}
            {g && <div className={`group/g flex items-center gap-2 mt-7 pb-1.5 text-[12px] text-ink-2 border-b border-rule font-mono ${drag ? 'outline outline-1 outline-offset-2 outline-accent/60' : ''}`} onDragOver={(e) => { if (drag) e.preventDefault() }} onDrop={(e) => { if (drag) { e.preventDefault(); moveTo(drag, g); setDrag(null) } }}><span className="flex-1 min-w-0 truncate">{g}<span className="ml-2 text-ink-3">{list.filter((c) => (c.Group ?? '') === g).length}</span></span>
              <button className="btn-ghost text-[11.5px] opacity-60 group-hover/g:opacity-100 focus-visible:opacity-100" onClick={() => renameGroup(g)}>Rename</button><button className="btn-ghost btn-danger text-[11.5px] opacity-60 group-hover/g:opacity-100 focus-visible:opacity-100" onClick={() => deleteGroup(g)}>Delete</button></div>}
            {shown.map(({ c, idx }) => (c.Group ?? '') === g && (
              <div className="group grid grid-cols-[8px_28px_1fr_auto] gap-x-4 items-center p-3 -mx-3 border-b border-rule first:border-t cursor-pointer hover:bg-panel focus-within:bg-panel max-[760px]:grid-cols-[8px_28px_1fr] max-[760px]:px-2 max-[760px]:-mx-2" key={c.Name} style={{ '--c': c.Color || tagColor(c) || undefined } as CSSProperties} tabIndex={0} draggable={!c.Source} onDragStart={() => setDrag(c.Name)} onDragEnd={() => setDrag(null)}
                onClick={(e) => { if ((e.target as HTMLElement).closest('button')) return; openConn(c) }}
                onKeyDown={(e) => { if (e.target !== e.currentTarget) return; if (e.key === 'Enter') openConn(c); else if (e.key === 'ContextMenu' || (e.key === 'F10' && e.shiftKey)) { e.preventDefault(); const r = e.currentTarget.getBoundingClientRect(); setRowMenu({ x: r.left + 40, y: r.bottom, c, idx }) } }}
                onContextMenu={(e) => { e.preventDefault(); setRowMenu({ x: e.clientX, y: e.clientY, c, idx }) }}>
                <div className="w-2 h-10 rounded-[1px] bg-[var(--c,var(--raised))] max-[760px]:h-full" aria-hidden />
                <Logo driver={c.Driver} size={28} className="text-ink-2 group-hover:text-ink" />
                <div className="flex-1 min-w-0"><span className="text-[20px] leading-[1.15] font-medium tracking-[-.015em] [overflow-wrap:anywhere]">{c.Name}</span>
                  {open[c.Name] && <span className="chip ml-2.5 align-[3px] border-new text-new" title="A workspace for this connection is open in this browser tab">● open</span>}
                  {c.Tag && <span className="chip ml-2.5 align-[3px] font-medium" style={tagColor(c) ? { borderColor: tagColor(c), color: tagColor(c) } : undefined}>{c.Tag}</span>}
                  {c.ReadOnly && <span className="chip ml-2 align-[3px]" title="Every statement that changes data is refused">read only</span>}
                  {c.Source && <span className="chip ml-2 align-[3px] text-ink-3" title={`From the connection source ${c.Source}. Edit that file to change it.`}>source</span>}
                  <div className="mt-0.5 font-mono text-[12px] leading-normal text-ink-2 [overflow-wrap:anywhere]"><b className="font-normal text-ink mr-3">{LABELS[c.Driver] ?? c.Driver}</b>{fileBased.includes(c.Driver) ? c.Database : `${c.Host}${c.Port ? ':' + c.Port : ''}${c.Database ? '/' + c.Database : ''}`}{c.lastConnected && <span className="ml-3 text-ink-3" title={new Date(c.lastConnected).toLocaleString()}>connected {ago(c.lastConnected)}</span>}</div></div>
                <div className="flex items-center gap-1.5 max-[760px]:col-[2/-1] max-[760px]:mt-[10px]">
                  <button className="btn-ghost btn-icon text-[15px] leading-none" aria-label={`More actions for ${c.Name}`} aria-haspopup="menu" title="Connect, edit, duplicate, copy as URL, test, delete" onClick={(e) => { const r = e.currentTarget.getBoundingClientRect(); setRowMenu({ x: r.left, y: r.bottom + 2, c, idx }) }}>⋯</button>
                  <button className="btn-primary" disabled={connect.isPending} onClick={() => { setMsg(''); open[c.Name] ? nav({ to: '/s/$sessionId', params: { sessionId: open[c.Name] } }) : connect.mutate(c.Name) }}>{connect.isPending && connect.variables === c.Name ? 'Connecting…' : open[c.Name] ? 'Resume' : 'Connect'}</button>
                </div>
              </div>
            ))}
          </div>
        ))}
      </div>
      {rowMenu && <TabMenu x={rowMenu.x} y={rowMenu.y} label={rowMenu.c.Name} onClose={() => setRowMenu(null)} items={rowItems(rowMenu.c, rowMenu.idx)} />}
      {dlg === 'sort' && <Dialog title="Sort connections by" onClose={() => setDlg('')}><div className="flex flex-col gap-0.5">{SORTS.map((o) => <button key={o.value} autoFocus={o.value === sortBy} className={`menu-item ${o.value === sortBy ? 'btn-on' : ''}`} onClick={() => { setSort(o.value); setDlg('') }}>{o.label}</button>)}</div></Dialog>}
      {dlg === 'settings' && <SettingsDialog onClose={() => setDlg('')} notify={setMsg} />}
      {dlg === 'help' && meta.data && <Help km={meta.data.keymap} onClose={() => setDlg('')} />}
      {dlg === 'recent' && <RecentDialog onClose={() => setDlg('')} onPick={(e) => openRecentFile(e).then(viewFile)} />}
      {io === 'export' && <ExportConnsDialog groups={groups.filter(Boolean)} onClose={() => setIo('')} />}
      {io === 'import' && <ImportConnsDialog onClose={() => setIo('')} onDone={() => qc.invalidateQueries({ queryKey: ['conns'] })} />}
      {edit && meta.data && <ConnForm meta={meta.data} init={edit.c} isNew={edit.idx < 0} error={save.error?.message} onClose={() => { save.reset(); setEdit(null) }} onSave={(c) => upsert(c, edit.idx)} />}
    </div></div>
  )
}

function ConnForm({ meta, init, isNew, error, onSave, onClose }: { meta: Meta; init: Connection; isNew: boolean; error?: string; onSave: (c: Connection) => void; onClose: () => void }) {
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
  const parseUrl = async (text = url): Promise<Connection | undefined> => {
    if (!dirty.current) return c
    if (!text.trim()) { dirty.current = false; return c }
    try {
      const p = await api<Connection>('POST', '/connections/parse', { url: text })
      dirty.current = false
      setOptText(Object.entries(p.Options ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${k}=${v}`).join(' ;; '))
      setUrlErr('')
      const merge = (x: Connection): Connection => ({ ...x, Driver: p.Driver, Host: p.Host, Port: p.Port, User: p.User, Password: p.Password, Database: p.Database, SSLMode: p.SSLMode, Options: p.Options, SSH: p.SSH ?? x.SSH,
        Name: x.Name || p.Name, Tag: p.Tag || x.Tag, Color: p.Color || x.Color })
      setC(merge)
      return merge(c)
    } catch (e) { setUrlErr(badUrl(e)) }
  }
  const pasteUrl = async () => { // the URL box is masked, so a pasted URL comes in through a prompt and replaces it whole
    const u = (await ask('Paste a connection URL'))?.trim(); if (!u) return
    dirty.current = true; setUrl(u); parseUrl(u)
  }
  const presets = meta.passwordCmdPresets?.length ? meta.passwordCmdPresets : PRESETS
  const set = <K extends keyof Connection>(k: K, v: Connection[K]) => setC((x) => ({ ...x, [k]: v }))
  const file = (meta.fileBased ?? []).includes(c.Driver)
  // options and bootstrap are edited as text separated by ' ;; ', like the TUI form
  const [optText, setOptText] = useState(() => Object.entries(init.Options ?? {}).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => `${k}=${v}`).join(' ;; '))
  const [bootText, setBootText] = useState(() => (init.Bootstrap ?? []).join(' ;; '))
  const parts = (t: string) => t.split(';;').map((x) => x.trim()).filter(Boolean)
  const optsOf = (t: string) => Object.fromEntries(parts(t).map((o) => { const i = o.indexOf('='); return i < 0 ? [o, ''] : [o.slice(0, i).trim(), o.slice(i + 1).trim()] }))
  const clean = (x = c): Connection => ({ ...x, Port: x.Port ? Number(x.Port) : undefined, SSH: x.SSH?.Host ? x.SSH : null, Color: colorHex(x.Color ?? '') ?? x.Color })
  const colorBad = colorHex(c.Color ?? '') === null
  const why = !c.Name?.trim() ? 'A name is required.' : colorBad ? 'Use a colour name like teal or a code like #7aa2f7.' : ''
  // closing a form that differs from what it opened with asks first (Escape, the backdrop, ✕ and Cancel all come here)
  const close = async () => { if ((dirty.current || JSON.stringify(c) !== JSON.stringify(init)) && !(await confirm('Discard the changes to this connection?', undefined, 'Discard'))) return; onClose() }
  const uid = useId()
  const showSSL = !!c.SSLMode && c.SSLMode !== 'disable'
  const field = (label: string, k: keyof Connection, type = 'text') => (
    <>
      <label htmlFor={uid + k}>{label}</label>
      <input id={uid + k} type={type} value={(c[k] as string | number | undefined) ?? ''} onChange={(e) => set(k, (type === 'number' ? Number(e.target.value) : e.target.value) as never)} />
    </>
  )
  return (
    <Dialog title={isNew || !init.Name ? 'New connection' : `Edit ${init.Name}`} onClose={close}>
      <div className="grid grid-cols-[150px_1fr] gap-x-[14px] gap-y-2 items-center [&_label]:text-ink-2 [&_label]:text-right max-[760px]:grid-cols-1 max-[760px]:gap-[3px] max-[760px]:[&_label]:mt-1.5 max-[760px]:[&_label]:text-left">
        <label htmlFor={uid + 'url'}>Connection URL</label>
        <div className="flex gap-1.5">
          {/* masked because it carries the password: a click selects all of it, so typing or pasting replaces the URL instead of landing inside it */}
          <input id={uid + 'url'} className="font-mono flex-1 min-w-0" type={showUrl ? 'text' : 'password'} autoComplete="off" spellCheck={false} placeholder="postgresql://user:password@host:5432/database" value={url}
            onFocus={(e) => e.target.select()} onMouseUp={(e) => e.preventDefault()}
            onChange={(e) => { dirty.current = true; setUrl(e.target.value) }} onBlur={() => { parseUrl() }} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); parseUrl() } }} />
          <button type="button" onClick={pasteUrl} title="Fill the form from a connection URL">Paste URL…</button>
          <button type="button" onClick={() => setShowUrl(!showUrl)}>{showUrl ? 'Hide' : 'Show'}</button>
        </div>
        {urlErr && <p className="text-del" style={{ gridColumn: '1 / -1', margin: 0 }}>{urlErr}</p>}
        {field('Name', 'Name')}
        <label>Driver</label>
        <Select className="w-full" aria-label="Driver" value={c.Driver} onChange={(v) => set('Driver', v)} options={meta.drivers.map((d) => ({ value: d, label: LABELS[d] ?? d }))} />
        {file ? field('File path', 'Database') : <>
          {field('Host', 'Host')}{field('Port', 'Port', 'number')}{field('User', 'User')}
          <label htmlFor={uid + 'pw'}>Password</label>
          <div className="flex items-center gap-3"><input id={uid + 'pw'} className="flex-1 min-w-0" type="password" autoComplete="new-password" aria-label="Password" disabled={c.Password === 'ask'} value={c.Password === 'ask' ? '' : c.Password ?? ''} onChange={(e) => set('Password', e.target.value)} />
            <label className="flex items-center gap-1.5 whitespace-nowrap !text-left"><input type="checkbox" checked={c.Password === 'ask'} onChange={(e) => set('Password', e.target.checked ? 'ask' : '')} />Ask every time</label></div>
          <label htmlFor={uid + 'pwcmd'}>Password command</label>
          <div className="min-w-0">
            <div className="flex gap-1.5">
              <input id={uid + 'pwcmd'} className="flex-1 min-w-0" aria-label="Password command" value={c.PasswordCmd ?? ''} placeholder="a command that prints the password" onChange={(e) => set('PasswordCmd', e.target.value)} />
              {/* the secret stores nobody should have to type a command for; <name> becomes this connection's name */}
              <Select className="w-[9em]" aria-label="Password command presets" value="" onChange={(v) => { const p = presets.find((x) => x.name === v); if (p) set('PasswordCmd', p.command.split('<name>').join(c.Name || 'name')) }} options={[{ value: '', label: 'Presets…' }, ...presets.map((p) => p.name)]} />
            </div>
            {presets.find((p) => c.PasswordCmd && c.PasswordCmd === p.command.split('<name>').join(c.Name || 'name'))?.note && <div className="text-ink-3 text-[11.5px] mt-0.5">{presets.find((p) => c.PasswordCmd === p.command.split('<name>').join(c.Name || 'name'))!.note.split('<name>').join(c.Name || 'name')}</div>}
          </div>
          {field('Database', 'Database')}
          <label>SSL mode</label>
          <Select className="w-full" aria-label="SSL mode" value={c.SSLMode ?? ''} onChange={(v) => set('SSLMode', v)} options={[{ value: '', label: 'default' }, ...meta.sslModes]} />
          {showSSL && <>{field('CA cert', 'SSLCA')}{field('Client cert', 'SSLCert')}{field('Client key', 'SSLKey')}</>}
          {field('SOCKS5 proxy', 'SocksProxy')}
          {['postgres', 'mysql', 'mariadb'].includes(c.Driver) && <><label>AWS IAM auth</label><label className="flex items-center gap-2 !text-left"><input type="checkbox" checked={!!c.IAMAuth} onChange={(e) => set('IAMAuth', e.target.checked)} />Use an RDS IAM token instead of the password</label></>}
        </>}
        <label htmlFor={uid + 'enc'}>Encoding</label><input id={uid + 'enc'} className="w-full" aria-label="Encoding" placeholder="server default" value={c.Encoding ?? ''} onChange={(e) => set('Encoding', e.target.value)} />
        {field('Group', 'Group')}
        <label>Tag</label>
        <Select className="w-full" aria-label="Tag" value={c.Tag ?? ''} onChange={(v) => set('Tag', v)} options={[{ value: '', label: 'none' }, ...(meta.tags ?? []).map((t) => t.name), ...(c.Tag && !meta.tags?.some((t) => t.name === c.Tag) ? [c.Tag] : [])]} />
        {field('Color', 'Color')}
        {colorBad && <p className="text-del m-0" style={{ gridColumn: '1 / -1' }} role="alert">Use a colour name like teal or a code like #7aa2f7.</p>}
        <label>Read only</label>
        <label className="flex items-center gap-2 !text-left"><input type="checkbox" checked={!!c.ReadOnly} onChange={(e) => set('ReadOnly', e.target.checked)} />Refuse every statement that changes data</label>
        <label>Safe mode</label>
        <Select className="w-full" aria-label="Safe mode" value={c.SafeMode ?? ''} onChange={(v) => set('SafeMode', v)} options={[{ value: '', label: 'default' }, ...meta.safeModes]} />
        {!file && <>
          <label htmlFor={uid + 'ssh'}>SSH tunnel host</label>
          <input id={uid + 'ssh'} value={c.SSH?.Host ?? ''} onChange={(e) => set('SSH', { Port: 22, User: '', Key: '', Password: '', ...c.SSH, Host: e.target.value })} />
          {c.SSH?.Host && <>
            <label>SSH user</label><input value={c.SSH.User} onChange={(e) => set('SSH', { ...c.SSH!, User: e.target.value })} />
            <label>SSH port</label><input type="number" value={c.SSH.Port || ''} placeholder="22" onChange={(e) => set('SSH', { ...c.SSH!, Port: Number(e.target.value) })} />
            <label>SSH key file</label><input value={c.SSH.Key} placeholder="empty = password or agent" onChange={(e) => set('SSH', { ...c.SSH!, Key: e.target.value })} />
            <label>SSH password</label><input type="password" autoComplete="new-password" value={c.SSH.Password} onChange={(e) => set('SSH', { ...c.SSH!, Password: e.target.value })} />
          </>}
        </>}
        <label htmlFor={uid + 'opts'}>Options</label><input id={uid + 'opts'} aria-label="Options" placeholder="key=value ;; key=value (driver extras)" value={optText} onChange={(e) => { setOptText(e.target.value); set('Options', optsOf(e.target.value)) }} />
        <label htmlFor={uid + 'boot'}>Bootstrap SQL</label><input id={uid + 'boot'} aria-label="Bootstrap SQL" placeholder="statements run on every new connection, separated by ;;" value={bootText} onChange={(e) => { setBootText(e.target.value); set('Bootstrap', parts(e.target.value)) }} />
      </div>
      {error && <p className="my-[1em] text-del" role="alert">{error}</p>}
      <div className="flex justify-end gap-2 mt-[18px]">
        {test ? <span className={`flex-1 min-w-0 text-left ${test.startsWith('Connected') || test.startsWith('Testing') ? 'text-ink-2' : 'text-del'}`}>{test}</span> : why && <span className="flex-1 min-w-0 text-left text-ink-2">{why}</span>}
        <button onClick={async () => {
          setTest('Testing…')
          const go = (askedPassword?: string) => api<{ serverInfo: string }>('POST', '/connections/test', { ...clean(), askedPassword })
          try {
            let r
            try { r = await go() } catch (e) {
              if (!(e instanceof ApiError && e.status === 422 && e.body.passwordRequired)) throw e
              const pw = await askSecret(`Password for ${c.Name || 'this connection'}`)
              if (pw === null) return setTest('')
              r = await go(pw)
            }
            setTest('Connected: ' + r.serverInfo)
          } catch (e) { setTest(String(e instanceof Error ? e.message : e)) }
        }}>Test</button>
        <button onClick={close}>Cancel</button>
        <button className="btn-primary" disabled={!!why} title={why || undefined} onClick={async () => { const m = await parseUrl(); if (m) onSave(clean(m)) }}>Save</button>
      </div>
    </Dialog>
  )
}

// ---- export / import of the connection list ------------------------------------------------------------

const b64 = (b: Uint8Array) => { let t = ''; for (let i = 0; i < b.length; i += 0x8000) t += String.fromCharCode(...b.subarray(i, i + 0x8000)); return btoa(t) }

function ExportConnsDialog({ groups, onClose }: { groups: string[]; onClose: () => void }) {
  const [pass, setPass] = useState(''), [withPw, setWithPw] = useState(false), [group, setGroup] = useState(''), [err, setErr] = useState('')
  const go = async () => {
    try { const f = await download('/connections/export', { Passphrase: pass, Passwords: withPw, Group: group }); saveBlob(f.blob, f.name); onClose() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) }
  }
  return (
    <Dialog title="Export connections" onClose={onClose}>
      <div className="grid grid-cols-[150px_1fr] gap-x-[14px] gap-y-2 items-center [&_label]:text-ink-2 max-[760px]:grid-cols-1">
        <label>Which</label><Select className="w-full" aria-label="Group" value={group} onChange={setGroup} options={[{ value: '', label: 'All connections' }, ...groups.map((g) => ({ value: g, label: `Group ${g}` }))]} />
        <label>Passphrase</label><input type="password" autoComplete="new-password" aria-label="Passphrase" placeholder="empty = a plain, unencrypted file" value={pass} onChange={(e) => { setPass(e.target.value); if (!e.target.value) setWithPw(false) }} />
        <span /><label className="flex items-center gap-2 !text-left"><input type="checkbox" checked={withPw} disabled={!pass} onChange={(e) => setWithPw(e.target.checked)} />Include passwords (needs a passphrase)</label>
      </div>
      <p className="text-ink-2 text-[12px]">Connections from a connection source are not exported: they live in their own files.</p>
      {err && <p className="text-del" role="alert">{err}</p>}
      <div className="flex justify-end gap-2 mt-[18px]"><button onClick={onClose}>Cancel</button><button className="btn-primary" onClick={go}>Export</button></div>
    </Dialog>
  )
}

function ImportConnsDialog({ onClose, onDone }: { onClose: () => void; onDone: () => void }) {
  const [file, setFile] = useState<File | null>(null), [pass, setPass] = useState(''), [err, setErr] = useState('')
  const [res, setRes] = useState<{ added: string[]; skipped: string[]; dropped: string[] } | null>(null)
  const go = async () => {
    if (!file) return
    try { setRes(await api('POST', '/connections/import', { ContentBase64: b64(new Uint8Array(await file.arrayBuffer())), Passphrase: pass })); onDone() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) }
  }
  const list = (t: string, l: string[]) => l.length > 0 && <p className="my-1.5"><b className="font-medium">{t}</b> <span className="text-ink-2">{l.join(', ')}</span></p>
  return (
    <Dialog title="Import connections" onClose={onClose}>
      {res ? <>
        {list(`Added ${res.added.length}:`, res.added)}{list('Skipped, the name exists already:', res.skipped)}
        {list('Dropped from the file, they could run programs or read keys on this machine:', res.dropped)}
        {!res.added.length && !res.skipped.length && <p className="text-ink-2">The file had no connections.</p>}
        <div className="flex justify-end mt-[18px]"><button className="btn-primary" autoFocus onClick={onClose}>Close</button></div>
      </> : <>
        <div className="grid grid-cols-[150px_1fr] gap-x-[14px] gap-y-2 items-center [&_label]:text-ink-2 max-[760px]:grid-cols-1">
          <label>File</label><input type="file" aria-label="Connections file" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          <label>Passphrase</label><input type="password" autoComplete="off" aria-label="Passphrase" placeholder="if the file is encrypted" value={pass} onChange={(e) => setPass(e.target.value)} />
        </div>
        <p className="text-ink-2 text-[12px]">Password commands, bootstrap statements and SSH keys in the file are always dropped: a file from someone else must not be able to run a program here. A connection whose name exists is skipped.</p>
        {err && <p className="text-del" role="alert">{err}</p>}
        <div className="flex justify-end gap-2 mt-[18px]"><button onClick={onClose}>Cancel</button><button className="btn-primary" disabled={!file} onClick={go}>Import</button></div>
      </>}
    </Dialog>
  )
}

