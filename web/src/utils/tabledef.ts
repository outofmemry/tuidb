// The structure editor's draft and the TableDef it posts (db.TableDef JSON, Go field names). Mirrors oldDef / newDef in internal/ui/struct_view.go.
// Pure, so it is tested without a browser.

export type ForeignKey = { Name: string; Columns: string[]; RefSchema: string; RefTable: string; RefColumns: string[]; OnUpdate: string; OnDelete: string }
export type DefColumn = { Name: string; OrigName: string; Type: string; Nullable: boolean; Default: string; HasDef: boolean; PK: boolean; Extra: string; FK: null }
export type DefIndex = { Name: string; Columns: string[]; Unique: boolean; Primary: boolean; Algorithm: string; Condition: string }
export type TableDef = { Schema: string; Name: string; Columns: DefColumn[]; PrimaryKey: string[]; Indexes: DefIndex[]; ForeignKeys: ForeignKey[] }

export const FK_ACTIONS = ['NO ACTION', 'RESTRICT', 'CASCADE', 'SET NULL', 'SET DEFAULT']

// the draft rows: `orig` is the server's name ('' = added here), `gone` marks a delete
export type DraftCol = { orig: string; name: string; type: string; nullable: boolean; def: string; pk: boolean; extra: string; gone?: boolean }
export type DraftIdx = { orig: string; name: string; cols: string; unique: boolean; algorithm: string; condition: string; gone?: boolean }
export type DraftFk = { orig: string; name: string; cols: string; refSchema: string; refTable: string; refCols: string; onUpdate: string; onDelete: string; gone?: boolean }
export type Draft = { name: string; cols: DraftCol[]; idx: DraftIdx[]; fks: DraftFk[] }

export const list = (s: string) => s.split(',').map((x) => x.trim()).filter(Boolean)

type ServerCol = { Name: string; Type: string; Nullable: boolean; Default: string; HasDef: boolean; PK: boolean; Extra: string }

// draftOf turns what the server sent into the editable draft (primary-key indexes are the PK checkboxes, not index rows)
export function draftOf(name: string, cols: ServerCol[], idx: Partial<DefIndex>[], fks: Partial<ForeignKey>[]): Draft {
  return {
    name,
    cols: cols.map((c) => ({ orig: c.Name, name: c.Name, type: c.Type, nullable: c.Nullable, def: c.HasDef ? c.Default : '', pk: c.PK, extra: c.Extra ?? '' })),
    idx: idx.filter((i) => !i.Primary).map((i) => ({ orig: i.Name ?? '', name: i.Name ?? '', cols: (i.Columns ?? []).join(', '), unique: !!i.Unique, algorithm: i.Algorithm ?? '', condition: i.Condition ?? '' })),
    fks: fks.map((f) => ({ orig: f.Name ?? '', name: f.Name ?? '', cols: (f.Columns ?? []).join(', '), refSchema: f.RefSchema ?? '', refTable: f.RefTable ?? '', refCols: (f.RefColumns ?? []).join(', '), onUpdate: f.OnUpdate ?? '', onDelete: f.OnDelete ?? '' })),
  }
}

const fkOf = (f: DraftFk): ForeignKey => ({ Name: f.name.trim(), Columns: list(f.cols), RefSchema: f.refSchema.trim(), RefTable: f.refTable.trim(), RefColumns: list(f.refCols), OnUpdate: f.onUpdate, OnDelete: f.onDelete })

// oldDef is the table as loaded: the "old" of the diff. Null when the table is being created.
export function oldDef(schema: string, d0: Draft): TableDef {
  return {
    Schema: schema, Name: d0.name,
    Columns: d0.cols.map((c) => ({ Name: c.name, OrigName: c.name, Type: c.type, Nullable: c.nullable, Default: c.def, HasDef: c.def !== '', PK: c.pk, Extra: c.extra, FK: null })),
    PrimaryKey: d0.cols.filter((c) => c.pk).map((c) => c.name),
    Indexes: d0.idx.map((i) => ({ Name: i.name, Columns: list(i.cols), Unique: i.unique, Primary: false, Algorithm: i.algorithm, Condition: i.condition })),
    ForeignKeys: d0.fks.map(fkOf),
  }
}

// newDef validates the draft and builds the "new" side. Throws Error with the sentence to show.
export function newDef(schema: string, d: Draft, d0?: Draft): TableDef {
  const cols = d.cols.filter((c) => !c.gone)
  if (!d.name.trim()) throw new Error('A table needs a name')
  if (!cols.length) throw new Error('A table needs at least one column')
  const seen = new Set<string>()
  for (const c of cols) {
    if (!c.name.trim() || !c.type.trim()) throw new Error('Every column needs a name and a data type')
    const k = c.name.trim().toLowerCase()
    if (seen.has(k)) throw new Error(`Duplicate column name "${c.name.trim()}"`)
    seen.add(k)
  }
  const names = new Set(cols.map((c) => c.name.trim()))
  const out: TableDef = { Schema: schema, Name: d.name.trim(), Columns: [], PrimaryKey: [], Indexes: [], ForeignKeys: [] }
  for (const c of cols) {
    const def = c.def.trim()
    out.Columns.push({ Name: c.name.trim(), OrigName: c.orig, Type: c.type.trim(), Nullable: c.nullable, Default: def, HasDef: def !== '', PK: c.pk, Extra: c.extra, FK: null })
    if (c.pk) out.PrimaryKey.push(c.name.trim())
  }
  for (const i of d.idx.filter((x) => !x.gone)) {
    const cs = list(i.cols)
    if (!i.name.trim() || !cs.length) throw new Error('Every index needs a name and at least one column')
    // an entry that is not a plain identifier is an expression (Postgres reports lower(email) as text), and rows the user did not touch are the server's own
    const o = d0?.idx.find((x) => x.orig !== '' && x.orig === i.orig)
    const untouched = !!o && o.name === i.name && o.cols === i.cols
    if (!untouched && cs.some((c) => /^\w+$/.test(c) && !names.has(c))) throw new Error(`Index ${i.name.trim()} names a column that is not in the table`)
    out.Indexes.push({ Name: i.name.trim(), Columns: cs, Unique: i.unique, Primary: false, Algorithm: i.algorithm.trim(), Condition: i.condition.trim() })
  }
  for (const f of d.fks.filter((x) => !x.gone)) {
    const v = fkOf(f)
    if (!v.Columns.length || !v.RefTable || !v.RefColumns.length) throw new Error('Every foreign key needs columns, a referenced table and referenced columns')
    if (v.Columns.length !== v.RefColumns.length) throw new Error(`Foreign key ${v.Name || v.RefTable}: ${v.Columns.length} column(s) cannot reference ${v.RefColumns.length}`)
    if (v.Columns.some((c) => !names.has(c))) throw new Error(`Foreign key ${v.Name || v.RefTable} names a column that is not in the table`)
    out.ForeignKeys.push(v)
  }
  return out
}

// dirty: the draft differs from the loaded one (a column renamed keeps its OrigName, so the planner renames instead of dropping and adding)
export const dirty = (a: Draft, b: Draft) => JSON.stringify(a) !== JSON.stringify(b)
