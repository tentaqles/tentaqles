-- want:
grant select on public.notes to anon;
grant select, insert, update, delete on public.notes to authenticated;
grant usage on schema public to anon, authenticated;
grant select on private.audit to public;
alter default privileges in schema public grant select on tables to authenticated;
revoke all on public.notes from anon;
