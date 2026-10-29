---
title: Testing
description: Unit, parity, driver, end-to-end and web tests; the Docker test databases with every port and credential; what CI runs.
weight: 20
---

There are four layers of tests: Go unit tests that need nothing, parity tests that keep the two front ends identical (their own page: [Parity]({{< relref "parity" >}})), integration tests that need the Docker test databases, and TypeScript tests for the pure modules of the web app. This page lists the command for each, the databases behind `make up`, and what the CI workflows run.

## The commands

| Command | What it runs | Needs |
|---|---|---|
| `make test` | `go vet ./... && go test ./...` | Go. The integration tests are compiled but skip themselves. |
| `make parity` | The parity subset of the Go tests. See [Parity]({{< relref "parity" >}}). | Go |
| `make web-test` | `cd web && npx tsc --noEmit -p . && node --test tests/*.test.mjs` | `npm ci` done once in `web/` |
| `make up` | Starts the test databases (below) and seeds Redis. | Docker |
| `make smoke` | `WEBTUIDB_INTEGRATION=1 go test -count=1 ./...`: every test, including the ones against the databases. | `make up` first |
| `make down` | `docker compose down -v`: stops the databases and deletes their volumes. | Docker |
| `go test -fuzz=FuzzSplit ./internal/db` | The fuzz tests (below). | Go |

`make test` takes about a minute on a laptop; `internal/ui` is the slowest package. Run one package with `go test ./internal/db/...`, one test with `-run TestName`.

## The test databases: `make up` and `make down`

`docker-compose.yml` defines the Compose project `webtuidb-test`. Every port is bound to `127.0.0.1` and uses a non-default host port, so the databases never clash with a real server and are not reachable from the network. The user, password and database are `webtuidb` everywhere unless the table says otherwise.

| Service | Image | Host port | Credentials | Seed |
|---|---|---|---|---|
| `postgres` | `postgres:16` | 55432 | user `webtuidb`, password `webtuidb`, database `webtuidb` | `testdata/seed/postgres.sql` (an `initdb.d` script) |
| `mysql` | `mysql:8.4` | 53306 | `webtuidb` / `webtuidb`, database `webtuidb`; root password `root` | `testdata/seed/mysql.sql` |
| `clickhouse` | `clickhouse/clickhouse-server:24.8` | 59000 (native), 58123 (HTTP) | `webtuidb` / `webtuidb`, database `webtuidb`, access management on | `testdata/seed/clickhouse.sql` |
| `redis` | `redis:7` | 56379 | password `webtuidb` (no user name) | `testdata/seed/redis.txt`, loaded by the one-shot `redis-seed` service |
| `mongo` | `mongo:7` | 57017 | `webtuidb` / `webtuidb`, `authSource=admin` | `testdata/seed/mongo.js` |
| `mariadb` | `mariadb:11.4` | 53307 | `webtuidb` / `webtuidb`, database `webtuidb`; root password `root` | `testdata/seed/mariadb.sql` |
| `cassandra` | `cassandra:5.0` | 59042 | `webtuidb` / `webtuidb` (superuser role) | `testdata/seed/cassandra.cql`, applied by the Cassandra tests |
| `dynamodb` | `amazon/dynamodb-local:latest` | 58000 | any key pair (DynamoDB Local does not check); the tests use `webtuidb` / `webtuidb`, region `us-east-1` | `testdata/seed/dynamodb.json`, created by the DynamoDB tests |

Details worth knowing:

- `make up` is `docker compose up -d --wait postgres mysql clickhouse redis mongo mariadb cassandra dynamodb && docker compose run --rm redis-seed`. `--wait` blocks until every health check passes, so Cassandra (the slowest) can take a minute or two on a cold start.
- The `initdb.d` seeds for PostgreSQL, MySQL, ClickHouse, MariaDB and MongoDB run only when the data directory is empty, which is why `make down` uses `-v`: bring them down and up again to get a pristine seed.
- `redis-seed` pipes `redis.txt` through `redis-cli`; its first line is `FLUSHALL`, so re-running it resets the data set.
- Cassandra is started with a patched `cassandra.yaml` (password authentication, user-defined functions and materialized views switched on). Its health check doubles as set-up: it logs in as the default superuser and creates the `webtuidb` role.
- DynamoDB Local runs `-inMemory -sharedDb`: restarting the container empties it.
- The Redis seed has keys of every type (strings, a hash, a list, a set, a sorted set, a stream, a JSON string); the SQL seeds define the tables the driver tests assert on.

