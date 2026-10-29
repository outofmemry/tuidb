import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Outlet, RouterProvider, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import './styles.css'
import { api, ApiError, initToken, type Meta } from './utils/api'
import { applyTheme, notifyCfg } from './utils/theme'
import { Connections } from './routes/Connections'
import { Workspace } from './routes/Workspace'
import { Lock } from './components/shell/Lock'
import { parseFilter } from './utils/links'
import { SqlViewer } from './components/query/SqlViewer'
import { Prompts } from './components/shell/Dialogs'

initToken() // must run before the router reads the URL, so the token never stays in the address bar

const qc = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, retry: false } } })

const root = createRootRoute({ component: function Root() {
  const meta = useQuery({ queryKey: ['meta'], queryFn: () => api<Meta>('GET', '/meta'), staleTime: Infinity })
  if (meta.data) { applyTheme(meta.data.theme, meta.data.web); notifyCfg.on = !!meta.data.web?.notifications }
  if (meta.error instanceof ApiError && meta.error.status === 401 && meta.error.body.login) return <Login onDone={() => meta.refetch()} />
  // no token (a copied link opened in a fresh browser tab): nothing below can load, so say how to get in instead of mounting pages that poll the API
  if (meta.error instanceof ApiError && meta.error.status === 401) return (
    <div className="max-w-[560px] mx-auto mt-[18vh] px-4 flex flex-col gap-3 font-sans">
      <h1 className="m-0 text-[28px] font-medium font-mono">This link needs the access token</h1>
      <p className="m-0 text-ink-2 text-[15px] leading-normal">Open the address that <code className="font-mono">webtuidb --frontend</code> printed, then open this link in the same browser.</p>
    </div>
  )
  return <><Lock ready={!!meta.data || !!meta.error} on={!!meta.data?.security?.passcode} afterMins={meta.data?.security?.lockAfterMins ?? 0}><Outlet /><Prompts /></Lock></>
} })

// [web] password: the page asks for it before anything else; the server answers with a session cookie.
function Login({ onDone }: { onDone: () => void }) {
  const [pw, setPw] = useState(''), [err, setErr] = useState('')
  const go = async () => { try { await api('POST', '/login', { Password: pw }); onDone() } catch (e) { setErr(e instanceof Error ? e.message : String(e)) } }
  return (
    <form className="max-w-[360px] mx-auto mt-[18vh] px-4 flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); go() }}>
      <h1 className="m-0 text-[28px] font-medium font-mono">Sign in</h1>
      <input type="password" autoFocus aria-label="Password" placeholder="Password" value={pw} onChange={(e) => setPw(e.target.value)} />
      {err && <div className="text-del" role="alert">{err}</div>}
      <button className="btn-primary" type="submit" disabled={!pw}>Sign in</button>
    </form>
  )
}
const home = createRoute({ getParentRoute: () => root, path: '/', component: Connections })
const session = createRoute({
  getParentRoute: () => root, path: '/s/$sessionId',
  component: function SessionPage() { return <Workspace sid={session.useParams().sessionId} key={session.useParams().sessionId} /> },
})
// deep links: /s/<sid>/t/<schema>/<table>?filter=<json FilterSet> opens a table, /s/<sid>/q/<saved query path> opens a saved query
const tableLinkRoute = createRoute({
  getParentRoute: () => root, path: '/s/$sessionId/t/$schema/$table', validateSearch: (s: Record<string, unknown>) => ({ filter: s.filter as unknown }),
  component: function TablePage() {
    const p = tableLinkRoute.useParams(), { filter } = tableLinkRoute.useSearch()
    return <Workspace sid={p.sessionId} key={p.sessionId} target={{ kind: 't', schema: p.schema, table: p.table, filter: parseFilter(filter) }} />
  },
})
const queryLinkRoute = createRoute({
  getParentRoute: () => root, path: '/s/$sessionId/q/$',
  component: function QueryPage() {
    const p = queryLinkRoute.useParams() as { sessionId: string; _splat?: string; '*'?: string }
    return <Workspace sid={p.sessionId} key={p.sessionId} target={{ kind: 'q', path: p._splat ?? p['*'] ?? '' }} />
  },
})
const viewer = createRoute({ getParentRoute: () => root, path: '/sql', component: SqlViewer })
const router = createRouter({ routeTree: root.addChildren([home, session, tableLinkRoute, queryLinkRoute, viewer]) })
declare module '@tanstack/react-router' { interface Register { router: typeof router } }

// one React root per page: a hot update in development re-runs this module, and a second createRoot on the same element would stack a second app (and its key listeners) on the first
const el = document.getElementById('root') as HTMLElement & { _root?: ReturnType<typeof createRoot> }
;(el._root ??= createRoot(el)).render(
  <StrictMode><QueryClientProvider client={qc}><RouterProvider router={router} /></QueryClientProvider></StrictMode>,
)
