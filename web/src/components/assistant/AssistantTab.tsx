// AI assistant tab, with the saved-chat history drawer.
import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type Chat, type ChatSummary, type Meta } from '../../utils/api'
import { copyText } from '../../utils/copy'
import { confirm } from '../shell/Dialogs'
import { chatTitle } from '../../utils/chats'
import { Select } from '../shell/Select'
import { msg } from '../shell/ui'

type Block = { sql: string; readOnly: boolean }
type Msg = { role: 'user' | 'assistant'; content: string; blocks?: Block[] }

// split a reply into prose and fenced code so sql blocks get their buttons
function parts(content: string): { code: boolean; text: string }[] {
  return content.split(/(```[^\n]*\n[\s\S]*?```)/g).filter(Boolean).map((t) => {
    const m = /^```([^\n]*)\n([\s\S]*?)```$/.exec(t)
    return m ? { code: true, text: m[2].trim() } : { code: false, text: t }
  })
}

export function AssistantTab({ sid, onRun, notify }: { sid: string; onRun: (sql: string) => void; notify: (m: string) => void }) {
  const [msgs, setMsgs] = useState<Msg[]>([])
  const [text, setText] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity }).data
  const [models, setModels] = useState<string[]>([])
  const [provider, setProvider] = useState('') // "" = the default provider; the others are [ai.providers.<name>]
  const [model, setModelState] = useState(() => { try { return localStorage.getItem('webtuidb-ai-model') ?? '' } catch { return '' } })
  const setModel = (m: string) => { setModelState(m); try { localStorage.setItem('webtuidb-ai-model', m) } catch { /* private mode */ } }
  const loadModels = async () => { try { setModels((await api<{ models: string[] }>('GET', '/assistant/models' + (provider ? `?provider=${encodeURIComponent(provider)}` : ''))).models) } catch (e) { setErr(msg(e)) } }
  const pick = (v: string) => { setProvider(v); setModels([]); setModel('') } // a model name belongs to its provider
  const ctrl = useRef<AbortController | null>(null)
  const end = useRef<HTMLDivElement>(null)
  const root = useRef<HTMLDivElement>(null)
  const [chatId, setChatId] = useState('') // '' until the first exchange is saved
  const [hist, setHist] = useState(false) // the history drawer
  const chatBase = `/sessions/${sid}/chats`
  // the whole chat is saved after every exchange; the server keeps it under the connection, shared with the terminal
  const save = async (list: Msg[], id = chatId) => {
    if (!list.length) return
    try {
      let at = id
      if (!at) { at = (await api<Chat>('POST', chatBase)).ID; setChatId(at) }
      const now = new Date().toISOString()
      await api('PUT', `${chatBase}/${encodeURIComponent(at)}`, { ID: at, Title: chatTitle(list), Updated: now, Messages: list.map((m) => ({ Role: m.role, Content: m.content, At: now })) })
    } catch (e) { setErr('The chat could not be saved: ' + msg(e)) }
  }
  const newChat = () => { ctrl.current?.abort(); ctrl.current = null; setMsgs([]); setChatId(''); setErr(''); setText('') }
  const clearChat = () => { ctrl.current?.abort(); ctrl.current = null; setMsgs([]); setErr(''); if (chatId) api('PUT', `${chatBase}/${encodeURIComponent(chatId)}`, { ID: chatId, Title: '', Messages: [] }).catch(() => {}) }
  const open = async (id: string) => {
    try {
      const c = await api<Chat>('GET', `${chatBase}/${encodeURIComponent(id)}`)
      ctrl.current?.abort(); ctrl.current = null; setMsgs((c.Messages ?? []).map((m) => ({ role: m.Role === 'user' ? 'user' : 'assistant', content: m.Content }))); setChatId(c.ID); setHist(false); setErr('')
    } catch (e) { setErr(msg(e)) }
  }
  // the keymap's assistant_* actions, answered only by the tab that is on screen
  const acts = useRef({ newChat, clearChat, openHist: () => setHist(true) }); acts.current = { newChat, clearChat, openHist: () => setHist(true) }
  useEffect(() => {
    const h = (e: Event) => {
      if (e.defaultPrevented || !root.current || root.current.offsetParent === null) return
      const a = (e as CustomEvent).detail.action
      if (a === 'assistant_new') { e.preventDefault(); acts.current.newChat() } else if (a === 'assistant_clear') { e.preventDefault(); acts.current.clearChat() } else if (a === 'assistant_history') { e.preventDefault(); acts.current.openHist() }
    }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  }, [])
  useEffect(() => { end.current?.scrollIntoView?.({ block: 'end' }) }, [msgs.length, busy])
  useEffect(() => () => ctrl.current?.abort(), [])
  const send = async () => {
    const t = text.trim(); if (!t || busy) return
    setText(''); chat([...msgs, { role: 'user', content: t }])
  }
  // resend: everything after that user message is dropped and the model answers again
  const resend = (i: number) => { if (!busy) chat(msgs.slice(0, i + 1)) }
  const chat = async (next: Msg[]) => {
    setMsgs(next); setBusy(true); setErr('')
    const mine = ctrl.current = new AbortController()
    try {
      const r = await api<{ reply: string; blocks: Block[] }>('POST', `/sessions/${sid}/assistant`, { messages: next.map(({ role, content }) => ({ role, content })), model: model || undefined, provider: provider || undefined }, mine.signal)
      const all: Msg[] = [...next, { role: 'assistant', content: r.reply, blocks: r.blocks }]
      setMsgs(all); save(all)
    } catch (e) {
      if (ctrl.current !== mine) return // the chat was replaced meanwhile
      if (e instanceof DOMException && e.name === 'AbortError') setErr('Stopped.'); else setErr(msg(e))
    } finally { setBusy(false) }
  }
  const run = async (m: Msg, sql: string) => {
    const b = m.blocks?.find((x) => x.sql === sql)
    if (!b?.readOnly && !(await confirm('This statement changes data or structure. Open it in a query tab and run it?', undefined, 'Open and run'))) return
    onRun(sql)
  }
  return (
    <div ref={root} className="flex-1 min-h-0 flex flex-col relative">
      <div className="toolbar bg-canvas">
        <button onClick={newChat} title="Start a new chat (the current one stays in History)">New chat</button>
        <button onClick={clearChat} disabled={!msgs.length} title="Empty this chat">Clear chat</button>
        <button onClick={() => setHist(true)} aria-haspopup="dialog">History…</button>
      </div>
      <div className="flex-1 min-h-0 overflow-auto p-3 flex flex-col gap-3 font-mono text-[12.5px]">
        {msgs.length === 0 && <div className="max-w-[56ch] text-ink-2 text-[15px] leading-normal">Ask about this database. Write @table to send that table&apos;s definition along, for example: how many orders per day in @orders.</div>}
        {msgs.map((m, i) => (
          <div key={i} className={`group max-w-[90ch] ${m.role === 'user' ? 'self-end bg-raised px-3 py-2 rounded-[2px]' : ''}`}>
            {parts(m.content).map((p, j) => p.code ? (
              <div key={j} className="my-2 border border-rule bg-panel rounded-[2px]">
                <pre className="m-0 p-2.5 overflow-auto text-[12px] whitespace-pre-wrap">{p.text}</pre>
                {m.role === 'assistant' && <div className="flex gap-2 px-2 py-1 border-t border-rule">
                  <button className="btn-ghost" onClick={() => copyText(p.text).then(() => notify('Copied'), (e) => notify(String(e)))}>Copy</button>
                  <button className="btn-ghost" onClick={() => run(m, p.text)}>Run</button>
                  {m.blocks?.find((x) => x.sql === p.text)?.readOnly === false && <span className="text-ink-2 self-center">changes data</span>}
                </div>}
              </div>) : <p key={j} className="m-0 whitespace-pre-wrap">{p.text}</p>)}
            <div className="flex gap-1 mt-1 opacity-0 group-hover:opacity-100 focus-within:opacity-100 max-[760px]:opacity-100">
              <button className="btn-ghost" onClick={() => copyText(m.content).then(() => notify('Copied the message'), (e) => notify(String(e)))}>Copy</button>
              {m.role === 'user' && <button className="btn-ghost" disabled={busy} onClick={() => resend(i)}>Resend</button>}
              <button className="btn-ghost" disabled={busy} onClick={() => setMsgs(msgs.filter((_, j) => j !== i))}>Delete</button>
            </div>
          </div>))}
        {busy && <div className="text-ink-2">Thinking…</div>}
        {err && <div className="text-del">{err}</div>}
        <div ref={end} />
      </div>
      <div className="toolbar bg-canvas">
        {!!meta?.ai?.providers?.length && <label className="flex items-center gap-1.5 text-[12px] text-ink-2">Provider
          <Select className="bg-raised border-transparent" aria-label="Provider" value={provider} onChange={pick} options={[{ value: '', label: `Default${meta.ai.provider ? ` (${meta.ai.provider})` : ''}` }, ...meta.ai.providers]} /></label>}
        <label className="flex items-center gap-1.5 text-[12px] text-ink-2">Model
          <Select className="bg-raised border-transparent" aria-label="Model" value={model} onChange={setModel} options={[{ value: '', label: `Default${meta?.ai?.model ? ` (${meta.ai.model})` : ''}` }, ...(model && !models.includes(model) ? [model] : []), ...models]} /></label>
        <button className="btn-ghost" onClick={loadModels} title="Ask the provider which models it offers">List models</button>
        <span className="text-ink-3 text-[12px]">ACP agents and the Copilot and Codex sign-ins are terminal-only.</span>
      </div>
      <div className="toolbar bg-canvas items-end">
        <textarea className="flex-1 min-w-0 h-16 bg-canvas resize-none" aria-label="Message" placeholder="Ask a question. Ctrl/Cmd+Enter sends." value={text} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) { e.preventDefault(); send() } }} />
        {busy ? <button className="btn-danger" onClick={() => ctrl.current?.abort()} aria-label="Stop the answer">Stop</button> : <button className="btn-primary" disabled={!text.trim()} onClick={send}>Send</button>}
      </div>
      {hist && <ChatHistory base={chatBase} current={chatId} onOpen={open} onClose={() => setHist(false)} onDeleted={(id) => { if (id === chatId) newChat() }} />}
    </div>
  )
}

