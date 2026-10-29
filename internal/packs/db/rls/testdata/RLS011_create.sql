-- want: RLS011:error:2 RLS011:error:3 RLS011:error:4 RLS011:error:5 RLS011:error:6 RLS011:error:7 RLS011:error:11 RLS011:error:12
do $$ begin execute 'create table public.dyn (id int)'; end $$;
do $$ begin execute 'create view public.dv as select 1'; end $$;
do $$ begin execute format('create unlogged table if not exists public.%I (id int)', 'part_1'); end $$;
create function public.mk() returns void language plpgsql as $f$ begin execute 'create or replace materialized view public.mv as select 1'; end $f$;
do $$ begin execute 'create or replace recursive view public.r (n) as select 1'; end $$;
do $$ begin execute 'alter view public.v set (security_invoker = false)'; end $$;
do $$ begin execute 'create temp table scratch (id int)'; end $$;
do $$ begin execute 'create temporary view tv as select 1'; end $$;
do $$ begin execute 'create global temporary table g (id int)'; end $$;
do $$ begin execute 'create foreign table public.f (id int) server remote'; end $$;
do $$ begin execute 'import foreign schema r from server remote into public'; end $$;
