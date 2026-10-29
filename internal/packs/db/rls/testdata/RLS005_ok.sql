-- want:
create policy admins on public.notes for select to authenticated using ((auth.jwt() -> 'app_metadata' ->> 'role') = 'admin');
