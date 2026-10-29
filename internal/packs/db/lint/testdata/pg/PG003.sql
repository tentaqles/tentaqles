-- want: PG003:error:3 PG003:error:4
SET lock_timeout = '5s';
ALTER TABLE orders ADD COLUMN created_at timestamptz DEFAULT now();
ALTER TABLE orders ADD COLUMN ref uuid NOT NULL DEFAULT gen_random_uuid();
