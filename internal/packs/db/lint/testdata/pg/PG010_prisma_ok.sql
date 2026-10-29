-- want: | opts: migrator=prisma
CREATE INDEX CONCURRENTLY orders_a_idx ON orders (a);
