-- want:
SET lock_timeout = '5s';
ALTER TABLE orders ADD CONSTRAINT orders_code_key UNIQUE USING INDEX orders_code_idx;
