// "Copy as" formats for rows and cells, shared by the data grid and query results.
import type { Meta, ResultCol } from './api'

export const COPY_FORMATS = ['Plain', 'JSON', 'HTML', 'Markdown Table', 'CSV', 'CSV with Header', 'INSERT Statement'] as const
export type CopyFormat = (typeof COPY_FORMATS)[number]
type Cells = (string | null)[][]

// the same rule as export.CSVField: quote_mode "always" quotes every non-empty field, otherwise only fields that need it
const csvLine = (cells: (string | null)[], c: Meta['config']['csv']) => {
  const q = c.Quote || '"', d = c.Delimiter || ','
  return cells.map((v) => {
    const t = v ?? ''
    return (c.QuoteMode === 'always' && t !== '') || t.includes(d) || t.includes(q) || /[\r\n]/.test(t) ? q + t.split(q).join(q + q) + q : t
  }).join(d)
}
const esc = (s: string) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
const md = (s: string | null) => (s ?? 'NULL').replace(/\|/g, '\\|').replace(/\r?\n/g, ' ')

export function formatRows(fmt: CopyFormat, cols: ResultCol[], rows: Cells, csv: Meta['config']['csv'], dialect: string, table = 'table_name'): string {
  const names = cols.map((c) => c.name)
  const nl = csv.LineBreak || '\n'
  switch (fmt) {
    case 'Plain': return rows.map((r) => r.map((v) => v ?? 'NULL').join('\t')).join('\n')
    case 'JSON': return JSON.stringify(rows.map((r) => Object.fromEntries(names.map((n, i) => [n, r[i]]))), null, 2)
    case 'HTML': return `<table>\n<tr>${names.map((n) => `<th>${esc(n)}</th>`).join('')}</tr>\n${rows.map((r) => `<tr>${r.map((v) => `<td>${v === null ? '' : esc(v)}</td>`).join('')}</tr>`).join('\n')}\n</table>`
    case 'Markdown Table': return [`| ${names.map(md).join(' | ')} |`, `| ${names.map(() => '---').join(' | ')} |`, ...rows.map((r) => `| ${r.map(md).join(' | ')} |`)].join('\n')
    case 'CSV': return rows.map((r) => csvLine(r, csv)).join(nl)
    case 'CSV with Header': return [csvLine(names, csv), ...rows.map((r) => csvLine(r, csv))].join(nl)
    case 'INSERT Statement': {
      const q = (id: string) => (dialect === 'mysql' || dialect === 'mariadb' || dialect === 'clickhouse' ? '`' + id.replace(/`/g, '``') + '`' : '"' + id.replace(/"/g, '""') + '"')
      const bs = dialect === 'mysql' || dialect === 'mariadb' || dialect === 'clickhouse' // these read \ as an escape, like db.Bind
      const lit = (v: string | null) => (v === null ? 'NULL' : "'" + (bs ? v.replace(/\\/g, '\\\\') : v).replace(/'/g, "''") + "'")
      return rows.map((r) => `INSERT INTO ${q(table)} (${names.map(q).join(', ')}) VALUES (${r.map(lit).join(', ')});`).join('\n')
    }
  }
}

export const copyText = (t: string) => navigator.clipboard.writeText(t)
