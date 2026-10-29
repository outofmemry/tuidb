// MCP activity: the log and approvals of the MCP server.
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { api, download, saveBlob } from '../../utils/api'
import { Dialog } from '../shell/Dialogs'
import { ACTIONS, TH, TD, msg } from '../shell/ui'

export function McpDialog({ onClose }: { onClose: () => void }) {
  const q = useQuery({ queryKey: ['mcplog'], queryFn: () => api<{ enabled: boolean; port?: number; listen?: string; tls?: boolean; entries: { time: string; token: string; tool: string; connection: string; sql: string; error: string }[] }>('GET', '/mcp/log'), gcTime: 0, refetchInterval: 3000 })
  const cl = useQuery({ queryKey: ['mcpclients'], queryFn: () => api<{ enabled: boolean; source: string; clients: { token: string; lastSeen: string; calls: number }[] }>('GET', '/mcp/clients'), gcTime: 0, refetchInterval: 3000 })
  const [err, setErr] = useState('')
  const d = q.data
  const csv = async () => { try { const f = await download('/mcp/log?format=csv'); saveBlob(f.blob, f.name) } catch (e) { setErr(msg(e)) } }
  return (
    <Dialog title="MCP activity" onClose={onClose}>
      {q.error && <div className="text-del">{q.error.message}</div>}
      {d && !d.enabled && <div className="text-ink-2">The MCP server is off. Set enabled = true in the [mcp] section of the config and restart.</div>}
      {d?.enabled && (
        <>
          <div className="flex items-center gap-3 text-ink-2 mb-2"><span className="flex-1 min-w-0">Listening on {d.listen ?? `127.0.0.1:${d.port}`} over {d.tls ? 'HTTPS (TLS)' : 'plain HTTP'}</span><button onClick={csv}>Export log as CSV</button></div>
          <h3 className="m-0 mt-3 mb-1 text-[12.5px] text-ink-2 font-medium">Connected clients</h3>
          {cl.data?.clients.length ? (
            <table className="text-[12px] font-mono border-collapse w-full mb-2">
              <thead><tr>{['Token', 'Requests', 'Last seen'].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
              <tbody>{cl.data.clients.map((c) => <tr key={c.token}><td className={TD}>{c.token}</td><td className={TD}>{c.calls}</td><td className={TD}>{new Date(c.lastSeen).toLocaleTimeString()}</td></tr>)}</tbody>
            </table>
          ) : <div className="text-ink-2 mb-2">No client called a tool in the last 10 minutes.</div>}
          <div className="text-ink-3 text-[12px] mb-2">Tools a client can call: list_connections, list_open_connections, open_connection, disconnect, execute_read_query, execute_query, list_saved_queries, get_saved_query, list_history, list_boards, run_board.</div>
          <div className="text-ink-3 text-[12px] mb-2">A client is a token seen in the activity log ({cl.data?.source ?? 'last 10 minutes'}). Calls are separate HTTP requests, so there is no session to disconnect: to cut a client off, remove its token from [mcp] in the config and restart.</div>
          <h3 className="m-0 mt-3 mb-1 text-[12.5px] text-ink-2 font-medium">Activity</h3>
          {d.entries.length === 0 ? <div className="text-ink-2">No tool calls yet.</div> : (
            <div className="overflow-auto max-h-[40vh] border border-rule">
              <table className="text-[12px] font-mono border-collapse w-full">
                <thead><tr>{['Time', 'Token', 'Tool', 'Connection', 'SQL / error'].map((h) => <th key={h} className={TH}>{h}</th>)}</tr></thead>
                <tbody>{[...d.entries].reverse().map((e, i) => (
                  <tr key={i}><td className={TD}>{e.time}</td><td className={TD}>{e.token}</td><td className={TD}>{e.tool}</td><td className={TD}>{e.connection}</td>
                    <td className={`${TD} max-w-[40ch] whitespace-pre-wrap break-words`}>{e.sql}{e.error && <div className="text-del">{e.error}</div>}</td></tr>))}</tbody>
              </table>
            </div>
          )}
        </>
      )}
      {err && <div className="text-del mt-2">{err}</div>}
      <div className={ACTIONS}><button className="btn-primary" onClick={onClose}>Close</button></div>
    </Dialog>
  )
}
