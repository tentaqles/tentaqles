-- want:
SET lock_timeout = '5s';
ALTER TABLE orders ADD COLUMN created_at timestamptz;
ALTER TABLE orders ALTER COLUMN created_at SET DEFAULT now();
