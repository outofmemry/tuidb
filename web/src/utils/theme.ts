import type { Meta } from './api'

// config [theme.colors] keys and the CSS variables they set; the syntax keys reach the SQL editor through cmtheme.ts.
const VARS: Record<string, string[]> = { accent: ['--accent'], error: ['--del'], ok: ['--new'], warn: ['--edit'], dim: ['--ink-2'], border: ['--rule-2'], selection: ['--raised'], keyword: ['--syn-keyword'], string: ['--syn-string'], number: ['--syn-number'], comment: ['--syn-comment'],
  // the surfaces TablePlus themes; each falls back to the palette when unset (styles.css reads var(--x, fallback))
  editor_bg: ['--editor-bg'], window_bg: ['--window-bg'], sidebar_bg: ['--sidebar-bg'], tabs_bg: ['--tabs-bg'], tab_active_bg: ['--tab-active-bg'], notification_bg: ['--notification-bg'], grid_line: ['--grid-line'], cell_selection: ['--cell-selection'] }

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
  // grid_line and cell_selection are applied by an attribute, so the stylesheet only overrides the grid when the theme sets them
  for (const [k, attr] of [['grid_line', 'data-grid-line'], ['cell_selection', 'data-cell-selection']] as const) t?.Colors?.[k] ? root.setAttribute(attr, '') : root.removeAttribute(attr)
  root.classList.toggle('scrollbars-auto', !!web?.autoHideScrollbars)
  web?.fontSize ? root.style.setProperty('--fs', web.fontSize + 'px') : root.style.removeProperty('--fs')
  web?.lineHeight ? root.style.setProperty('--lh', web.lineHeight) : root.style.removeProperty('--lh')
}

// Desktop notification when a backup / import finishes; the permission is asked at the start (a user gesture).
export const notifyCfg = { on: false }
export function askNotify() { if (notifyCfg.on && typeof Notification !== 'undefined' && Notification.permission === 'default') Notification.requestPermission().catch(() => {}) }
export function desktopNotify(title: string, body = '') {
  if (notifyCfg.on && typeof Notification !== 'undefined' && Notification.permission === 'granted') { try { new Notification(title, { body }) } catch { /* not allowed here */ } }
}
