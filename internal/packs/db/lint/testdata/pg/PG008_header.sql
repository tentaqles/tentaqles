-- tq:contract expand-0039: the expand shipped in release 12
-- want:
SET lock_timeout = '5s';
ALTER TABLE orders DROP COLUMN legacy, DROP COLUMN legacy2;
