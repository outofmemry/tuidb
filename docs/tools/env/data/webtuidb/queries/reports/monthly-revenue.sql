-- name: Monthly revenue
-- keywords: report, chart
-- barchart x: month, y: revenue
SELECT date_trunc('month', placed_at)::date AS month, sum(total) AS revenue, count(*) AS orders
FROM orders
WHERE status <> 'cancelled'
GROUP BY 1
ORDER BY 1;
