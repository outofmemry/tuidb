// Backup and restore option helpers.


export type BackupOption = { Flag: string; Desc: string }
// shortcut: a static copy of backup.CommonOptions(driver, restore) for when GET /backup/options is not served; upgrade: drop it once the route is stable
export function commonBackupOptions(driver: string, restore: boolean): BackupOption[] {
  if (driver === 'postgres') return restore
    ? [{ Flag: '--no-owner', Desc: 'Do not restore object ownership' }, { Flag: '--no-privileges', Desc: 'Do not restore grants' }, { Flag: '--clean', Desc: 'Drop objects before creating them' }, { Flag: '--if-exists', Desc: 'Use IF EXISTS with --clean' }, { Flag: '--single-transaction', Desc: 'Restore in one transaction' }, { Flag: '--exit-on-error', Desc: 'Stop at the first error' }]
    : [{ Flag: '--no-owner', Desc: 'Do not dump object ownership' }, { Flag: '--no-privileges', Desc: 'Do not dump grants' }, { Flag: '--clean', Desc: 'Add DROP statements before CREATE' }, { Flag: '--if-exists', Desc: 'Use IF EXISTS with --clean' }, { Flag: '--inserts', Desc: 'Dump data as INSERT instead of COPY' }, { Flag: '--column-inserts', Desc: 'INSERT with column names' }]
  if (driver === 'mysql' || driver === 'mariadb') return restore
    ? [{ Flag: '--force', Desc: 'Continue after SQL errors' }, { Flag: '--binary-mode', Desc: 'Allow binary data in the file' }]
    : [{ Flag: '--single-transaction', Desc: 'Consistent snapshot without locking' }, { Flag: '--routines', Desc: 'Include stored routines' }, { Flag: '--triggers', Desc: 'Include triggers' }, { Flag: '--events', Desc: 'Include events' }, { Flag: '--no-tablespaces', Desc: 'Skip tablespace statements' }, { Flag: '--skip-lock-tables', Desc: 'Do not lock tables' }]
  return []
}

// extraArgs joins the ticked common options with the free-form text (split on spaces, double quotes group), in that order, without repeats.
export function extraArgs(picked: string[], text: string): string[] {
  const out = [...picked]
  for (const m of text.matchAll(/"([^"]*)"|(\S+)/g)) { const a = m[1] ?? m[2]; if (a && !out.includes(a)) out.push(a) }
  return out
}
