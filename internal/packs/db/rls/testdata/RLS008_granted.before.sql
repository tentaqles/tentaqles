create function public.f() returns void language sql security definer set search_path = '' as $$ select 1 $$;
revoke execute on function public.f() from public, anon;
create function public.g() returns void language sql security definer set search_path = '' as $$ select 1 $$;
revoke execute on function public.g() from public, anon;
