-- want: PG001:error:4 PG001:error:5
-- tq:allow PG001, PG009: header allows cover only the file rules
SELECT 1;
CREATE INDEX a_idx ON orders (a);
CREATE INDEX b_idx ON orders (b);
