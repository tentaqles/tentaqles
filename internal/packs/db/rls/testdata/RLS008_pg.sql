-- want:
create function public.g(a int) returns int language sql security definer set search_path = '' as $$ select a $$;
revoke execute on function public.g(int) from public;
