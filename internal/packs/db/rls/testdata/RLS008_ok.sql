-- want: | opts: supabase=1
create function public.g(a int) returns int language sql security definer set search_path = '' as $$ select a $$;
revoke execute on function public.g(int) from public, anon;
-- tq:rls-anon-exec the status page health check
create function public.health() returns int language sql security definer set search_path = '' as $$ select 1 $$;
create function public.plain() returns int language sql as $$ select 1 $$;
create function private.internal() returns int language sql security definer as $$ select 1 $$;
