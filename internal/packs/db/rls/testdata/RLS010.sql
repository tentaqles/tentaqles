-- want: RLS010:warn:2 RLS010:warn:3
create policy svc on public.notes for all to service_role using (true);
create policy svc2 on public.notes for select to authenticated using (auth.role() = 'service_role');
