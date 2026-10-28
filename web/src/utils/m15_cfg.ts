// The config keys the web honours (M14 check 3). /api/meta carries them as meta.table / general / editor / sidebar in camelCase;
// older servers only have meta.config.<section>.<PascalCase>, so read both.
type Any = Record<string, any>
const pascal = (k: string) => k[0].toUpperCase() + k.slice(1)
export function cfg<T = unknown>(meta: unknown, section: 'table' | 'general' | 'editor' | 'sidebar', key: string, dflt?: T): T {
  const m = meta as Any
  const v = m?.[section]?.[key] ?? m?.config?.[section]?.[key] ?? m?.config?.[section]?.[pascal(key)]
  return (v === undefined || v === null ? dflt : v) as T
}
