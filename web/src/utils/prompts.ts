// ask / confirm replace window.prompt and window.confirm with the app's own dialog. <Prompts/> (components/Dialogs.tsx) is
// mounted once at the root, registers itself here and renders whatever request is pending. This module imports nothing,
// so api.ts can ask too without importing the React components.
export type PromptReq = { kind: 'prompt' | 'confirm'; title: string; text?: string; init: string; secret?: boolean; match?: string; ok?: string; no?: string; resolve: (v: string | null | boolean) => void }
let pending: ((r: PromptReq | null) => void) | null = null
export const setPromptHost = (f: typeof pending) => { pending = f }
export const ask = (title: string, init = '') => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init, resolve: resolve as PromptReq['resolve'] }))
export const askSecret = (title: string) => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init: '', secret: true, resolve: resolve as PromptReq['resolve'] }))
// ok names the action on the primary button (Delete, Kill, Discard; without it the button says Continue); no names the other
// answer when it is a choice rather than a cancel ("Drop only this"). Escape and ✕ answer false, like that button.
export const confirm = (title: string, text?: string, ok?: string, no?: string) => new Promise<boolean>((resolve) => pending?.({ kind: 'confirm', title, text, ok, no, resolve: resolve as PromptReq['resolve'], init: '' }))

// ask with match: the answer is only accepted when it is typed exactly (Enter does nothing until then). The destructive
// dialogs use it to make the user type the table's name or the number of tables, like the terminal's forms.
export const askMatch = (title: string, match: string, text?: string) => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, text, init: '', match, resolve: resolve as PromptReq['resolve'] }))
// typedOk: may the prompt be answered with val? Without match any non-blank text; with match exactly that text.
export const typedOk = (val: string, match?: string) => match === undefined ? val.trim() !== '' : val === match
// isDanger: a confirm whose button names a destructive action is shown like the terminal's red dialogs: Cancel has the focus, so Enter
// (a reflex) cancels and the action needs a click or its own key. Match on the start of the button label.
const DANGER = /^(delete|drop|truncate|discard|kill|clear|roll back|overwrite|replace|close|reset|remove|open and run|run)\b/i
export const isDanger = (ok?: string) => !!ok && DANGER.test(ok)
// enterAnswers: does Enter on the dialog's focused field answer it? A prompt needs a complete typed answer; a confirm leaves Enter
// to the focused button (Cancel on a danger dialog).
export const enterAnswers = (kind: 'prompt' | 'confirm', val: string, match?: string) => kind === 'prompt' && typedOk(val, match)
