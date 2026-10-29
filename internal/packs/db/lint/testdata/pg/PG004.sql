-- want: PG004:error:3 PG004:error:4
SET lock_timeout = '5s';
ALTER TABLE orders ALTER COLUMN qty TYPE bigint;
ALTER TABLE orders ALTER COLUMN code TYPE varchar(5);
