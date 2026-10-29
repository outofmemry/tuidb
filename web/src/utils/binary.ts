// Bytes helpers for the quick look: hex / base64 views and the image / PDF sniff.
export const isBinCell = (v: string | null) => v !== null && /^\\x([0-9a-f]{2})*$/i.test(v)
export const bytesOfHex = (hex: string) => { const h = hex.replace(/\s+/g, ''); return /^([0-9a-f]{2})*$/i.test(h) ? Uint8Array.from(h.match(/../g) ?? [], (x) => parseInt(x, 16)) : null }
export const hexOfBytes = (b: Uint8Array, sep = ' ') => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join(sep)
export function base64Of(b: Uint8Array): string {
  let s = ''
  for (let i = 0; i < b.length; i += 0x8000) s += String.fromCharCode(...b.subarray(i, i + 0x8000))
  return btoa(s)
}
export function bytesOfBase64(t: string): Uint8Array | null {
  try { return Uint8Array.from(atob(t.replace(/\s+/g, '')), (c) => c.charCodeAt(0)) } catch { return null }
}
const startsWith = (b: Uint8Array, sig: number[], at = 0) => sig.every((x, i) => b[at + i] === x)
// sniff recognises png, jpeg, gif, webp and pdf by their magic bytes
export function sniff(b: Uint8Array): { kind: 'image' | 'pdf'; mime: string } | null {
  if (startsWith(b, [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a])) return { kind: 'image', mime: 'image/png' }
  if (startsWith(b, [0xff, 0xd8, 0xff])) return { kind: 'image', mime: 'image/jpeg' }
  if (startsWith(b, [0x47, 0x49, 0x46, 0x38])) return { kind: 'image', mime: 'image/gif' }
  if (startsWith(b, [0x52, 0x49, 0x46, 0x46]) && startsWith(b, [0x57, 0x45, 0x42, 0x50], 8)) return { kind: 'image', mime: 'image/webp' }
  if (startsWith(b, [0x25, 0x50, 0x44, 0x46, 0x2d])) return { kind: 'pdf', mime: 'application/pdf' }
  return null
}
// openInWindow shows text (a cell, SQL) in a window of its own: a blob document, so nothing is sent anywhere
export function openInWindow(title: string, text: string) {
  const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  const html = `<!doctype html><meta charset="utf-8"><title>${esc(title)}</title><body style="margin:0;background:#1a1b26;color:#c0caf5"><pre style="margin:0;padding:16px;font:13px/1.5 ui-monospace,monospace;white-space:pre-wrap;word-break:break-word">${esc(text)}</pre>`
  const url = URL.createObjectURL(new Blob([html], { type: 'text/html;charset=utf-8' }))
  window.open(url, '_blank', 'popup,width=800,height=600')
  setTimeout(() => URL.revokeObjectURL(url), 60_000)
}
