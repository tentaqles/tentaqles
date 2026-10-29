-- want: PG007:error:3
SET lock_timeout = '5s';
ALTER TABLE orders ADD CONSTRAINT orders_code_key UNIQUE (code);
