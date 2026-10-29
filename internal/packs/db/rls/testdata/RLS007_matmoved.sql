-- want: RLS007:error:2 RLS007:error:3
alter materialized view private.mv set schema public;
alter table private.mv2 set schema public;
alter materialized view if exists private.keep set schema archive;
alter materialized view private.keep rename to kept;
