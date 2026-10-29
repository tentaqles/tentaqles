-- want: RLS001:error:3 | opts: migrator=dbmate
-- migrate:up
create table public.notes (id bigint primary key);
-- migrate:down
alter table public.notes disable row level security;
drop table public.notes;
