// Editor folds keyed by statement text (M13.8): the server keeps statement indexes, the client also keeps the FNV-1a 32 hash of each
// folded statement's trimmed text, so an edit above (or a statement added) leaves the fold on the same statement.
export function fnv1a32(s: string): string {
  let h = 0x811c9dc5
  for (const b of new TextEncoder().encode(s)) { h ^= b; h = Math.imul(h, 0x01000193) >>> 0 }
  return h.toString(16).padStart(8, '0')
}
export const stmtHash = (text: string) => fnv1a32(text.trim())
// foldedIndexes: the statements whose hash was folded before (identical statements share a hash, like the terminal)
export function foldedIndexes(hashes: string[], stmts: string[]): number[] {
  const want = new Set(hashes)
  return stmts.flatMap((t, i) => (want.has(stmtHash(t)) ? [i] : []))
}
const lsKey = (key: string) => 'webtuidb-folds:' + key
export function loadHashes(key: string): string[] | null {
  try { const v = JSON.parse(localStorage.getItem(lsKey(key)) ?? 'null'); return Array.isArray(v) ? v : null } catch { return null }
}
export function saveHashes(key: string, h: string[]) { try { localStorage.setItem(lsKey(key), JSON.stringify(h)) } catch { /* private mode */ } }
