-- Demo dataset for the documentation screenshots: a small web shop.
-- Run with: docs/tools/seed.sh   (creates database "shop" on the compose Postgres)
DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;
CREATE SCHEMA IF NOT EXISTS analytics;

CREATE TABLE categories (
  id serial PRIMARY KEY,
  name text NOT NULL UNIQUE,
  parent_id int REFERENCES categories(id)
);
CREATE TABLE products (
  id serial PRIMARY KEY,
  sku text NOT NULL UNIQUE,
  name text NOT NULL,
  category_id int NOT NULL REFERENCES categories(id),
  price numeric(10,2) NOT NULL CHECK (price >= 0),
  stock int NOT NULL DEFAULT 0,
  attrs jsonb NOT NULL DEFAULT '{}'::jsonb,
  active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE customers (
  id serial PRIMARY KEY,
  email text NOT NULL UNIQUE,
  full_name text NOT NULL,
  country char(2) NOT NULL,
  city text,
  signed_up date NOT NULL,
  marketing_opt_in boolean NOT NULL DEFAULT false,
  notes text
);
CREATE TYPE order_status AS ENUM ('new', 'paid', 'shipped', 'delivered', 'cancelled', 'refunded');
CREATE TABLE orders (
  id serial PRIMARY KEY,
  customer_id int NOT NULL REFERENCES customers(id) ON DELETE RESTRICT,
  status order_status NOT NULL DEFAULT 'new',
  placed_at timestamptz NOT NULL DEFAULT now(),
  shipped_at timestamptz,
  total numeric(10,2) NOT NULL DEFAULT 0,
  shipping_address jsonb,
  coupon text
);
CREATE INDEX orders_customer_idx ON orders (customer_id);
CREATE INDEX orders_status_placed_idx ON orders (status, placed_at DESC);
CREATE TABLE order_items (
  order_id int NOT NULL REFERENCES orders(id) ON DELETE CASCADE,
  line_no int NOT NULL,
  product_id int NOT NULL REFERENCES products(id),
  quantity int NOT NULL CHECK (quantity > 0),
  unit_price numeric(10,2) NOT NULL,
  PRIMARY KEY (order_id, line_no)
);
CREATE TABLE payments (
  id bigserial PRIMARY KEY,
  order_id int NOT NULL REFERENCES orders(id),
  provider text NOT NULL,
  amount numeric(10,2) NOT NULL,
  paid_at timestamptz NOT NULL DEFAULT now(),
  reference uuid NOT NULL DEFAULT gen_random_uuid()
);
CREATE TABLE reviews (
  id serial PRIMARY KEY,
  product_id int NOT NULL REFERENCES products(id),
  customer_id int NOT NULL REFERENCES customers(id),
  rating smallint NOT NULL CHECK (rating BETWEEN 1 AND 5),
  body text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE audit_log (id bigserial PRIMARY KEY, what text NOT NULL, at timestamptz NOT NULL DEFAULT now());
CREATE TABLE no_pk (a int, b text);
CREATE TABLE analytics.daily_sales (day date PRIMARY KEY, orders int NOT NULL, revenue numeric(12,2) NOT NULL);
CREATE SEQUENCE invoice_seq START 1000;

INSERT INTO categories (name, parent_id) VALUES
 ('Electronics', NULL), ('Audio', 1), ('Cameras', 1), ('Home', NULL), ('Kitchen', 4), ('Garden', 4),
 ('Books', NULL), ('Fiction', 7), ('Technical', 7), ('Sports', NULL);

INSERT INTO products (sku, name, category_id, price, stock, attrs, active) VALUES
 ('AU-0001', 'Studio headphones', 2, 149.00, 42, '{"color":"black","wireless":false,"impedance_ohm":32}', true),
 ('AU-0002', 'Bluetooth speaker', 2, 59.90, 120, '{"color":"teal","wireless":true,"battery_h":12}', true),
 ('AU-0003', 'USB microphone', 2, 89.00, 15, '{"pattern":"cardioid","usb":"c"}', true),
 ('CA-0001', 'Mirrorless camera body', 3, 1299.00, 7, '{"sensor":"APS-C","megapixels":26}', true),
 ('CA-0002', '35mm f/1.8 lens', 3, 449.00, 11, '{"mount":"E","aperture":1.8}', true),
 ('CA-0003', 'Tripod, carbon', 3, 189.00, 0, '{"height_cm":160,"weight_kg":1.2}', false),
 ('KI-0001', 'Chef knife 20cm', 5, 79.00, 64, '{"steel":"VG-10","handle":"pakka"}', true),
 ('KI-0002', 'Cast iron pan 28cm', 5, 45.00, 88, '{"seasoned":true}', true),
 ('KI-0003', 'Espresso grinder', 5, 329.00, 9, '{"burr_mm":58,"stepless":true}', true),
 ('GA-0001', 'Garden hose 25m', 6, 24.50, 200, '{"material":"pvc"}', true),
 ('GA-0002', 'Pruning shears', 6, 18.00, 150, '{"blade":"bypass"}', true),
 ('BO-0001', 'The Left Hand of Darkness', 8, 12.99, 30, '{"author":"Ursula K. Le Guin","pages":304}', true),
 ('BO-0002', 'Designing Data-Intensive Applications', 9, 54.00, 22, '{"author":"Martin Kleppmann","pages":616}', true),
 ('BO-0003', 'The Art of PostgreSQL', 9, 49.00, 18, '{"author":"Dimitri Fontaine","pages":500}', true),
 ('SP-0001', 'Running shoes', 10, 119.00, 36, '{"size_eu":[40,41,42,43,44],"color":"orange"}', true),
 ('SP-0002', 'Yoga mat', 10, 29.00, 75, '{"thickness_mm":6}', true),
 ('SP-0003', 'Kettlebell 16kg', 10, 39.00, 40, '{"weight_kg":16}', true),
 ('EL-0001', 'Mechanical keyboard', 1, 139.00, 25, '{"switches":"brown","layout":"ISO"}', true),
 ('EL-0002', '27" monitor', 1, 349.00, 14, '{"resolution":"2560x1440","hz":144}', true),
 ('EL-0003', 'USB-C hub', 1, 39.00, 90, '{"ports":7}', true);

INSERT INTO customers (email, full_name, country, city, signed_up, marketing_opt_in, notes)
SELECT
  lower(first) || '.' || lower(last) || n || '@example.com',
  first || ' ' || last,
  (ARRAY['US','GB','DE','FR','NL','IN','BR','JP','CA','AU'])[1 + (n % 10)],
  (ARRAY['Austin','London','Berlin','Lyon','Utrecht','Pune','Recife','Osaka','Toronto','Perth'])[1 + (n % 10)],
  date '2023-01-01' + (n * 3 % 700),
  n % 3 = 0,
  CASE WHEN n % 17 = 0 THEN 'VIP: handle returns personally' WHEN n % 23 = 0 THEN 'Asked for paper invoices' ELSE NULL END
FROM generate_series(1, 240) AS n,
LATERAL (SELECT (ARRAY['Ann','Bob','Chen','Dana','Eli','Fatima','Gus','Hana','Ivan','Jo','Kai','Lena','Mo','Nia','Omar','Pia','Quinn','Rosa','Sam','Tess'])[1 + (n % 20)] AS first,
                (ARRAY['Lee','Novak','Okafor','Petrov','Silva','Tanaka','Weber','Khan','Moreau','Haddad','Nilsen','Costa'])[1 + (n % 12)] AS last) names;

INSERT INTO orders (customer_id, status, placed_at, shipped_at, total, shipping_address, coupon)
SELECT
  1 + (n * 7 % 240),
  (ARRAY['new','paid','shipped','delivered','delivered','delivered','cancelled','refunded'])[1 + (n % 8)]::order_status,
  timestamptz '2024-01-01 09:00+00' + (n * interval '5 hours 13 minutes'),
  CASE WHEN n % 8 IN (2,3,4,5) THEN timestamptz '2024-01-02 09:00+00' + (n * interval '5 hours 13 minutes') END,
  0,
  jsonb_build_object('line1', (n % 97 + 1) || ' Main St', 'city', 'Springfield', 'zip', lpad((10000 + n * 37 % 89999)::text, 5, '0')),
  CASE WHEN n % 11 = 0 THEN 'WELCOME10' WHEN n % 29 = 0 THEN 'FREESHIP' END
FROM generate_series(1, 1200) AS n;

INSERT INTO order_items (order_id, line_no, product_id, quantity, unit_price)
SELECT o.id, l, p.id, 1 + (o.id * l) % 3, p.price
FROM orders o
CROSS JOIN generate_series(1, 1 + (o.id % 3)) AS l
JOIN products p ON p.id = 1 + ((o.id * 13 + l * 7) % 20);

UPDATE orders o SET total = s.sum FROM (SELECT order_id, sum(quantity * unit_price) AS sum FROM order_items GROUP BY order_id) s WHERE s.order_id = o.id;

INSERT INTO payments (order_id, provider, amount, paid_at)
SELECT id, (ARRAY['stripe','paypal','card','bank'])[1 + (id % 4)], total, placed_at + interval '3 minutes'
FROM orders WHERE status IN ('paid','shipped','delivered','refunded');

INSERT INTO reviews (product_id, customer_id, rating, body, created_at)
SELECT 1 + (n % 20), 1 + (n * 11 % 240), 1 + (n * 7 % 5),
  (ARRAY['Exactly as described.','Arrived late but works well.','Would buy again.','Not worth the price.','Great build quality.', NULL])[1 + (n % 6)],
  timestamptz '2024-03-01 12:00+00' + (n * interval '9 hours')
FROM generate_series(1, 160) AS n;

INSERT INTO analytics.daily_sales
SELECT placed_at::date, count(*), sum(total) FROM orders GROUP BY 1;

INSERT INTO no_pk VALUES (1, 'one'), (2, 'two'), (2, 'two');

CREATE VIEW paid_orders AS SELECT * FROM orders WHERE status IN ('paid','shipped','delivered');
CREATE VIEW customer_summary AS
  SELECT c.id, c.full_name, c.country, count(o.id) AS orders, coalesce(sum(o.total), 0) AS revenue
  FROM customers c LEFT JOIN orders o ON o.customer_id = c.id GROUP BY c.id;
CREATE MATERIALIZED VIEW top_products AS
  SELECT p.name, sum(oi.quantity) AS units, sum(oi.quantity * oi.unit_price) AS revenue
  FROM order_items oi JOIN products p ON p.id = oi.product_id GROUP BY p.name ORDER BY revenue DESC;
CREATE FUNCTION customer_order_count(p_customer_id int) RETURNS int LANGUAGE sql AS $$
  SELECT count(*)::int FROM orders WHERE customer_id = p_customer_id $$;
CREATE FUNCTION log_order() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN INSERT INTO audit_log(what) VALUES ('order ' || NEW.id || ' -> ' || NEW.status); RETURN NEW; END $$;
CREATE TRIGGER orders_audit AFTER INSERT OR UPDATE OF status ON orders FOR EACH ROW EXECUTE FUNCTION log_order();
CREATE PROCEDURE archive_cancelled() LANGUAGE sql AS $$ DELETE FROM orders WHERE status = 'cancelled' AND placed_at < now() - interval '1 year' $$;
COMMENT ON TABLE orders IS 'One row per checkout; totals are denormalised from order_items.';
COMMENT ON COLUMN customers.marketing_opt_in IS 'GDPR consent flag';
