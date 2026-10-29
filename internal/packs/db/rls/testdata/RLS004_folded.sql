-- want: RLS004:error:2 RLS004:error:3 RLS004:error:4 RLS004:warn:5 RLS004:error:6 RLS004:error:7 RLS004:error:11
create policy a on public.notes for insert to anon with check (true or false);
create policy b on public.notes for update to authenticated using (true::boolean);
create policy c on public.notes for delete to anon using ('a' = 'a');
create policy d on public.notes for select to anon using (owner_id = (select auth.uid()) or true);
create policy e on public.notes for all to anon using (not false);
create policy f on public.notes for insert to anon with check (true and (1 = 1));
create policy g on public.notes for select to anon using (owner_id = (select auth.uid()) or false);
create policy h on public.notes for update to authenticated using ((select auth.uid()) = owner_id and true);
create policy i on public.notes for all to anon using ('t'::bool = true and id > 0);
create policy j on public.notes for delete to anon using (id between 1 and 10 or true);
create policy k on public.notes for delete to anon using (true and id between 1 and 2);