The databases for the documentation screenshots (the demo "shop" data set) sit on the same containers and are loaded by `docs/tools/seed.sh`; see [Documentation]({{< relref "documentation" >}}).

## `make smoke`: the integration tests

Setting `WEBTUIDB_INTEGRATION=1` un-skips every test that talks to a real server. Each test connects to the URL in an environment variable and falls back to the Compose defaults:

| Variable | Default |
|---|---|
| `WEBTUIDB_TEST_URL_POSTGRES` | `postgresql://webtuidb:webtuidb@127.0.0.1:55432/webtuidb` |
| `WEBTUIDB_TEST_URL_MYSQL` | `mysql://webtuidb:webtuidb@127.0.0.1:53306/webtuidb` |
| `WEBTUIDB_TEST_URL_MARIADB` | `mariadb://webtuidb:webtuidb@127.0.0.1:53307/webtuidb` |
| `WEBTUIDB_TEST_URL_CLICKHOUSE` | `clickhouse://webtuidb:webtuidb@127.0.0.1:59000/webtuidb` |
| `WEBTUIDB_TEST_URL_REDIS` | `redis://:webtuidb@127.0.0.1:56379/0` |
| `WEBTUIDB_TEST_URL_MONGODB` | `mongodb://webtuidb:webtuidb@127.0.0.1:57017/webtuidb?authSource=admin` |
| `WEBTUIDB_TEST_URL_CASSANDRA` | `cassandra://webtuidb:webtuidb@127.0.0.1:59042/webtuidb` |
| `WEBTUIDB_TEST_URL_DYNAMODB` | `dynamodb://webtuidb:webtuidb@127.0.0.1:58000/?region=us-east-1` |
| `WEBTUIDB_TEST_POSTGRES` | `postgres://webtuidb:webtuidb@127.0.0.1:55432/webtuidb` (the web server round trip only) |

Point them at other servers to test your own instances; the tests create objects with throwaway names and drop them afterwards.

### Driver tests

One `*_integration_test.go` file per driver in `internal/db/<engine>/`. Between them they cover, per engine and as far as the engine supports it:

- opening a session, wrong passwords, bootstrap commands, unknown options, `UseDatabase`;
- the catalog: databases, schemas, objects, columns, indexes, triggers, foreign keys, DDL and the "Copy script as" texts, compared with the seed;
- reading rows (sorting, filters, paging, exact and estimated counts, big-table and stream paging) and running statements (`Exec` values, row limits, writes, cancel by context, concurrent use);
- `Plan` and `Apply` round trips: edit, insert, delete, atomicity of a batch, and that a failed DDL batch stops and reports what ran;
- `PlanTable` and `PlanAdmin`: structure edits, clone, rename, truncate, create and drop databases, schemas, sequences;
- process lists and kill, users (PostgreSQL and MySQL), `SetSchema` and transaction state on a pinned PostgreSQL connection;
- engine specifics: PostgreSQL timestamptz round trip and a connection dropped under the pool, MariaDB system-versioned tables, MongoDB raw filters staying inert and cursor types, Redis bounded loads and stream paging, Cassandra SAI filters and counters, DynamoDB PartiQL classification and paging, and that the lexer agrees with the server about where strings and comments end.

### End-to-end UI tests

`internal/ui/e2e_test.go` and its siblings drive the real terminal model (`NewApp`) with key presses, in process (no pseudo-terminal), against a real database. `e2eSQL` is run for PostgreSQL (`TestE2EPostgres`), MySQL (`TestE2EMySQL`) and MariaDB (`TestE2EMariaDB`); Cassandra and DynamoDB have their own variants because their SQL differs. Each run:

1. creates a table and fills it from the SQL editor (`prefix e`, then run all);
2. opens it from the sidebar, edits a cell, commits (`prefix s`) and checks the value on the server;
3. adds a row through the grid and commits;
4. deletes a row and commits;
5. adds a column in the structure editor and commits;
6. edits a query result in place (a plain single-table `SELECT` with its key is editable) and commits;
7. checks that the history holds both `editor` and `gui` statements.

The DynamoDB variant ends with the structure editor's refusal of what DynamoDB cannot do. The table names start with `webtuidb_e2e_` and are dropped on cleanup.

