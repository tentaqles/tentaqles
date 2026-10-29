create table public.notes (id bigint primary key, owner_id uuid);
alter table public.notes enable row level security, force row level security;
create policy notes_owner on public.notes using ((select auth.uid()) = owner_id);
