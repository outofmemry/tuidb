import { siApachecassandra, siClickhouse, siCloudflare, siCockroachlabs, siDuckdb, siMariadb, siMongodb, siMysql, siPostgresql, siRedis, siSqlite, type SimpleIcon } from 'simple-icons'

// Driver names are the Go config.Drivers values. DynamoDB has no icon in simple-icons, so it gets the lettered fallback.
const ICONS: Record<string, SimpleIcon> = {
  postgres: siPostgresql, mysql: siMysql, mariadb: siMariadb, cockroachdb: siCockroachlabs, sqlite: siSqlite, duckdb: siDuckdb,
  clickhouse: siClickhouse, cassandra: siApachecassandra, d1: siCloudflare, redis: siRedis, mongodb: siMongodb,
}
export const LABELS: Record<string, string> = {
  postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', cockroachdb: 'CockroachDB', sqlite: 'SQLite', duckdb: 'DuckDB',
  clickhouse: 'ClickHouse', cassandra: 'Cassandra', dynamodb: 'DynamoDB', d1: 'Cloudflare D1', redis: 'Redis', mongodb: 'MongoDB',
}

// Logo renders the engine's mark in the current text colour; `brand` uses the engine's own colour.
export function Logo({ driver, size = 20, brand }: { driver: string; size?: number; brand?: boolean }) {
  const i = ICONS[driver]
  if (!i) return <span className="logo fallback" style={{ width: size, height: size, fontSize: size * 0.42 }} aria-label={LABELS[driver] ?? driver}>{driver.slice(0, 2)}</span>
  return (
    <svg className="logo" width={size} height={size} viewBox="0 0 24 24" role="img" aria-label={i.title} style={brand ? { color: '#' + i.hex } : undefined}>
      <path d={i.path} fill="currentColor" />
    </svg>
  )
}
