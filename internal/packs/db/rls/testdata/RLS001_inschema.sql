-- want: RLS001:error:2 RLS007:error:2 | opts: exposed=public,app
create schema app create table orders (id int) create view v as select 1;
create schema app2 authorization tester create table ok (id int) create view w as select 1;
create schema if not exists app create table app.safe (id int);
alter table app.safe enable row level security; -- tq:rls-deny-all service role only
