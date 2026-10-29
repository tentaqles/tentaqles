-- want: PG007:error:3 PG007:error:4 PG007:error:5
SET lock_timeout = '5s';
ALTER TABLE orders ADD COLUMN code text UNIQUE;
ALTER TABLE orders ADD COLUMN ref bigint CONSTRAINT orders_ref_key UNIQUE;
ALTER TABLE orders ADD uid uuid PRIMARY KEY;
ALTER TABLE orders ADD COLUMN note text;
