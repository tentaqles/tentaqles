-- want: RLS004:error:2 | opts: supabase=1
create policy uploads on storage.objects for insert to authenticated with check (true);
