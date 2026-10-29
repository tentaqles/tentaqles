-- want: PG011:error:3 PG011:error:4 PG011:error:5
SET lock_timeout = '5s';
WITH x AS (SELECT id FROM customers WHERE id > 0) UPDATE orders SET total = 0;
WITH RECURSIVE x(id) AS (SELECT 1), y AS NOT MATERIALIZED (SELECT 2) DELETE FROM orders;
WITH x AS (SELECT 1)
DELETE FROM orders;
WITH x AS (SELECT id FROM customers) UPDATE orders SET total = 0 WHERE id IN (SELECT id FROM x);
