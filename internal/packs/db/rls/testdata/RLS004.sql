-- want: RLS004:error:2 RLS004:warn:3 RLS004:error:4 RLS004:error:5
create policy anon_insert on public.notes for insert to anon with check (true);
create policy read_all on public.notes for select to anon, authenticated using (true);
create policy upd on public.notes for update to authenticated using ((select auth.uid()) = owner_id) with check ((true));
create policy everyone on public.notes using (1 = 1);
