-- want: RLS007:error:2 RLS007:error:3
create materialized view public.mv as select 1;
create materialized view if not exists public.mv2 as select 1;
create materialized view private.mv as select 1;
