-- name: Find customer
-- keywords: lookup
SELECT * FROM customers WHERE email ILIKE :pattern OR full_name ILIKE :pattern;
