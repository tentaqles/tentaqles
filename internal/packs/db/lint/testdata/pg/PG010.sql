-- want: PG010:error:3
BEGIN;
CREATE INDEX CONCURRENTLY orders_note_idx ON orders (note);
COMMIT;
