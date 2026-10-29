-- want: SQL001:error:3
SET lock_timeout = '5s';
SELECT 1 /* a /* b */ DROP TABLE orders; */;
