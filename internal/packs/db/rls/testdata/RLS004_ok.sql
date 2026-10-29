-- want:
create policy own on public.notes for all to authenticated using ((select auth.uid()) = owner_id) with check ((select auth.uid()) = owner_id);
create policy only_live on public.notes as restrictive for all to authenticated using (true);
