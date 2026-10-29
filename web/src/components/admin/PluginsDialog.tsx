// Plugins: run a plugin against the current table, selection or cell.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, type ObjRef, type PluginEffect, type PluginInfo } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { copyText } from '../../utils/copy'
import { ACTIONS, msg } from '../shell/ui'

export type PluginCtx = { context: string; object?: ObjRef; selection: string; cell: string }

export function PluginsDialog({ sid, ctx, onClose, onSql, notify }: { sid: string; ctx: PluginCtx; onClose: () => void; onSql: (t: string) => void; notify: (m: string) => void }) {
  const q = useQuery({ queryKey: ['plugins'], queryFn: () => api<PluginInfo[]>('GET', '/plugins'), gcTime: 0 })
  const [out, setOut] = useState<{ name: string; text: string } | null>(null)
  const [busy, setBusy] = useState('')
  const list = (q.data ?? []).filter((p) => !p.error && (!ctx.context || p.contexts.length === 0 || p.contexts.includes(ctx.context)))
  const run = async (p: PluginInfo) => {
    setBusy(p.name)
    try {
      const r = await api<{ output: string; text: string; effects?: PluginEffect[] }>('POST', `/sessions/${sid}/plugins/run`, { Name: p.name, Context: ctx.context, Object: ctx.object, Selection: ctx.selection, Cell: ctx.cell })
      // effects first (copy, open, notify, sql), then the text; a plugin that only has effects closes the dialog
      for (const ef of r.effects ?? []) {
        const kind = (ef.Kind ?? (ef as any).kind) as string, text = ef.Text ?? (ef as any).text ?? '', ref = ef.Ref ?? (ef as any).ref
        if (kind === 'copy') await copyText(text).then(() => notify('Copied'), (e) => notify(msg(e)))
        else if (kind === 'open') window.dispatchEvent(new CustomEvent('webtuidb:open', { detail: { obj: ref, newTab: true } }))
        else if (kind === 'notify') notify(text)
        else if (kind === 'sql') onSql(text)
      }
      if (r.output === 'sql' && r.text) { onSql(r.text); onClose() } else if (r.text) { setOut({ name: p.name, text: r.text }); setBusy('') } else onClose()
    } catch (e) { notify(msg(e)); setBusy('') }
  }
  return (
    <Dialog title={out ? out.name : 'Plugins'} onClose={onClose}>
      {out ? <pre>{out.text || '(no output)'}</pre> : (
        <>
          {q.isLoading && <div className="text-ink-2">Loading…</div>}
          {q.error && <div className="text-del">{q.error.message}</div>}
          {q.data && list.length === 0 && <div className="text-ink-2">No plugins for this place. Add a .toml file to the plugins folder in the config directory.</div>}
          <div className="flex flex-col gap-0.5">{list.map((p) => <button key={p.name} className="text-left border-transparent hover:border-ink-3" disabled={!!busy} onClick={() => run(p)}><span>{p.name}</span> <span className="text-ink-2">{busy === p.name ? 'running…' : p.output}</span>{p.js && <span className="chip ml-1.5" title="A JavaScript plugin">js</span>}{p.key && <kbd className="ml-1.5 text-ink-3 font-mono text-[11.5px]" title="Shortcut">{p.key}</kbd>}</button>)}</div>
        </>
      )}
      <div className={ACTIONS}>{out && <button onClick={() => setOut(null)}>Back</button>}<button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}
