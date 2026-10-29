-- want: RLS007:error:2 RLS007:error:3
alter view private.v set schema public;
alter view if exists public.w set (security_invoker = false);
alter view private.x set schema archive;
alter view private.y rename to z;
alter view public.ok set (security_invoker = true);
