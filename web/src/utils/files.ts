// Reading a SQL file in the browser: the file picker gives a page only a name, never a path, so a picked file is not added to the shared recent list;
// its name goes to this browser's own list (localRecent), and reopening it asks for the file again.
import { ApiError, api } from './api'

export function pickFile(accept = '.sql,text/plain'): Promise<File | null> {
  return new Promise((done) => {
    const i = document.createElement('input')
    i.type = 'file'; i.accept = accept
    i.onchange = () => done(i.files?.[0] ?? null)
    i.oncancel = () => done(null)
    i.click()
  })
}

const LOCAL = 'webtuidb-recent-files'
export const localRecent = (): string[] => { try { return JSON.parse(localStorage.getItem(LOCAL) || '[]') } catch { return [] } }

// openSqlFile asks for a .sql file.
export async function openSqlFile(): Promise<{ name: string; text: string } | null> {
  const f = await pickFile()
  if (!f) return null
  try { localStorage.setItem(LOCAL, JSON.stringify([f.name, ...localRecent().filter((n) => n !== f.name)].slice(0, 10))) } catch { /* private mode */ }
  return { name: f.name, text: await f.text() }
}

// openRecentFile reopens an entry of the recent list: the server reads a path the terminal UI stored; a bare name (all a browser knows) means asking for the file again.
export async function openRecentFile(entry: string): Promise<{ name: string; text: string } | null> {
  try {
    const r = await api<{ name: string; text: string }>('POST', '/files/open', { Path: entry })
    api('PUT', '/state/recent-files', { Name: entry }).catch(() => {}) // an absolute path from the server's own list: moves it to the front
    return r
  } catch { return openSqlFile() }
}

// revealPath asks the server to open a file's folder in its file manager (POST /reveal; the server only does it for a loopback request).
// A 403 means the browser is not on that machine: `otherwise` runs instead (a download, or copying the path). Returns true when the folder was opened.
export async function revealPath(path: string, otherwise?: () => unknown): Promise<boolean> {
  try { await api('POST', '/reveal', { path }); return true } catch (e) {
    if (e instanceof ApiError && e.status === 403 && otherwise) { await otherwise(); return false }
    throw e
  }
}
