-- want: PG010:error:3 PG010:error:4 | opts: migrator=prisma
-- prisma sends migration.sql as one multi-statement query: one implicit transaction
CREATE INDEX CONCURRENTLY orders_a_idx ON orders (a);
CREATE INDEX CONCURRENTLY orders_b_idx ON orders (b);
