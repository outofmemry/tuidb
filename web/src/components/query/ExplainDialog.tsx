// Explain as a tree. Plans come from the /explain endpoint.
import { useQuery } from '@tanstack/react-query'
import { api } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { ACTIONS, msg } from '../shell/ui'

type Node = { depth: number; label: string; cost?: string; rows?: string }

export function ExplainDialog({ sid, body, onClose }: { sid: string; body: { Text: string; Current?: boolean; Offset?: number }; onClose: () => void }) {
  const q = useQuery({ queryKey: ['explain', sid, body], queryFn: () => api<{ statement: string; nodes: Node[] }>('POST', `/sessions/${sid}/explain`, body), gcTime: 0, retry: false })
  return (
    <Dialog title="Explain" onClose={onClose}>
      {q.isLoading && <div className="text-ink-2">Planning…</div>}
      {q.error && <div className="text-del whitespace-pre-wrap">{msg(q.error)}</div>}
      {q.data && <>
        <pre className="mb-3">{q.data.statement}</pre>
        <div className="font-mono text-[12.5px] max-h-[50vh] overflow-auto" role="tree" aria-label="Query plan">
          {q.data.nodes.map((n, i) => (
            <div key={i} role="treeitem" aria-level={n.depth + 1} className="flex gap-3 py-0.5 border-b border-rule" style={{ paddingLeft: n.depth * 18 }}>
              <span className="flex-1 min-w-0 wrap-anywhere"><span className="text-ink-3">{n.depth ? '└ ' : ''}</span>{n.label}</span>
              {n.cost && <span className="text-ink-2 whitespace-nowrap">cost {n.cost}</span>}
              {n.rows && <span className="text-ink-2 whitespace-nowrap">rows {n.rows}</span>}
            </div>
          ))}
          {!q.data.nodes.length && <div className="text-ink-2">The plan is empty.</div>}
        </div>
      </>}
      <div className={ACTIONS}><button className="btn-primary" autoFocus onClick={onClose}>Close</button></div>
    </Dialog>
  )
}
