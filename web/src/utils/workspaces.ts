// What the browser tab remembers about workspaces: the open sessions (sessionStorage, so a reload comes back to them), each session's open
// tabs (M12.5), and the workspaces to reopen on the next start ([general] reopen_workspaces; localStorage, like the terminal's last session).
// shortcut: the "last workspaces" list lives in this browser's localStorage, so a different port or browser starts empty; a server-side state key is the upgrade.

const OPEN = 'webtuidb-open'
const TABS = 'webtuidb-tabs-'
const LAST = 'webtuidb-last'
const REOPENED = 'webtuidb-reopened'

const get = (s: Storage, k: string) => { try { return s.getItem(k) } catch { return null } }
const set = (s: Storage, k: string, v: string | null) => { try { v === null ? s.removeItem(k) : s.setItem(k, v) } catch { /* private mode */ } }
const json = <T,>(s: Storage, k: string, d: T): T => { try { return JSON.parse(get(s, k) || 'null') ?? d } catch { return d } }

// A stored value of the wrong shape (valid JSON, but not what this code wrote) is treated like a missing one.
export const asRecord = (v: unknown): Record<string, string> => (v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, string>) : {})
export const asSavedTabs = (v: unknown): SavedTabs | null => {
  const o = v as SavedTabs | null
  return o && typeof o === 'object' && Array.isArray(o.tabs) && Array.isArray(o.active) && o.active.length >= 2 ? { tabs: o.tabs.filter((t) => t && typeof t === 'object'), active: [Number(o.active[0]) || 0, Number(o.active[1]) || 0], split: !!o.split } : null
}

export const openSessions = (): Record<string, string> => asRecord(json(sessionStorage, OPEN, {}))
export function remember(name: string, sid: string) { const o = openSessions(); if (o[name] !== sid) { o[name] = sid; set(sessionStorage, OPEN, JSON.stringify(o)) } }
export function forget(sid: string) { const o = openSessions(); for (const k in o) if (o[k] === sid) delete o[k]; set(sessionStorage, OPEN, JSON.stringify(o)); set(sessionStorage, TABS + sid, null) }

// open tabs of one session, restored after a reload
export type SavedTabs = { tabs: Record<string, unknown>[]; active: [number, number]; split: boolean }
export const loadTabs = (sid: string): SavedTabs | null => asSavedTabs(json<unknown>(sessionStorage, TABS + sid, null))
// A tab whose first line is `-- sensitive` is never persisted (the documented opt-out), and one tab's text is capped at 64 KB.
export const MAX_TAB_TEXT = 64 * 1024
export const persistable = (t: Record<string, unknown>): boolean => typeof t.text !== 'string' || !/^\s*--\s*sensitive\b/i.test(t.text.split('\n', 1)[0])
export const capText = (t: Record<string, unknown>): Record<string, unknown> => (typeof t.text === 'string' && t.text.length > MAX_TAB_TEXT ? { ...t, text: t.text.slice(0, MAX_TAB_TEXT) } : t)
export function saveTabs(sid: string, v: SavedTabs) {
  const keep = v.tabs.map(persistable) // dropping a tab shifts the indexes of the active ones
  const at = (i: number) => Math.max(0, keep.slice(0, i).filter(Boolean).length)
  set(sessionStorage, TABS + sid, JSON.stringify({ ...v, tabs: v.tabs.filter((_, i) => keep[i]).map(capText), active: [at(v.active[0]), at(v.active[1])] }))
}

// connection name -> database it was showing, in the order they were opened
export const lastWorkspaces = (): Record<string, string> => asRecord(json(localStorage, LAST, {}))
export function noteWorkspace(name: string, database: string) { const l = lastWorkspaces(); if (l[name] !== database) { l[name] = database; set(localStorage, LAST, JSON.stringify(l)) } }
export function dropWorkspace(name: string) { const l = lastWorkspaces(); if (name in l) { delete l[name]; set(localStorage, LAST, JSON.stringify(l)) } }
// the start page reopens the last workspaces once per browser tab
export const reopenDue = () => get(sessionStorage, REOPENED) === null && Object.keys(openSessions()).length === 0
export const markReopened = () => set(sessionStorage, REOPENED, '1')
