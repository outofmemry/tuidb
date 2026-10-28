// Context menus built from meta.menus (internal/menus): the same labels in the same order as the terminal's pickers.
import type { Column } from './api'

export type MenuItem = { label: string; action: string; when?: string; ui?: string; why?: string }
export type MenuEnv = { editable?: boolean }

// holds evaluates the ';'-separated predicates the web can know about; kinds and caps do not occur in grid menus
export function holds(it: MenuItem, env: MenuEnv): boolean {
  return (it.when ?? '').split(';').map((p) => p.trim()).filter(Boolean).every((p) => (p === 'editable' ? !!env.editable : true))
}
// menuItems: the entries to show, in list order; the ones only the terminal can do stay, greyed out with the reason
export function menuItems(list: MenuItem[] | undefined, env: MenuEnv): (MenuItem & { disabled?: boolean })[] {
  return (list ?? []).filter((it) => holds(it, env)).map((it) => (it.ui === 'tui' ? { ...it, disabled: true } : it))
}

// the lists always come from meta.menus; an unknown name or missing meta is an empty list
export const listOf = (menus: Record<string, MenuItem[]> | undefined, name: 'grid' | 'result' | 'header'): MenuItem[] => menus?.[name] ?? []

// the header tooltip (M15.6): type, nullability, default and key, from the table definition
export function columnTip(c: Pick<Column, 'Name' | 'Type' | 'Nullable' | 'Default' | 'HasDef' | 'PK'> & { FK?: Column['FK'] }): string {
  const key = c.PK ? 'primary key' : c.FK ? `foreign key to ${c.FK.RefTable}(${c.FK.RefColumns.join(', ')})` : 'no key'
  return `${c.Name}\n${c.Type} · ${c.Nullable ? 'nullable' : 'not null'} · ${c.HasDef ? 'default ' + c.Default : 'no default'} · ${key}`
}
// "Copy column definition": the column as it would sit in a CREATE TABLE
export function columnDef(c: Pick<Column, 'Name' | 'Type' | 'Nullable' | 'Default' | 'HasDef' | 'PK'>): string {
  return [c.Name, c.Type, c.Nullable ? '' : 'NOT NULL', c.HasDef ? 'DEFAULT ' + c.Default : '', c.PK ? 'PRIMARY KEY' : ''].filter(Boolean).join(' ')
}
