-- want: PG009:error:2
ALTER TABLE orders ADD COLUMN note text;
SET lock_timeout = '5s';
