-- want:
SET lock_timeout = '5s';
ALTER TABLE orders ALTER COLUMN code TYPE varchar(40);
ALTER TABLE orders ALTER COLUMN label SET DATA TYPE text;
