-- want:
UPDATE orders SET status = 'new' WHERE status IS NULL AND id IN (SELECT id FROM orders LIMIT 1000);
CREATE TABLE acme_tmp (id int);
DELETE FROM acme_tmp;
