-- want: RLS002:error:3
create table public.jobs (id bigint primary key);
alter table public.jobs enable row level security; -- tq:rls-deny-all
