-- want: SUP001:error:5 PG001:error:6
SET lock_timeout = '5s';
-- tq:allow PG001: orders has 12 rows in every environment
CREATE INDEX a_idx ON orders (a);
-- tq:allow PG001
CREATE INDEX b_idx ON orders (b);
