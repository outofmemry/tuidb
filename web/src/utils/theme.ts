import type { Meta } from './api'

// config [theme.colors] keys that have a counterpart in the web palette; the syntax keys (keyword, string,
// comment, number) have no CSS variable here yet, because the SQL editor takes its colours from CodeMirror.
const VARS: Record<string, string[]> = { accent: ['--accent'], error: ['--del'], ok: ['--new'], warn: ['--edit'], dim: ['--ink-2'], border: ['--rule-2'], selection: ['--raised'] }

// applyTheme sets data-theme from theme.name (the server resolves named palettes to dark | light | auto; auto follows
// prefers-color-scheme), the colour overrides, and the [web] font size and line height on the root.
export function applyTheme(t: Meta['theme'] | undefined, web?: Meta['web']) {
  const root = document.documentElement
  if (t?.Name === 'light' || t?.Name === 'dark') root.dataset.theme = t.Name
  else delete root.dataset.theme
  for (const [k, vars] of Object.entries(VARS)) {
    const v = t?.Colors?.[k]
    for (const name of vars) v ? root.style.setProperty(name, v) : root.style.removeProperty(name)
  }
  web?.fontSize ? root.style.setProperty('--fs', web.fontSize + 'px') : root.style.removeProperty('--fs')
  web?.lineHeight ? root.style.setProperty('--lh', web.lineHeight) : root.style.removeProperty('--lh')
}

// Desktop notification when a backup / import finishes; the permission is asked at the start (a user gesture).
export const notifyCfg = { on: false }
export function askNotify() { if (notifyCfg.on && typeof Notification !== 'undefined' && Notification.permission === 'default') Notification.requestPermission().catch(() => {}) }
export function desktopNotify(title: string, body = '') {
  if (notifyCfg.on && typeof Notification !== 'undefined' && Notification.permission === 'granted') { try { new Notification(title, { body }) } catch { /* not allowed here */ } }
}
