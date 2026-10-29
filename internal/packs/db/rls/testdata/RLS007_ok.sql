-- want:
create view public.note_titles with (security_invoker = true) as select id from public.notes;
create view public.v2 as select id from public.notes;
alter view public.v2 set (security_invoker = on);
create view private.v3 as select id from public.notes;
create materialized view private.mv as select 1;
