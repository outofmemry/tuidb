-- Demo dataset for the documentation screenshots (MySQL / MariaDB): database "shop".
DROP DATABASE IF EXISTS shop; CREATE DATABASE shop CHARACTER SET utf8mb4; USE shop;
CREATE TABLE categories (id INT AUTO_INCREMENT PRIMARY KEY, name VARCHAR(80) NOT NULL UNIQUE, parent_id INT NULL, FOREIGN KEY (parent_id) REFERENCES categories(id));
CREATE TABLE products (id INT AUTO_INCREMENT PRIMARY KEY, sku VARCHAR(16) NOT NULL UNIQUE, name VARCHAR(120) NOT NULL, category_id INT NOT NULL,
  price DECIMAL(10,2) NOT NULL, stock INT NOT NULL DEFAULT 0, attrs JSON NULL, active TINYINT(1) NOT NULL DEFAULT 1,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY (category_id) REFERENCES categories(id)) ENGINE=InnoDB;
CREATE TABLE customers (id INT AUTO_INCREMENT PRIMARY KEY, email VARCHAR(120) NOT NULL UNIQUE, full_name VARCHAR(120) NOT NULL, country CHAR(2) NOT NULL,
  city VARCHAR(80) NULL, signed_up DATE NOT NULL, marketing_opt_in TINYINT(1) NOT NULL DEFAULT 0, notes TEXT NULL);
CREATE TABLE orders (id INT AUTO_INCREMENT PRIMARY KEY, customer_id INT NOT NULL, status ENUM('new','paid','shipped','delivered','cancelled','refunded') NOT NULL DEFAULT 'new',
  placed_at DATETIME NOT NULL, shipped_at DATETIME NULL, total DECIMAL(10,2) NOT NULL DEFAULT 0, coupon VARCHAR(20) NULL,
  FOREIGN KEY (customer_id) REFERENCES customers(id), INDEX orders_status_idx (status, placed_at)) ENGINE=InnoDB;
CREATE TABLE order_items (order_id INT NOT NULL, line_no INT NOT NULL, product_id INT NOT NULL, quantity INT NOT NULL, unit_price DECIMAL(10,2) NOT NULL,
  PRIMARY KEY (order_id, line_no), FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE, FOREIGN KEY (product_id) REFERENCES products(id));
CREATE TABLE audit_log (id BIGINT AUTO_INCREMENT PRIMARY KEY, what VARCHAR(200) NOT NULL, at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP);
INSERT INTO categories (name, parent_id) VALUES ('Electronics', NULL), ('Audio', 1), ('Cameras', 1), ('Home', NULL), ('Kitchen', 4), ('Books', NULL), ('Sports', NULL);
INSERT INTO products (sku, name, category_id, price, stock, attrs) VALUES
 ('AU-0001','Studio headphones',2,149.00,42,'{"color":"black","wireless":false}'), ('AU-0002','Bluetooth speaker',2,59.90,120,'{"color":"teal","wireless":true}'),
 ('CA-0001','Mirrorless camera body',3,1299.00,7,'{"sensor":"APS-C"}'), ('CA-0002','35mm f/1.8 lens',3,449.00,11,'{"mount":"E"}'),
 ('KI-0001','Chef knife 20cm',5,79.00,64,'{"steel":"VG-10"}'), ('KI-0002','Cast iron pan 28cm',5,45.00,88,'{"seasoned":true}'),
 ('BO-0001','The Left Hand of Darkness',6,12.99,30,'{"pages":304}'), ('BO-0002','Designing Data-Intensive Applications',6,54.00,22,'{"pages":616}'),
 ('SP-0001','Running shoes',7,119.00,36,'{"color":"orange"}'), ('SP-0002','Yoga mat',7,29.00,75,'{"thickness_mm":6}'),
 ('EL-0001','Mechanical keyboard',1,139.00,25,'{"layout":"ISO"}'), ('EL-0002','27 inch monitor',1,349.00,14,'{"hz":144}');
INSERT INTO customers (email, full_name, country, city, signed_up, marketing_opt_in) WITH RECURSIVE n AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM n WHERE n < 120)
SELECT CONCAT('customer', n, '@example.com'), CONCAT(ELT(1 + n % 10, 'Ann','Bob','Chen','Dana','Eli','Fatima','Gus','Hana','Ivan','Jo'), ' ', ELT(1 + n % 6, 'Lee','Novak','Okafor','Petrov','Silva','Tanaka')),
  ELT(1 + n % 5, 'US','GB','DE','IN','BR'), ELT(1 + n % 5, 'Austin','London','Berlin','Pune','Recife'), DATE_ADD('2023-01-01', INTERVAL n*3 DAY), n % 3 = 0 FROM n;
INSERT INTO orders (customer_id, status, placed_at, total, coupon) WITH RECURSIVE n AS (SELECT 1 AS n UNION ALL SELECT n+1 FROM n WHERE n < 600)
SELECT 1 + (n*7 % 120), ELT(1 + n % 6, 'new','paid','shipped','delivered','cancelled','refunded'), DATE_ADD('2024-01-01 09:00:00', INTERVAL n*5 HOUR), 0, IF(n % 11 = 0, 'WELCOME10', NULL) FROM n;
INSERT INTO order_items SELECT o.id, 1, 1 + (o.id % 12), 1 + o.id % 3, p.price FROM orders o JOIN products p ON p.id = 1 + (o.id % 12);
INSERT INTO order_items SELECT o.id, 2, 1 + ((o.id*5) % 12), 1, p.price FROM orders o JOIN products p ON p.id = 1 + ((o.id*5) % 12) WHERE o.id % 2 = 0;
UPDATE orders o JOIN (SELECT order_id, SUM(quantity*unit_price) s FROM order_items GROUP BY order_id) t ON t.order_id = o.id SET o.total = t.s;
CREATE VIEW paid_orders AS SELECT * FROM orders WHERE status IN ('paid','shipped','delivered');
CREATE TRIGGER orders_audit AFTER INSERT ON orders FOR EACH ROW INSERT INTO audit_log(what) VALUES (CONCAT('order ', NEW.id));
CREATE FUNCTION customer_order_count(p INT) RETURNS INT READS SQL DATA RETURN (SELECT COUNT(*) FROM orders WHERE customer_id = p);
GRANT ALL ON shop.* TO 'webtuidb'@'%';
