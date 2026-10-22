CREATE TABLE customers (
  id serial PRIMARY KEY, name text NOT NULL, email text UNIQUE, notes text,
  balance numeric(10,2) DEFAULT 0, created_at timestamptz DEFAULT now(), meta jsonb);
CREATE TABLE orders (
  id serial PRIMARY KEY, customer_id int REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
  total numeric(10,2) NOT NULL, status text DEFAULT 'new');
CREATE INDEX orders_status_idx ON orders (status);
CREATE TABLE no_pk (a int, b text);
CREATE TABLE audit_log (id serial PRIMARY KEY, what text, at timestamptz DEFAULT now());
CREATE VIEW paid_orders AS SELECT * FROM orders WHERE status = 'paid';
CREATE MATERIALIZED VIEW order_totals AS SELECT customer_id, sum(total) AS total FROM orders GROUP BY 1;
CREATE SEQUENCE ticket_seq;
CREATE FUNCTION customer_order_count(p_customer_id int) RETURNS int LANGUAGE sql AS $$ SELECT count(*)::int FROM orders WHERE customer_id = p_customer_id $$;
CREATE PROCEDURE touch_audit(p text) LANGUAGE sql AS $$ INSERT INTO audit_log(what) VALUES (p) $$;
CREATE FUNCTION log_order() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO audit_log(what) VALUES ('order ' || NEW.id); RETURN NEW; END $$;
CREATE TRIGGER orders_audit AFTER INSERT ON orders FOR EACH ROW EXECUTE FUNCTION log_order();
CREATE SCHEMA app;
CREATE TABLE app.settings (k text PRIMARY KEY, v text);
INSERT INTO app.settings VALUES ('theme', 'dark');
INSERT INTO customers (name, email, notes, balance, meta) VALUES
  ('Ann Lee', 'ann@example.com', 'vip', 120.50, '{"tier": "gold", "tags": ["a","b"]}'),
  ('Bob O''Neil', 'bob@example.com', NULL, 0, NULL),
  ('Cy 100%_done', NULL, 'has % and _ chars', 5, '{"tier": "free"}');
INSERT INTO orders (customer_id, total, status) VALUES (1, 10.00, 'paid'), (1, 25.50, 'new'), (2, 99.99, 'paid');
INSERT INTO no_pk VALUES (1, 'x'), (1, 'x'), (2, NULL);
REFRESH MATERIALIZED VIEW order_totals;
