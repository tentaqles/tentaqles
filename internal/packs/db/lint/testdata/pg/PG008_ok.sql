-- want:
SET lock_timeout = '5s';
-- tq:contract expand-0041
ALTER TABLE orders DROP COLUMN legacy;
-- tq:contract expand-0040
DROP TABLE old_orders;
