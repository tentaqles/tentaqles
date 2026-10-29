-- want: PG002:error:3
SET lock_timeout = '5s';
ALTER TABLE orders ADD COLUMN region text NOT NULL;
