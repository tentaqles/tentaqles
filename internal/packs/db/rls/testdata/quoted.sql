-- want: RLS001:error:2
create table "public"."Notes" (id bigint primary key);
create table "public"."Other" (id bigint primary key);
alter table public."Other" enable row level security; -- tq:rls-deny-all service only
