-- want: RLS009:warn:2
create policy own on public.notes for select to authenticated using (auth.uid() = owner_id);
