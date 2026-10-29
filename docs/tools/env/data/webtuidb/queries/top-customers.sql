-- name: Top customers
-- keywords: revenue, customers
SELECT c.full_name, c.country, count(o.id) AS orders, sum(o.total) AS revenue
FROM customers c
JOIN orders o ON o.customer_id = c.id
WHERE o.status IN ('paid', 'shipped', 'delivered')
GROUP BY c.id
ORDER BY revenue DESC
LIMIT 20;
