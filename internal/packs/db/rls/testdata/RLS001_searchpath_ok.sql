-- want: | opts: exposed=api
set search_path to api, public;
create table t (id int, owner uuid);
alter table t enable row level security;
create policy p on t for select to authenticated using ((select auth.uid()) = owner);
set search_path to default;
create table u (id int);
set search_path = private;
create table v (id int);
select set_config('search_path', '', false);
create table w (id int);
