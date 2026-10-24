-- Idempotent: the integration tests apply this file statement by statement (db.Split("cql", ...)) before they run.
CREATE KEYSPACE IF NOT EXISTS tuidb WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};
USE tuidb;
CREATE TYPE IF NOT EXISTS address (street text, city text, zip int);
CREATE TABLE IF NOT EXISTS customers (
  id int PRIMARY KEY, name text, email text, notes text, balance decimal, created_at timestamp, tags set<text>, prefs map<text, int>, home frozen<address>);
CREATE TABLE IF NOT EXISTS orders (
  customer_id int, id int, total decimal, status text, placed date,
  PRIMARY KEY ((customer_id), id)) WITH CLUSTERING ORDER BY (id DESC);
CREATE INDEX IF NOT EXISTS orders_status_idx ON orders (status);
CREATE TABLE IF NOT EXISTS events (
  tenant text, ts timestamp, kind text, tenant_name text STATIC, PRIMARY KEY ((tenant), ts));
CREATE TABLE IF NOT EXISTS hits (page text PRIMARY KEY, n counter);
CREATE TABLE IF NOT EXISTS all_types (
  id int PRIMARY KEY, a ascii, t text, ti tinyint, si smallint, i int, bi bigint, vi varint, f float, d double, dec decimal, b boolean,
  u uuid, tu timeuuid, ts timestamp, dt date, tm time, dur duration, ip inet, bl blob,
  l list<int>, s set<text>, m map<text, int>, fl frozen<list<int>>, tup tuple<int, text>, addr frozen<address>, v vector<float, 3>);
CREATE MATERIALIZED VIEW IF NOT EXISTS orders_by_status AS
  SELECT * FROM orders WHERE status IS NOT NULL AND customer_id IS NOT NULL AND id IS NOT NULL PRIMARY KEY ((status), customer_id, id);
CREATE OR REPLACE FUNCTION add_one (x int) CALLED ON NULL INPUT RETURNS int LANGUAGE java AS $$ return x == null ? null : x + 1; $$;
CREATE OR REPLACE FUNCTION sum_state (state int, x int) CALLED ON NULL INPUT RETURNS int LANGUAGE java AS 'return (state == null ? 0 : state) + (x == null ? 0 : x);';
CREATE OR REPLACE AGGREGATE total_sum (int) SFUNC sum_state STYPE int INITCOND 0;
INSERT INTO customers (id, name, email, notes, balance, created_at, tags, prefs, home) VALUES
  (1, 'Ann Lee', 'ann@example.com', 'vip', 120.50, '2024-01-02 03:04:05+0000', {'a', 'b'}, {'x': 1, 'y': 2}, {street: '1 Main St', city: 'Oslo', zip: 123});
INSERT INTO customers (id, name, email, notes) VALUES (2, 'Bob O''Neil', 'bob@example.com', NULL);
INSERT INTO customers (id, name, notes, balance) VALUES (3, 'Cy 100%_done', 'has % and _ chars; and a semicolon', 5);
INSERT INTO orders (customer_id, id, total, status, placed) VALUES (1, 1, 10.00, 'paid', '2024-02-01');
INSERT INTO orders (customer_id, id, total, status, placed) VALUES (1, 2, 25.50, 'new', '2024-02-02');
INSERT INTO orders (customer_id, id, total, status, placed) VALUES (2, 3, 99.99, 'paid', '2024-02-03');
INSERT INTO events (tenant, ts, kind, tenant_name) VALUES ('acme', '2024-03-01 00:00:00+0000', 'login', 'Acme Corp');
INSERT INTO events (tenant, ts, kind) VALUES ('acme', '2024-03-02 00:00:00+0000', 'logout');