`internal/web/postgres_integration_test.go` (`TestPostgresRoundTrip`) is the web counterpart: a real server (`httptest`) and a real PostgreSQL, open a session, read rows, plan an edit, apply it, verify, and restore the original value. There is no browser-driven test of the web app: the React code is covered by the TypeScript tests below and by the Go tests of `internal/web`.

## Unit tests without a database

- `internal/ui` uses a `harness` with a fake session (`fake_test.go`): it feeds key presses to the Bubble Tea model and asserts on the rendered screen, pending changes and the statements that reached the fake. Most behaviour of the terminal UI is tested this way.
- `internal/web` uses `newEnv`: a real `Server` behind `httptest` with a fake session, so every handler is called over HTTP with a bearer token.
- `main_test.go` and `commands_test.go` cover the command-line flags, completion scripts, `-mcp-config` snippets, `-bugreport` masking and `-check-update`.
- Packages such as `internal/db` (splitter, classifier, planner, filter SQL), `internal/importer`, `internal/export`, `internal/sqlfmt`, `internal/store`, `internal/config` and `internal/keymap` test their pure logic directly.

## Fuzz tests

`internal/db/fuzz_test.go` has two fuzz targets. In a normal `go test` run they only execute their seed corpus. To fuzz:

```sh
go test -fuzz=FuzzSplit -fuzztime=60s ./internal/db
go test -fuzz=FuzzIsReadOnly -fuzztime=60s ./internal/db
```

- `FuzzSplit` checks, for the PostgreSQL, MySQL, ClickHouse, MongoDB and Redis splitters, that every statement's `Start`/`End` match its text, that statements do not overlap, and that whatever the splitter leaves between statements is never code. A splitter that disagrees with the server about where a string or comment ends could hide a write from the safe-mode classifier, which is why this is fuzzed.
- `FuzzIsReadOnly` runs the safe-mode classifier on arbitrary text for every dialect and requires that it never panics.

## Web tests: `make web-test`

`npx tsc --noEmit -p .` type-checks `web/src`, then `node --test tests/*.test.mjs` runs the files in `web/tests/` (one per pure module: `keys`, `pending`, `grid`, `menus`, `completion`, `import`, `tabledef`, and so on). The files are listed with a glob because Node 22 does not accept a bare directory argument to `--test`. They import the `.ts` modules directly, so the Node you run them with has to strip TypeScript types by itself (Node 22.18 or newer, or 23.6 or newer; CI uses Node 22, which resolves to a release past 22.18; `engines` in `web/package.json` says the same). The tests do not need a browser or a running server. Run one file with `node --test tests/pending.test.mjs`, or `npm run test:keys` for the key-notation tests.

## CI: `.github/workflows/ci.yml`

One job, `test`, on `ubuntu-latest`, for every push and every pull request. The steps, in order:

1. `actions/checkout`, `actions/setup-go` (Go version from `go.mod`), `actions/setup-node` (Node 22, npm cache keyed on `web/package-lock.json`).
2. `make web && test -s internal/web/dist/index.html`: the web app builds (including `tsc`) and the output is not empty.
3. `make web-test`.
4. `go vet ./... && test -z "$(gofmt -l .)"`: vet, and every file must be `gofmt`-clean.
5. `make parity`.
6. `go test -race ./...`: everything, with the race detector.
7. `make up`, then `make smoke`: the integration and end-to-end tests against the Compose databases.
8. `make down`, with `if: always()` so the containers go even when a step failed.

CI does not run goreleaser, `make docker` or the documentation build; those are run by hand and by the docs workflow.

## CI: `.github/workflows/docs.yml`

Runs on pushes to `main` that touch `docs/**` or the workflow file, and on demand (`workflow_dispatch`). Two jobs:

- `build`: checkout with `fetch-depth: 0` (the site shows each page's last-updated date and an edit link from Git history, `enableGitInfo`), Hugo 0.169.0 extended via `peaceiris/actions-hugo`, `actions/configure-pages`, then `hugo -s docs --minify --baseURL "<pages base url>/"`, and uploads `docs/public` as the Pages artifact.
- `deploy`: `actions/deploy-pages` into the `github-pages` environment.

The workflow has `contents: read`, `pages: write` and `id-token: write` permissions, and a `pages` concurrency group that queues rather than cancels a running deploy. The repository's Pages setting has to use "GitHub Actions" as its source. More in [Documentation]({{< relref "documentation" >}}).
