import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Outlet, RouterProvider, createRootRoute, createRoute, createRouter } from '@tanstack/react-router'
import { initToken } from './utils/api'
import { Connections } from './routes/Connections'
import { Workspace } from './routes/Workspace'
import { Prompts } from './components/Dialogs'
import './styles.css'

initToken() // must run before the router reads the URL, so the token never stays in the address bar

const qc = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false, retry: false } } })

const root = createRootRoute({ component: () => <><Outlet /><Prompts /></> })
const home = createRoute({ getParentRoute: () => root, path: '/', component: Connections })
const session = createRoute({
  getParentRoute: () => root, path: '/s/$sessionId',
  component: function SessionPage() { return <Workspace sid={session.useParams().sessionId} key={session.useParams().sessionId} /> },
})
const router = createRouter({ routeTree: root.addChildren([home, session]) })
declare module '@tanstack/react-router' { interface Register { router: typeof router } }

createRoot(document.getElementById('root')!).render(
  <StrictMode><QueryClientProvider client={qc}><RouterProvider router={router} /></QueryClientProvider></StrictMode>,
)
