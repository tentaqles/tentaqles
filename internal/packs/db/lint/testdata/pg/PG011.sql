-- want: PG011:error:3 PG011:error:4
SET lock_timeout = '5s';
UPDATE orders SET status = 'new';
DELETE FROM orders USING customers;
