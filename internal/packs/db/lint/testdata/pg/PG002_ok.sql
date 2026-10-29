-- want:
SET lock_timeout = '5s';
ALTER TABLE orders ADD COLUMN region text NOT NULL DEFAULT 'eu', ADD COLUMN note text;
