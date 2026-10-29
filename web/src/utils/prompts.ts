// ask / confirm replace window.prompt and window.confirm with the app's own dialog. <Prompts/> (components/Dialogs.tsx) is
// mounted once at the root, registers itself here and renders whatever request is pending. This module imports nothing,
// so api.ts can ask too without importing the React components.
export type PromptReq = { kind: 'prompt' | 'confirm'; title: string; text?: string; init: string; secret?: boolean; ok?: string; no?: string; resolve: (v: string | null | boolean) => void }
let pending: ((r: PromptReq | null) => void) | null = null
export const setPromptHost = (f: typeof pending) => { pending = f }
export const ask = (title: string, init = '') => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init, resolve: resolve as PromptReq['resolve'] }))
export const askSecret = (title: string) => new Promise<string | null>((resolve) => pending?.({ kind: 'prompt', title, init: '', secret: true, resolve: resolve as PromptReq['resolve'] }))
// ok names the action on the primary button (Delete, Kill, Discard; without it the button says Continue); no names the other
// answer when it is a choice rather than a cancel ("Drop only this"). Escape and ✕ answer false, like that button.
export const confirm = (title: string, text?: string, ok?: string, no?: string) => new Promise<boolean>((resolve) => pending?.({ kind: 'confirm', title, text, ok, no, resolve: resolve as PromptReq['resolve'], init: '' }))
