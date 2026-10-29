-- want: RLS001:error:2 RLS001:error:3 RLS001:error:4 RLS001:error:5
select * into public.users_backup from auth.users;
select id, email into table public.leak2 from auth.users where true;
with u as (select id from auth.users) select id into unlogged public.leak3 from u;
select * into leak4 from private.secrets;
select 1 into temp scratch;
select * into temporary table scratch2 from private.secrets;
select * into private.archive from private.secrets;
select count(*) from (select 1) s;
with x as (insert into public.notes (id) values (1) returning id) select id from x;
