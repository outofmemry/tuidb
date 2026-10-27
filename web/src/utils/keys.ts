// The tmux-style prefix state machine, mirroring internal/keymap in Go. Bindings and the prefix
// come from GET /api/meta, so a user's [general] prefix and [keys] overrides apply to both UIs.

export type Binding = { Action: string; Key: string; Context: string; NoPrefix: boolean; Desc: string }
export type Keymap = { prefix: string; bindings: Binding[] }

const NAMED: Record<string, string> = {
  Enter: 'enter', Tab: 'tab', ' ': 'space', Escape: 'esc', Backspace: 'backspace', Delete: 'delete', Insert: 'insert',
  ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right', Home: 'home', End: 'end', PageUp: 'pgup', PageDown: 'pgdown',
}

// US-layout characters for punctuation keys, [plain, shifted]. With Ctrl or Alt held (Option on a Mac) e.key is a
// composed character ("Æ" for Option+Shift+'), so the physical key is what matches the TUI's "alt+\"".
const PUNCT: Record<string, [string, string]> = {
  Quote: ["'", '"'], Period: ['.', '>'], Comma: [',', '<'], Semicolon: [';', ':'], Slash: ['/', '?'], Backslash: ['\\', '|'],
  BracketLeft: ['[', '{'], BracketRight: [']', '}'], Minus: ['-', '_'], Equal: ['=', '+'], Backquote: ['`', '~'],
}

// Shifted digit row, folded like PUNCT so "alt+shift+1" and the TUI's "alt+!" are one key.
const DIGIT_SHIFT: Record<string, string> = { '1': '!', '2': '@', '3': '#', '4': '$', '5': '%', '6': '^', '7': '&', '8': '*', '9': '(', '0': ')' }

// canon turns a browser key event into the canonical string the Go side uses ("ctrl+d", "alt+enter", "P").
// Cmd on a Mac is left alone so the browser's own shortcuts keep working; null means "not a key we name".
export function canon(e: KeyboardEvent): string | null {
  if (e.metaKey || e.key === 'Shift' || e.key === 'Control' || e.key === 'Alt' || e.key === 'Meta') return null
  let base = NAMED[e.key]
  let shift = e.shiftKey
  if (!base && /^F([1-9]|1[0-9]|2[0-4])$/.test(e.key)) base = e.key.toLowerCase()
  if (!base && (e.ctrlKey || e.altKey) && /^(Key|Digit)/.test(e.code)) {
    // with Ctrl or Alt held, e.key may be a dead or composed character; the physical key is what the TUI sees
    base = e.code.slice(e.code.startsWith('Key') ? 3 : 5).toLowerCase()
    // the TUI reports Option+Shift+k as a capital letter: "alt+K", not "alt+shift+k"
    if (e.altKey && !e.ctrlKey && shift && e.code.startsWith('Key')) { base = base.toUpperCase(); shift = false }
    if (shift && e.code.startsWith('Digit') && DIGIT_SHIFT[base]) { base = DIGIT_SHIFT[base]; shift = false }
  } else if (!base && (e.ctrlKey || e.altKey) && PUNCT[e.code]) {
    base = PUNCT[e.code][shift ? 1 : 0]
    shift = false // the character already carries the shift
  } else if (!base && [...e.key].length === 1) {
    base = e.key
    shift = false // the character already carries the shift ("P", "{", "?")
  }
  if (!base) return null
  const mods = [e.ctrlKey && 'ctrl', e.altKey && 'alt', shift && 'shift'].filter(Boolean)
  return [...mods, base].join('+')
}

// display is the TUI's spelling for the which-key overlay: "ctrl+d" -> "C-d".
export function display(k: string): string {
  if (k === '+') return '+'
  const parts = k.split('+')
  let last = parts.pop() || '+'
  if (last.length > 1) last = last[0].toUpperCase() + last.slice(1)
  return parts.map((p) => ({ ctrl: 'C-', alt: 'M-', shift: 'S-' })[p] ?? '').join('') + last
}

// isPrintable: a single character with no ctrl or alt, which is what a text field types.
export const isPrintable = (k: string): boolean => [...k].length === 1

// inTextField: the focused element takes typed text.
export function inTextField(el: Element | null = typeof document !== 'undefined' ? document.activeElement : null): boolean {
  const h = el as HTMLElement | null
  if (!h) return false
  if (h.isContentEditable) return true
  if (h.tagName === 'TEXTAREA') return true
  return h.tagName === 'INPUT' && !/^(checkbox|radio|button|submit|reset|file|range|color|image)$/i.test((h as HTMLInputElement).type)
}

