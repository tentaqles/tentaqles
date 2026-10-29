-- want: RLS008:error:2 RLS008:error:3 RLS006:error:3 | opts: supabase=1
grant execute on function public.f() to anon;
grant all on all functions in schema public to public;
grant execute on function public.g() to authenticated;
