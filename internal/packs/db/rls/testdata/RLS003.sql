-- want: RLS003:error:2 RLS003:error:3 RLS003:error:4
alter table public.notes disable row level security;
alter table public.notes no force row level security;
drop policy notes_owner on public.notes;
