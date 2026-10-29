-- want: RLS001:error:2
create table private.notes (id bigint primary key);
alter table private.notes set schema public;
create table public.scratch (id bigint);
alter table public.scratch set schema private;
