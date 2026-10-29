create table public.notes (id bigint primary key, owner_id uuid);
alter table public.notes enable row level security;