export type Fed = { action?: string; pending?: boolean; passthrough?: boolean; unknown?: boolean }

export class Machine {
  pending = false
  map: Keymap
  constructor(map: Keymap) { this.map = map }
  lookup(ctx: string, key: string, noPrefix: boolean): string | undefined {
    for (const c of ctx === 'global' ? ['global'] : [ctx, 'global']) {
      const b = this.map.bindings.find((b) => b.NoPrefix === noPrefix && b.Context === c && b.Key === key)
      if (b) return b.Action
    }
  }
  // Valid keys after the prefix in ctx (the focused context shadows global), sorted like the TUI's which-key.
  bindings(ctx: string): Binding[] {
    const taken = new Set<string>()
    const out: Binding[] = []
    for (const c of ctx === 'global' ? ['global'] : [ctx, 'global']) {
      for (const b of this.map.bindings) {
        if (b.NoPrefix || b.Context !== c || taken.has(b.Key)) continue
        taken.add(b.Key); out.push(b)
      }
    }
    return out.sort((a, b) => (a.Key < b.Key ? -1 : a.Key > b.Key ? 1 : 0))
  }
  // typing: a text field has focus. A bare printable key is then text, never a no-prefix shortcut ("/" in the console find box).
  feed(ctx: string, key: string, typing = false): Fed {
    if (this.pending) {
      this.pending = false
      if (key === 'esc') return {}
      if (key === this.map.prefix) return { passthrough: true } // prefix twice sends the raw key, like tmux
      const a = this.lookup(ctx, key, false)
      return a ? { action: a } : { unknown: true }
    }
    if (key === this.map.prefix) { this.pending = true; return { pending: true } }
    if (typing && isPrintable(key)) return { passthrough: true }
    const a = this.lookup(ctx, key, true)
    return a ? { action: a } : { passthrough: true }
  }
}

// keysLocked: the lock screen is up (Lock sets data-locked), so no shortcut may run, whatever order the listeners were added in.
export const keysLocked = (): boolean => typeof document !== 'undefined' && !!document.documentElement.dataset.locked

// contextOf finds the focused TUI context from the DOM: the nearest data-ctx around the focused element,
// else around the last thing clicked (grid cells are not focusable), else global.
let lastClick: Element | null = null
if (typeof window !== 'undefined') window.addEventListener('pointerdown', (e) => { lastClick = e.target as Element }, true)
export function contextOf(): string {
  for (const el of [document.activeElement, lastClick]) {
    const c = el?.closest?.('[data-ctx]')?.getAttribute('data-ctx')
    if (c && document.contains(el!)) return c
  }
  return 'global'
}

// moveFocus moves keyboard focus to the nearest visible data-ctx region in a direction (sidebar, editor, results, grid, row detail, console).
export function moveFocus(dir: 'left' | 'right' | 'up' | 'down'): boolean {
  const regions = [...document.querySelectorAll<HTMLElement>('[data-ctx]')].filter((el) => el.getClientRects().length > 0)
  const here = [document.activeElement, lastClick].map((el) => el?.closest?.('[data-ctx]') as HTMLElement | null | undefined).find((el) => el && regions.includes(el))
  const c = (el: HTMLElement) => { const r = el.getBoundingClientRect(); return [r.left + r.width / 2, r.top + r.height / 2] }
  const [fx, fy] = here ? c(here) : [0, 0]
  const horiz = dir === 'left' || dir === 'right', sign = dir === 'left' || dir === 'up' ? -1 : 1
  let best: HTMLElement | null = null, score = Infinity
  for (const el of regions) {
    if (el === here || (here && (el.contains(here) || here.contains(el)))) continue
    const [x, y] = c(el), along = ((horiz ? x - fx : y - fy)) * sign, across = Math.abs(horiz ? y - fy : x - fx)
    if (along <= 1) continue
    const sc = along + 2 * across
    if (sc < score) { score = sc; best = el }
  }
  if (!best) return false
  const target = best.matches('[tabindex]') ? best : best.querySelector<HTMLElement>('.cm-content, input, textarea, [tabindex="0"], button')
  if (!target) return false
  target.focus(); lastClick = target
  return true
}
