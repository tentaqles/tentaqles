-- want:
drop policy if exists notes_owner on public.notes;
create policy notes_owner on public.notes for all to authenticated using ((select auth.uid()) = owner_id);
alter table public.notes disable row level security;
update public.notes set owner_id = owner_id where id < 0;
alter table public.notes enable row level security;
