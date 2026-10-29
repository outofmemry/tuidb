-- name: Orders by status
-- keywords: report
SELECT status, count(*) AS orders, round(avg(total), 2) AS avg_total
FROM orders
GROUP BY status
ORDER BY orders DESC;
