-- want:
create table public.notes (id bigint primary key, owner_id uuid);
alter table public.notes enable row level security;
create policy notes_owner on public.notes for select to authenticated using ((select auth.uid()) = owner_id);
