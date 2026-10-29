// Recent files list.
import { useQuery } from '@tanstack/react-query'
import { api } from '../../utils/api'
import { Dialog } from './Dialogs'
import { localRecent } from '../../utils/files'
import { ACTIONS } from './ui'

export function RecentDialog({ onPick, onClose }: { onPick: (entry: string) => void; onClose: () => void }) {
  const q = useQuery({ queryKey: ['recent-files'], queryFn: () => api<{ files: string[] }>('GET', '/state/recent-files'), gcTime: 0 })
  // the server's list holds paths (the terminal's); files picked in this browser are only names, kept in this browser
  const files = q.data ? [...new Set([...q.data.files, ...localRecent()])] : []
  return (
    <Dialog title="Open a recent file" onClose={onClose}>
      {q.error && <div className="text-del">{q.error.message}</div>}
      {q.data && !files.length && <div className="text-ink-2">No file was opened yet.</div>}
      <div className="flex flex-col gap-0.5">{files.map((f, i) => <button key={f} autoFocus={i === 0} className="text-left border-transparent hover:border-ink-3 font-mono truncate" title={f} onClick={() => { onClose(); onPick(f) }}>{f}</button>)}</div>
      <p className="text-ink-3 text-[12px]">A browser cannot reopen a file by name: files first opened here ask for the file again.</p>
      <div className={ACTIONS}><button onClick={onClose}>Cancel</button></div>
    </Dialog>
  )
}
