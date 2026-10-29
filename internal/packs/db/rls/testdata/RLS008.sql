-- want: RLS008:error:2 RLS008:error:2 RLS008:error:3 RLS008:error:4 | opts: supabase=1
create function public.f() returns void language plpgsql security definer as $$ begin perform 1; end $$;
create or replace function public.g(a int) returns int language sql security definer set search_path = '' as $$ select a $$;
create function public.h() returns void language sql security definer set search_path = '' as $$ select 1 $$;
revoke execute on function public.h() from public;
