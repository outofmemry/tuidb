// M15.16 / 17: the jobs centre. One list of background jobs (import, export, backup, restore, search, diff) with progress, stop, download and dismiss.
// useJobs polls the list (every second while the drawer is open or a job runs, slowly otherwise), counts the running ones for the status bar,
// and sends a desktop notification when a job finishes while the page is hidden.
import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api, download, saveBlob, type JobRow, type MenuItem } from '../utils/api'
import { desktopNotify } from '../utils/theme'
import { whyNot } from '../utils/menus'

// which entries show for a job; unknown actions are hidden rather than guessed at
export const jobShows = (action: string, j: { done: boolean; file?: string; err?: string }) => (action === 'cancel' ? !j.done : action === 'download' ? j.done && !!j.file && !j.err : action === 'close' ? j.done : false)

const msg = (e: unknown) => (e instanceof Error ? e.message : String(e))

export function useJobs(sid: string, open: boolean) {
  const last = useRef<Map<string, boolean>>(new Map())
  const q = useQuery({
    queryKey: ['jobs', sid], queryFn: () => api<{ jobs: JobRow[] | null }>('GET', `/sessions/${sid}/jobs`), gcTime: 0,
    refetchInterval: (query) => (query.state.error instanceof ApiError && query.state.error.status === 401 ? false : open || query.state.data?.jobs?.some((j) => !j.done) ? 1000 : 4000), // no token: stop asking
  })
  const jobs = q.data?.jobs ?? []
  useEffect(() => {
    for (const j of jobs) {
      if (last.current.get(j.id) === false && j.done && document.hidden) desktopNotify(`${j.title || j.kind} ${j.err ? 'failed' : 'finished'}`, j.err)
      last.current.set(j.id, j.done)
    }
  }, [q.data]) // eslint-disable-line react-hooks/exhaustive-deps
  return { jobs, running: jobs.filter((j) => !j.done).length, refetch: q.refetch }
}

export function JobsDrawer({ sid, jobs, items, notify, onClose, refetch }: { sid: string; jobs: JobRow[]; items?: MenuItem[]; notify: (m: string) => void; onClose: () => void; refetch: () => void }) {
  // the same query useJobs runs (disabled here): only to read its error, so a failed /jobs is not shown as an empty list
  const error = useQuery({ queryKey: ['jobs', sid], queryFn: () => api<{ jobs: JobRow[] | null }>('GET', `/sessions/${sid}/jobs`), enabled: false }).error
  const fail = (m: string) => (e: unknown) => notify(`${m}: ${msg(e)}`)
  const stop = (j: JobRow) => api('POST', `/sessions/${sid}/jobs/${j.id}/cancel`).then(refetch, fail('Could not stop'))
  const dismiss = (j: JobRow) => api('DELETE', `/sessions/${sid}/jobs/${j.id}`).then(refetch, fail('Could not dismiss'))
  const get = async (j: JobRow) => { try { const f = await download(`/sessions/${sid}/jobs/${j.id}/file`); saveBlob(f.blob, f.name || j.file) } catch (e) { fail('Could not download')(e) } }
  // the buttons come from meta.menus.jobs (labels and order); entries only the terminal can do are greyed with the reason
  const menu = (items?.length ? items : [{ label: 'Stop', action: 'cancel' }, { label: 'Download', action: 'download', ui: 'web' }, { label: 'Dismiss', action: 'close' }])
  const show = (it: MenuItem, j: JobRow) => jobShows(it.action, j)
  // an action this drawer does not know does nothing (the menu is server-driven metadata)
  const run = (it: MenuItem, j: JobRow) => (it.action === 'cancel' ? stop(j) : it.action === 'download' ? get(j) : it.action === 'close' ? dismiss(j) : notify(`Unknown job action "${it.action}"`))
  return (
    <div className="w-[340px] flex-none border-l border-rule flex flex-col min-h-0 bg-canvas font-mono max-[760px]:fixed max-[760px]:z-30 max-[760px]:inset-0 max-[760px]:w-full" role="region" aria-label="Jobs" data-ctx="jobs">
      <div className="toolbar"><strong className="flex-1 min-w-0 font-medium">Jobs</strong>
        <button className="btn-ghost" disabled={!jobs.some((j) => j.done)} onClick={() => Promise.all(jobs.filter((j) => j.done).map((j) => api('DELETE', `/sessions/${sid}/jobs/${j.id}`).catch(() => {}))).then(refetch)}>Dismiss finished</button>
        <button className="btn-ghost" onClick={onClose} aria-label="Close">✕</button></div>
      <div className="overflow-auto flex-1">
        {error && <div className="p-3 text-[12.5px] text-del font-sans" role="alert">Could not load jobs: {msg(error)}</div>}
        {!jobs.length && !error && <div className="p-3 text-[12.5px] text-ink-2 font-sans">Imports, exports, backups, restores and searches show up here while they run.</div>}
        {[...jobs].sort((a, b) => b.created.localeCompare(a.created)).map((j) => (
          <div key={j.id} className="px-3 py-2.5 border-b border-rule text-[12px]">
            <div className="flex items-center gap-2"><span className="chip">{j.kind}</span><span className="flex-1 min-w-0 truncate" title={j.title}>{j.title || j.id}</span>
              <span className={j.err ? 'text-del' : j.done ? 'text-new' : 'text-ink-2'}>{j.err ? 'failed' : j.done ? 'done' : 'running'}</span></div>
            {!j.done && <div className="relative h-[3px] mt-2 bg-raised overflow-hidden" role="progressbar" aria-label={`${j.title} progress`} aria-valuenow={j.progress >= 0 ? Math.round(j.progress * 100) : undefined} aria-valuemin={0} aria-valuemax={100}>
              {j.progress >= 0 ? <div className="absolute inset-y-0 left-0 bg-accent" style={{ width: `${Math.min(100, j.progress * 100)}%` }} /> : <div className="absolute inset-y-0 w-2/5 bg-accent motion-safe:animate-slide" />}</div>}
            {j.err && <div className="mt-1.5 text-del wrap-anywhere">{j.err}</div>}
            {!!j.lines?.length && <pre className="m-0 mt-1.5 max-h-[5.5em] overflow-auto text-[11px] text-ink-2 whitespace-pre-wrap">{j.lines.slice(-4).join('\n')}</pre>}
            <div className="flex gap-1 mt-1.5">{menu.filter((it) => show(it, j)).map((it) => {
              const off = whyNot(it)
              return <button key={it.action} className="btn-ghost text-[11px] px-1.5" disabled={!!off} title={off} onClick={() => run(it, j)}>{it.label}</button>
            })}</div>
          </div>
        ))}
      </div>
    </div>
  )
}
