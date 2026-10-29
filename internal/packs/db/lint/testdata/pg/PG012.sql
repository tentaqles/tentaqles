-- want: PG012:error:3 PG012:error:4 PG012:error:5 PG012:error:6
SET lock_timeout = '5s';
VACUUM FULL orders;
CLUSTER orders USING orders_pkey;
LOCK TABLE orders IN ACCESS EXCLUSIVE MODE;
REINDEX TABLE orders;
