-- want: RLS011:error:2 RLS011:error:3 RLS011:error:4 RLS011:error:5
do $$ begin execute format('create policy p on %I for all using (true)', 'notes'); end $$;
create or replace function public.setup() returns void language plpgsql as $fn$ begin execute 'alter table private.t set schema public'; end $fn$;
do 'begin execute ''grant select on public.notes to anon''; end';
create procedure public.p() language plpgsql as $$ begin execute 'create function public.x() returns int security definer language sql as ''select 1'''; end $$;
