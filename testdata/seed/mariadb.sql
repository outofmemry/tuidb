GRANT ALL ON *.* TO 'webtuidb'@'%';
USE webtuidb;
CREATE TABLE customers (
  id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(100) NOT NULL, email VARCHAR(100) UNIQUE, notes TEXT,
  balance DECIMAL(10,2) DEFAULT 0, created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, meta JSON);
CREATE TABLE orders (
  id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT, total DECIMAL(10,2) NOT NULL, status VARCHAR(20) DEFAULT 'new',
  CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE ON UPDATE CASCADE,
  CONSTRAINT orders_total_positive CHECK (total >= 0));
CREATE INDEX orders_status_idx ON orders (status);
CREATE TABLE no_pk (a INT, b VARCHAR(10));
CREATE TABLE audit_log (id INT AUTO_INCREMENT PRIMARY KEY, what VARCHAR(100), at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE VIEW paid_orders AS SELECT * FROM orders WHERE status = 'paid';
CREATE FUNCTION customer_order_count(p_customer_id INT) RETURNS INT DETERMINISTIC READS SQL DATA RETURN (SELECT COUNT(*) FROM orders WHERE customer_id = p_customer_id);
CREATE PROCEDURE touch_audit(IN p VARCHAR(100)) INSERT INTO audit_log(what) VALUES (p);
CREATE TRIGGER orders_audit AFTER INSERT ON orders FOR EACH ROW INSERT INTO audit_log(what) VALUES (CONCAT('order ', NEW.id));
-- MariaDB specifics
CREATE SEQUENCE invoice_seq START WITH 1000 INCREMENT BY 5 MINVALUE 1 MAXVALUE 1000000 CACHE 10;
CREATE TABLE devices (
  id UUID PRIMARY KEY DEFAULT UUID(), ip4 INET4, ip6 INET6, label VARCHAR(50) DEFAULT 'it''s new',
  seen_at DATETIME DEFAULT CURRENT_TIMESTAMP, secret VARCHAR(20) INVISIBLE, doc JSON,
  seq_no BIGINT DEFAULT NEXTVAL(invoice_seq));
CREATE TABLE prices (
  id INT PRIMARY KEY, item VARCHAR(50) NOT NULL, price DECIMAL(10,2) NOT NULL
) WITH SYSTEM VERSIONING;
INSERT INTO customers (name, email, notes, balance, meta) VALUES
  ('Ann Lee', 'ann@example.com', 'vip', 120.50, '{"tier": "gold", "tags": ["a","b"]}'),
  ('Bob O''Neil', 'bob@example.com', NULL, 0, NULL),
  ('Cy 100%_done', NULL, 'has % and _ chars', 5, '{"tier": "free"}');
INSERT INTO orders (customer_id, total, status) VALUES (1, 10.00, 'paid'), (1, 25.50, 'new'), (2, 99.99, 'paid');
INSERT INTO no_pk VALUES (1, 'x'), (1, 'x'), (2, NULL);
INSERT INTO devices (ip4, ip6, secret, doc) VALUES ('10.0.0.1', '::1', 's3', '{"os": "linux"}'), ('192.168.1.7', 'fe80::1', NULL, NULL);
INSERT INTO prices VALUES (1, 'pen', 1.50), (2, 'ink', 7.25);
UPDATE prices SET price = 1.75 WHERE id = 1;
