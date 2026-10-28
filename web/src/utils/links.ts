// Pure helpers for in-app links (M15.19 / M15.20): the token fragment and the deep-link paths. No browser globals, so node can test them.

// linkWithToken adds `#token=<token>` to a link so a second browser tab signs in; a link that has the token already is kept as it is.
export function linkWithToken(url: string, token: string): string {
  if (!token || /[#&]token=/.test(url)) return url
  const at = url.indexOf('#')
  if (at < 0) return `${url}#token=${token}`
  return url.endsWith('#') ? `${url}token=${token}` : `${url}&token=${token}`
}

const enc = (s: string) => encodeURIComponent(s)

// tableLink: /s/<sid>/t/<schema>/<table>?filter=<json FilterSet>; a schema-less engine uses "-" for the schema segment.
export function tableLink(sid: string, schema: string, table: string, filter?: unknown): string {
  return `/s/${enc(sid)}/t/${enc(schema || '-')}/${enc(table)}` + (filter ? `?filter=${enc(JSON.stringify(filter))}` : '')
}

// queryLink: /s/<sid>/q/<saved query path>, the path keeping its slashes.
export const queryLink = (sid: string, path: string) => `/s/${enc(sid)}/q/${path.split('/').map(enc).join('/')}`

// The filter-column event names its tab (session + table key); a FilterBar answers only its own, so hidden table tabs do not gain a row.
export const forThisTab = (detail: { sid?: string; key?: string } | undefined, sid: string, key: string): boolean => !!detail && detail.sid === sid && detail.key === key

export type Filters = { Filters: { Column: string; Op: string; Value: string; Enabled: boolean }[]; Or: boolean }

// parseFilter reads the ?filter= value; anything that is not a FilterSet is ignored (a hand-edited link must not break the page).
export function parseFilter(raw: unknown): Filters | undefined {
  try {
    const v = typeof raw === 'string' ? JSON.parse(raw) : raw
    if (!v || !Array.isArray(v.Filters)) return undefined
    const fs = v.Filters.filter((f: any) => f && typeof f.Column === 'string' && typeof f.Op === 'string').map((f: any) => ({ Column: f.Column, Op: f.Op, Value: String(f.Value ?? ''), Enabled: f.Enabled !== false }))
    return fs.length ? { Filters: fs, Or: !!v.Or } : undefined
  } catch { return undefined }
}
