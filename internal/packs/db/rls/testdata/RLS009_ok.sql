-- want:
create policy own on public.notes for select to authenticated using ((select auth.uid()) = owner_id);
