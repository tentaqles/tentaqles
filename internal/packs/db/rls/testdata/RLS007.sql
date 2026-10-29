-- want: RLS007:error:2 RLS007:error:3 RLS007:error:5
create view public.note_titles as select id from public.notes;
create or replace view public.note_ids with (security_barrier) as select id from public.notes;
create view public.v3 with (security_invoker = true) as select id from public.notes;
alter view public.v3 reset (security_invoker);
