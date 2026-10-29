// Run: node --test tests/hint.test.mjs
import test from 'node:test'
import assert from 'node:assert/strict'
import { hint } from '../src/utils/hint.ts'

test('pool exhaustion is not reported as an SSL problem', () => {
  const e = 'failed to connect to `host=127.0.0.1 user=app database=app sslmode=prefer`: server error (FATAL: sorry, too many clients already (SQLSTATE 53300))'
  const h = hint(e)
  assert.match(h, /no free connections/)
  assert.doesNotMatch(h, /SSL|secure connection/)
  assert.match(hint('Error 1040: Too many connections'), /no free connections/)
  assert.match(hint('FATAL: remaining connection slots are reserved for non-replication superuser connections'), /no free connections/)
})

test('real TLS failures still get the SSL hint, others keep theirs', () => {
  assert.match(hint('tls: failed to verify certificate: x509: certificate signed by unknown authority'), /SSL settings/)
  assert.match(hint('server does not support SSL, but SSL was required'), /SSL settings/)
  assert.match(hint('dial tcp 127.0.0.1:1: connect: connection refused'), /Nothing answered/)
  assert.match(hint('password authentication failed for user "x"'), /not accepted/)
  assert.match(hint('something odd'), /Check the connection details/)
})
