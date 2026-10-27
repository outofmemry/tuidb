// A standalone SQL viewer: .sql files opened without a connection, read only. Reached by Open file on the connection list.
import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import CodeMirror from '@uiw/react-codemirror'
import { cmTheme } from './cmtheme'
import { StandardSQL, sql } from '@codemirror/lang-sql'
import { EditorState } from '@codemirror/state'
import { api, type Meta } from '../utils/api'
import { openRecentFile, openSqlFile } from '../utils/files'
import { useKeymap } from '../utils/useKeymap'
import { RecentDialog } from './Extras'

type File = { name: string; text: string }
let files: File[] = [] // kept for the page's life, so Open file on the list and then coming back here keeps the tabs
let current = 0
export function addViewerFile(f: File) { files = [...files, f]; current = files.length - 1 }

export function SqlViewer() {
  const nav = useNavigate()
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  const [, bump] = useState(0)
  const [recent, setRecent] = useState(false)
  const add = (f: File | null) => { if (f) { addViewerFile(f); bump((n) => n + 1) } }
  useKeymap(meta.data?.keymap, ['open_file', 'open_recent'], (a) => { if (a === 'open_file') openSqlFile().then(add); else setRecent(true) })
  const close = (i: number) => { files = files.filter((_, j) => j !== i); current = Math.max(0, Math.min(current, files.length - 1)); bump((n) => n + 1) }
  const f = files[current]
  return (
    <div className="flex flex-col h-full">
      <div className="toolbar bg-canvas">
        <button onClick={() => nav({ to: '/' })}>Connections</button>
        <button onClick={() => openSqlFile().then(add)}>Open file…</button>
        <button onClick={() => setRecent(true)}>Recent…</button>
        <span className="text-ink-2 text-[12px]">Read only, no connection. Open the file from a workspace to run it.</span>
      </div>
      <div className="flex h-8 text-[12px] border-b border-rule overflow-x-auto [scrollbar-width:none] bg-canvas font-mono">
        {files.map((x, i) => (
          <div key={x.name + i} className={`group flex items-center gap-2 pl-3.5 pr-2.5 cursor-pointer whitespace-nowrap border-r border-rule ${i === current ? 'text-ink bg-panel shadow-[inset_0_-2px_0_var(--accent)]' : 'text-ink-2 hover:text-ink hover:bg-panel'}`} onClick={() => { current = i; bump((n) => n + 1) }}>
            {x.name}<button className="btn-ghost" aria-label={`Close ${x.name}`} onClick={(e) => { e.stopPropagation(); close(i) }}>✕</button>
          </div>
        ))}
      </div>
      {f ? <div className="flex-1 min-h-0 overflow-auto" data-ctx="editor"><CodeMirror key={current + f.name} value={f.text} height="100%" theme={dark() ? 'dark' : 'light'} editable={false} extensions={[sql({ dialect: StandardSQL }), EditorState.readOnly.of(true)]} basicSetup={{ lineNumbers: true, foldGutter: true }} /></div>
        : <div className="px-8 py-14 max-w-[46ch] text-ink-2 text-[15px] leading-normal">Open a .sql file to read it here.</div>}
      {recent && <RecentDialog onClose={() => setRecent(false)} onPick={(e) => openRecentFile(e).then(add)} />}
    </div>
  )
}
