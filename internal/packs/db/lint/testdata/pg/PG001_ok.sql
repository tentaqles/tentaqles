-- want:
CREATE INDEX CONCURRENTLY IF NOT EXISTS orders_status_idx ON public.orders (status);
CREATE TABLE acme_notes (id bigint PRIMARY KEY, owner_id uuid NOT NULL);
CREATE INDEX acme_notes_owner_idx ON acme_notes (owner_id);
