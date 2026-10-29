-- want:
create table public.jobs (id bigint primary key);
alter table public.jobs enable row level security; -- tq:rls-deny-all service-only queue
create table public.jobs2 (id bigint primary key);
-- tq:rls-deny-all written by the worker only
alter table public.jobs2 enable row level security;
