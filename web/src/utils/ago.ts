// "3 hours ago" for the last-connected time on the connection list; an empty or unparsable time gives "".
export function ago(iso: string | undefined, now = Date.now()): string {
  const t = iso ? Date.parse(iso) : NaN
  if (Number.isNaN(t)) return ''
  const s = Math.max(0, Math.round((now - t) / 1000))
  if (s < 60) return 'just now'
  const [n, unit] = s < 3600 ? [Math.floor(s / 60), 'minute'] : s < 86400 ? [Math.floor(s / 3600), 'hour'] : s < 86400 * 60 ? [Math.floor(s / 86400), 'day'] : [Math.floor(s / (86400 * 30)), 'month']
  return `${n} ${unit}${n === 1 ? '' : 's'} ago`
}

// recentRank orders connections for the "Recent" sort: newest connect first, never connected last.
export const recentRank = (iso: string | undefined) => { const t = iso ? Date.parse(iso) : NaN; return Number.isNaN(t) ? 0 : t }
