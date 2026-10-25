CREATE TABLE webtuidb.customers (id UInt32, name String, email Nullable(String), notes Nullable(String), balance Decimal(10,2) DEFAULT 0, created_at DateTime DEFAULT now())
  ENGINE = MergeTree ORDER BY id;
CREATE TABLE webtuidb.orders (id UInt32, customer_id UInt32, total Decimal(10,2), status String DEFAULT 'new') ENGINE = MergeTree ORDER BY id;
CREATE TABLE webtuidb.events (ts DateTime, kind LowCardinality(String), n UInt64) ENGINE = MergeTree PARTITION BY toYYYYMM(ts) ORDER BY (kind, ts);
CREATE VIEW webtuidb.paid_orders AS SELECT * FROM webtuidb.orders WHERE status = 'paid';
INSERT INTO webtuidb.customers (id, name, email, notes, balance) VALUES (1, 'Ann Lee', 'ann@example.com', 'vip', 120.50), (2, 'Bob O''Neil', 'bob@example.com', NULL, 0), (3, 'Cy 100%_done', NULL, 'has % and _ chars', 5);
INSERT INTO webtuidb.orders (id, customer_id, total, status) VALUES (1, 1, 10.00, 'paid'), (2, 1, 25.50, 'new'), (3, 2, 99.99, 'paid');
INSERT INTO webtuidb.events SELECT now() - number * 3600, ['click','view','buy'][number % 3 + 1], number FROM numbers(50);