// The chat history drawer: search over titles and messages, load or delete a saved chat.
function ChatHistory({ base, current, onOpen, onClose, onDeleted }: { base: string; current: string; onOpen: (id: string) => void; onClose: () => void; onDeleted: (id: string) => void }) {
  const [q, setQ] = useState(''), [list, setList] = useState<ChatSummary[] | null>(null), [err, setErr] = useState('')
  useEffect(() => {
    const t = setTimeout(() => {
      api<ChatSummary[] | { chats: ChatSummary[] }>('GET', base + (q.trim() ? `?q=${encodeURIComponent(q.trim())}` : ''))
        .then((r) => { setList((Array.isArray(r) ? r : r.chats) ?? []); setErr('') }, (e) => setErr(msg(e)))
    }, 200)
    return () => clearTimeout(t)
  }, [q, base])
  const del = async (c: ChatSummary) => {
    if (!(await confirm(`Delete the chat "${c.Title || 'untitled'}"?`, undefined, 'Delete'))) return
    try { await api('DELETE', `${base}/${encodeURIComponent(c.ID)}`); setList((l) => l && l.filter((x) => x.ID !== c.ID)); onDeleted(c.ID) } catch (e) { setErr(msg(e)) }
  }
  return (
    <aside className="absolute right-0 top-0 bottom-0 w-[min(360px,100%)] z-20 bg-panel border-l border-rule flex flex-col motion-safe:animate-pop" role="dialog" aria-label="Chat history" onKeyDown={(e) => { if (e.key === 'Escape') { e.stopPropagation(); onClose() } }}>
      <h2 className="m-0 px-2.5 pt-2.5 text-[14px] font-medium">Chat history</h2>
      <div className="flex items-center gap-2 p-2.5 border-b border-rule">
        <input autoFocus className="flex-1 min-w-0" type="search" aria-label="Search chats" placeholder="Search chats" value={q} onChange={(e) => setQ(e.target.value)} />
        <button className="btn-ghost btn-icon" onClick={onClose} aria-label="Close history" title="Close (Esc)">✕</button>
      </div>
      <div className="flex-1 min-h-0 overflow-auto text-[12.5px] font-mono">
        {err && <div className="p-2.5 text-del">{err}</div>}
        {list === null && !err && <div className="p-2.5 text-ink-2">Loading…</div>}
        {list?.length === 0 && <div className="p-2.5 text-ink-2">{q.trim() ? 'No chat matches.' : 'No saved chats yet.'}</div>}
        {list?.map((c) => (
          <div key={c.ID} className={`flex items-center gap-1 px-2.5 py-1.5 border-b border-rule ${c.ID === current ? 'bg-raised' : ''}`}>
            <button className="flex-1 min-w-0 text-left border-transparent" onClick={() => onOpen(c.ID)} title="Open this chat">
              <div className="truncate">{c.Title || 'untitled'}</div>
              <div className="text-ink-3 text-[11.5px]">{c.Count} message{c.Count === 1 ? '' : 's'}, {new Date(c.Updated).toLocaleString()}</div>
            </button>
            <button className="btn-ghost btn-danger" aria-label={`Delete chat ${c.Title}`} onClick={() => del(c)}>Delete</button>
          </div>))}
      </div>
    </aside>
  )
}
