-- want:
SET LOCAL lock_timeout TO '5s';
ALTER TABLE orders ADD COLUMN note text;
