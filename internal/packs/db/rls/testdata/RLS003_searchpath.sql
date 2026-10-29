-- want: RLS003:error:3 RLS004:error:4 RLS003:error:7
set search_path = private, public;
alter table notes disable row level security;
create policy p on notes for insert to anon with check (true);
create schema api;
set search_path = api, public;
alter table items disable row level security;
