-- want: PG008:error:3 PG008:error:4 PG008:error:5 PG008:error:6
SET lock_timeout = '5s';
ALTER TABLE orders DROP COLUMN legacy;
ALTER TABLE orders RENAME COLUMN note TO memo;
ALTER TABLE orders RENAME TO purchases;
DROP TABLE IF EXISTS old_orders;
