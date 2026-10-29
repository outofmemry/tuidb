// Pure helpers of the ER diagram (foreign keys, colour overrides), tested without a browser.
import type { TableDef, ForeignKey } from './tabledef'

// withForeignKey is the "new" side of a structure plan that adds one foreign key to the table as loaded (old stays untouched).
export function withForeignKey(old: TableDef, fk: ForeignKey): TableDef {
  if (!fk.Columns.length || !fk.RefTable || !fk.RefColumns.length) throw new Error('A foreign key needs columns, a referenced table and referenced columns')
  if (fk.Columns.length !== fk.RefColumns.length) throw new Error(`${fk.Columns.length} column(s) cannot reference ${fk.RefColumns.length}`)
  const have = new Set(old.Columns.map((c) => c.Name))
  const bad = fk.Columns.find((c) => !have.has(c))
  if (bad) throw new Error(`The table has no column ${bad}`)
  return { ...old, Columns: old.Columns.map((c) => ({ ...c })), ForeignKeys: [...old.ForeignKeys, fk] }
}

// colour overrides of the diagram: stored as {"schema.table": "#rrggbb"}; an override is only used when it is a hex colour
export const validColour = (c: string | undefined) => (c && /^#[0-9a-fA-F]{6}$/.test(c) ? c : undefined)
