-- want: RLS006:error:2 RLS006:error:3 RLS006:error:4 RLS006:error:5 RLS006:error:6
grant all on table public.notes to anon;
grant insert, update on public.notes to anon, authenticated;
grant select on public.notes to public;
alter default privileges in schema public grant select on tables to anon;
grant usage on schema public to public;
