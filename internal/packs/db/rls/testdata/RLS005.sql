-- want: RLS005:error:2 RLS005:error:3
create policy admins on public.notes for select to authenticated using ((auth.jwt() -> 'user_metadata' ->> 'role') = 'admin');
create policy admins2 on public.notes for select to authenticated using (exists (select 1 from auth.users u where u.id = (select auth.uid()) and u.raw_user_meta_data ->> 'role' = 'admin'));
