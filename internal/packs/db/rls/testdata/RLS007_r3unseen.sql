-- want: RLS007:error:2 RLS007:error:4 RLS001:error:5
alter materialized view private.u2 set schema public;
set search_path = private;
alter materialized view u3 set schema public;
alter table private.u4 set schema public;
