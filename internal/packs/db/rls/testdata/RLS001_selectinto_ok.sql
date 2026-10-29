-- want:
select * into public.users_backup from auth.users;
alter table public.users_backup enable row level security;
create policy own on public.users_backup for select to authenticated using ((select auth.uid()) = id);
select 1 into temp scratch;
select * into private.archive from private.secrets;
