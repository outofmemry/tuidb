// Run: node --test tests/templates.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { createTemplate } from '../src/utils/templates.ts'

test('create templates by dialect', () => {
  assert.match(createTemplate('view', 'postgres', 'public'), /^CREATE VIEW "public"\."name" AS/)
  assert.match(createTemplate('view', 'mysql', 'app'), /^CREATE VIEW `app`\.`name` AS/)
  assert.match(createTemplate('matview', 'postgres'), /MATERIALIZED VIEW "name"/)
  assert.match(createTemplate('function', 'postgres', 'public'), /plpgsql/)
  assert.match(createTemplate('function', 'mysql'), /DETERMINISTIC/)
  for (const d of ['mysql', 'mariadb']) { // one statement: no BEGIN/END, a single trailing ';'
    const t = createTemplate('function', d)
    assert.doesNotMatch(t, /\b(BEGIN|END)\b/i)
    assert.equal(t.split(';').filter((x) => x.trim()).length, 1)
  }
  assert.match(createTemplate('view', 'clickhouse', 'db'), /^CREATE VIEW `db`\.`name` AS/)
})

test('P2-41: the MySQL function template is one statement', () => {
  const t = createTemplate('function', 'mysql', 'app')
  assert.match(t, /DETERMINISTIC\nRETURN 0;\n$/)
  assert.ok(!/\bBEGIN\b|\bEND\b/.test(t))
})

test('P2-42: every dialect prefixes the schema', () => {
  for (const d of ['mysql', 'mariadb', 'clickhouse']) assert.match(createTemplate('view', d, 'app'), /`app`\.`name`/)
})
