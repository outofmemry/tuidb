// M15.7: the CREATE templates the sidebar "+" menu opens in a query tab. shortcut: one template per dialect family; the user finishes the body.
export type CreateKind = 'view' | 'matview' | 'function'

const q = (dialect: string, id: string) => (dialect === 'mysql' || dialect === 'mariadb' || dialect === 'clickhouse' ? '`' + id + '`' : '"' + id + '"')

export function createTemplate(kind: CreateKind, dialect: string, schema = ''): string {
  const name = (schema ? q(dialect, schema) + '.' : '') + q(dialect, 'name')
  if (kind === 'view') return `CREATE VIEW ${name} AS\nSELECT *\nFROM table_name;\n`
  if (kind === 'matview') return dialect === 'clickhouse'
    ? `CREATE MATERIALIZED VIEW ${name}\nENGINE = MergeTree ORDER BY tuple()\nAS SELECT *\nFROM table_name;\n`
    : `CREATE MATERIALIZED VIEW ${name} AS\nSELECT *\nFROM table_name;\n`
  if (dialect === 'postgres') return `CREATE OR REPLACE FUNCTION ${name}()\nRETURNS void\nLANGUAGE plpgsql\nAS $$\nBEGIN\n  -- body\nEND;\n$$;\n`
  if (dialect === 'mysql' || dialect === 'mariadb') return `CREATE FUNCTION ${name}()\nRETURNS INT\nDETERMINISTIC\nRETURN 0;\n` // one statement: Run splits on ';' unless the script uses DELIMITER
  return `CREATE FUNCTION ${name}()\nRETURNS INT\nAS\nBEGIN\n  RETURN 0;\nEND;\n`
}
