-- want: PG001:error:4 PG004:error:5
SET lock_timeout = '5s';
CREATE TABLE IF NOT EXISTS orders (id bigint);
CREATE INDEX orders_total_idx ON orders (total);
ALTER TABLE orders ALTER COLUMN code TYPE varchar(5);
