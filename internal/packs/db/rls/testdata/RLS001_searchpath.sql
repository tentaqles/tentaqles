-- want: RLS001:error:3 RLS001:error:8 RLS001:error:10 | opts: exposed=api
set search_path = api;
create table t (id int);
create table private.p (id int);
reset search_path;
create table u (id int);
set local search_path to "$user", api, public;
create table w (id int);
select pg_catalog.set_config('search_path', 'api, public', false);
create table x (id int);
