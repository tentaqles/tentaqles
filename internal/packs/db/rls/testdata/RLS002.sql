-- want: RLS002:error:3
create table public.notes (id bigint primary key);
alter table public.notes enable row level security;
