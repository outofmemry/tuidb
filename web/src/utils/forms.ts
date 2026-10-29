// Dialog form helpers (Enter to submit, limit hints, CSV preview size).


// enterSubmits runs fn when Enter is pressed in a single-line text field of a dialog (not a button, checkbox, select or textarea).
export function enterSubmits(e: { key: string; target: EventTarget | null; defaultPrevented?: boolean; preventDefault: () => void }, fn: () => void): boolean {
  const t = e.target as { tagName?: string; type?: string } | null
  if (e.key !== 'Enter' || e.defaultPrevented || t?.tagName !== 'INPUT' || !['text', 'number', 'search', 'password', 'email', ''].includes(t.type ?? '')) return false
  e.preventDefault(); fn(); return true
}

// limitHint: 0 means unlimited for the per-hour limits but "use the server's max_user_connections" for MAX_USER_CONNECTIONS (Postgres: -1 = no limit)
export const limitHint = (key: string, pg: boolean) => (pg ? '-1 = no limit' : key === 'MAX_USER_CONNECTIONS' ? '0 = the server default' : '0 = unlimited')

// previewLength is how many bytes of a CSV prefix to upload for the preview: up to and including the last newline,
// so a slice that cuts the last row does not fail it (a short row is an error). The first row is always kept.
export function previewLength(bytes: Uint8Array): number {
  const i = bytes.lastIndexOf(10)
  return i < 0 ? bytes.length : i + 1
}
