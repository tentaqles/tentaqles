-- want: PG005:error:3
SET lock_timeout = '5s';
ALTER TABLE orders ALTER COLUMN region SET NOT NULL;
