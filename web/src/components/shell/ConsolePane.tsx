// Console pane: the log of statements the session ran, with find, errors-only and copy.
import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, qs, type MenuItem } from '../../utils/api'
import { copyText } from '../../utils/copy'
import { TabMenu } from './TabMenu'
import { msg } from './ui'

const KW = /\b(select|from|where|insert|into|values|update|set|delete|create|alter|drop|table|index|join|left|right|inner|outer|on|group|order|by|limit|offset|having|and|or|not|null|as|in|is|like|begin|commit|rollback|truncate)\b|('(?:[^']|'')*')|(--[^\n]*)|(\b\d+(?:\.\d+)?\b)/gi
function highlight(sql: string): ReactNode[] {
  const out: ReactNode[] = []
  let last = 0
  for (const m of sql.matchAll(KW)) {
    out.push(sql.slice(last, m.index))
    out.push(<span key={m.index} className={m[1] ? 'text-accent font-semibold' : m[2] ? 'text-new' : m[3] ? 'text-ink-3 italic' : 'text-edit'}>{m[0]}</span>)
    last = m.index! + m[0].length
  }
  out.push(sql.slice(last))
  return out
}

// shortcut: console lines carry no failed flag yet, so "Errors only" matches text that says error or failed; a `failed` field from the server is used when present.
const isErr = (l: { text: string; failed?: boolean }) => !!l.failed || /(^|\W)(error|failed|exception)(\W|$)/i.test(l.text)

export function ConsolePane({ conn, items, notify, onClose }: { conn: string; items?: MenuItem[]; notify?: (m: string) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['console', conn], queryFn: () => api<{ time: string; text: string; failed?: boolean }[]>('GET', '/console' + qs({ conn })), refetchInterval: 2000, gcTime: 0 })
  const end = useRef<HTMLDivElement>(null)
  const [find, setFind] = useState(''), [errs, setErrs] = useState(false), [menu, setMenu] = useState<{ x: number; y: number; text: string } | null>(null)
  const lines = (q.data ?? []).filter((l) => (!errs || isErr(l)) && (!find || l.text.toLowerCase().includes(find.toLowerCase())))
  const [sel, setSel] = useState(-1) // the selected line (n / N move it, y copies it, m opens its menu); -1 follows the newest
  const at = Math.min(sel < 0 ? lines.length - 1 : sel, lines.length - 1)
  useEffect(() => { if (sel < 0) end.current?.scrollTo(0, end.current.scrollHeight) }, [q.data?.length, errs, sel])
  useEffect(() => { setSel(-1) }, [find, errs])
  useEffect(() => { if (sel >= 0) end.current?.querySelector('[data-sel]')?.scrollIntoView({ block: 'nearest' }) }, [sel])
  const clear = async () => { await api('DELETE', '/console').catch(() => {}); q.refetch() }
  const copy = (text: string) => copyText(text).then(() => notify?.('Copied the line'), (e) => notify?.(msg(e)))
  const mark = (t: string): ReactNode => { // the find text is highlighted inside a plain-text line
    if (!find) return highlight(t)
    const at = t.toLowerCase().indexOf(find.toLowerCase())
    return at < 0 ? highlight(t) : <>{highlight(t.slice(0, at))}<mark className="bg-edit/40 text-ink">{t.slice(at, at + find.length)}</mark>{highlight(t.slice(at + find.length))}</>
  }
  // keymap actions of the console context (data-ctx="console"): the TUI's / n N y e m
  const rowRef = useRef<HTMLDivElement>(null)
  const acts = useRef<(a: string) => boolean>(() => false)
  acts.current = (a) => {
    const step = (d: number) => lines.length && setSel((at + d + lines.length) % lines.length)
    switch (a) {
      case 'console_find': document.getElementById('console-find')?.focus(); return true
      case 'console_next': step(1); return true
      case 'console_prev': step(-1); return true
      case 'console_copy': if (lines[at]) copy(lines[at].text); else notify?.('No line to copy'); return true
      case 'console_errors': setErrs((v) => !v); return true
      case 'console_menu': { const r = rowRef.current?.getBoundingClientRect(), b = end.current?.getBoundingClientRect(); if (lines[at]) setMenu({ x: (b?.left ?? 0) + 24, y: r?.bottom ?? b?.top ?? 0, text: lines[at].text }); return true }
    }
    return false
  }
  useEffect(() => {
    const h = (e: Event) => { if (!e.defaultPrevented && acts.current((e as CustomEvent).detail?.action)) e.preventDefault() }
    window.addEventListener('webtuidb:action', h)
    return () => window.removeEventListener('webtuidb:action', h)
  }, [])
  // the right-click menu is meta.menus.console (labels and order); Find focuses the box, Errors only toggles, Copy line copies the clicked line
  const run = (a: string, text: string) => (a === 'find' ? document.getElementById('console-find')?.focus() : a === 'copy' ? copy(text) : a === 'errors_only' ? setErrs((v) => !v) : clear())
  const list: MenuItem[] = items?.length ? items : [{ label: 'Find…', action: 'find' }, { label: 'Copy line', action: 'copy' }, { label: 'Errors only', action: 'errors_only' }, { label: 'Clear', action: 'clear_console' }]
  return (
    <div className="h-[200px] flex-none border-t border-rule flex flex-col bg-canvas font-mono" data-ctx="console" role="region" aria-label="Console log">
      <div className="toolbar bg-canvas"><strong className="font-medium">Console</strong>
        <input id="console-find" className="flex-1 min-w-[8em] max-w-[22em] text-[12px] py-0" aria-label="Find in the console" placeholder="Find" value={find} onChange={(e) => setFind(e.target.value)} onKeyDown={(e) => { if (e.key === 'Escape' && find) { e.stopPropagation(); setFind('') } }} />
        <button className={`btn-ghost ${errs ? 'btn-on' : ''}`} aria-pressed={errs} onClick={() => setErrs(!errs)}>Errors only</button>
        <span className="flex-1" />
        <button className="btn-ghost" onClick={clear}>Clear</button>
        <button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      <div ref={end} className="overflow-auto flex-1 px-3 py-1.5 text-[12px]">
        {lines.map((l, i) => <div key={i} ref={i === at ? rowRef : undefined} {...(i === at && sel >= 0 ? { 'data-sel': '' } : {})} className={`group/l flex items-start gap-2 whitespace-pre-wrap wrap-anywhere py-px hover:bg-panel ${i === at && sel >= 0 ? 'bg-panel shadow-[inset_2px_0_0_var(--accent)]' : ''}`} onContextMenu={(e) => { e.preventDefault(); setMenu({ x: e.clientX, y: e.clientY, text: l.text }) }}>
          <span className="flex-1 min-w-0"><span className="text-ink-3 select-none">{new Date(l.time).toLocaleTimeString()} </span>{mark(l.text)}</span>
          <button className="btn-ghost text-[11px] px-1 opacity-0 group-hover/l:opacity-100 focus-visible:opacity-100" aria-label="Copy this line" onClick={() => copy(l.text)}>Copy</button></div>)}
        {q.data?.length === 0 && <div className="text-ink-2">Statements sent to the database show up here.</div>}
        {!!q.data?.length && !lines.length && <div className="text-ink-2">No line matches.</div>}
      </div>
      {menu && <TabMenu x={menu.x} y={menu.y} label="Console" onClose={() => setMenu(null)} items={list.map((it): [string, () => void] => [it.label, () => run(it.action, menu.text)])} />}
    </div>
  )
}
