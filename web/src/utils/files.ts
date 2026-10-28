// Reading a SQL file in the browser: the file picker gives a page only a name, never a path, so a picked file is not added to the shared recent list.
import { api } from './api'

export function pickFile(accept = '.sql,text/plain'): Promise<File | null> {
  return new Promise((done) => {
    const i = document.createElement('input')
    i.type = 'file'; i.accept = accept
    i.onchange = () => done(i.files?.[0] ?? null)
    i.oncancel = () => done(null)
    i.click()
  })
}

// openSqlFile asks for a .sql file.
export async function openSqlFile(): Promise<{ name: string; text: string } | null> {
  const f = await pickFile()
  if (!f) return null
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
