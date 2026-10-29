import { siApachecassandra, siClickhouse, siMariadb, siMongodb, siMysql, siPostgresql, siRedis, type SimpleIcon } from 'simple-icons'

// Driver names are the Go config.Drivers values. Engines without an icon in simple-icons (DynamoDB, the PostgreSQL and MySQL profiles) get the lettered fallback.
const ICONS: Record<string, SimpleIcon> = {
  postgres: siPostgresql, mysql: siMysql, mariadb: siMariadb, clickhouse: siClickhouse, cassandra: siApachecassandra, redis: siRedis, mongodb: siMongodb,
}
export const LABELS: Record<string, string> = {
  postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', redshift: 'Redshift', greenplum: 'Greenplum', vertica: 'Vertica', singlestore: 'SingleStore',
  clickhouse: 'ClickHouse', cassandra: 'Cassandra', dynamodb: 'DynamoDB', redis: 'Redis', mongodb: 'MongoDB',
}

// Logo renders the engine's mark in ink-2; `className` replaces that colour (e.g. text-current), `brand` uses the engine's own colour.
export function Logo({ driver, size = 20, brand, className = 'text-ink-2' }: { driver: string; size?: number; brand?: boolean; className?: string }) {
  const i = ICONS[driver]
  if (!i) return <span className={`flex-none inline-flex items-center justify-center border border-current rounded-[2px] [font:600_1em_var(--font-mono)] uppercase ${className}`} style={{ width: size, height: size, fontSize: size * 0.42 }} aria-label={LABELS[driver] ?? driver}>{driver.slice(0, 2)}</span>
  return (
    <svg className={`flex-none ${className}`} width={size} height={size} viewBox="0 0 24 24" role="img" aria-label={i.title} style={brand ? { color: '#' + i.hex } : undefined}>
      <path d={i.path} fill="currentColor" />
    </svg>
  )
}
