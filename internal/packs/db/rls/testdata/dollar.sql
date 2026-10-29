-- want:
create table public.notes (id bigint primary key);
alter table public.notes enable row level security;
create policy own on public.notes for select to authenticated using (id > 0);
do $$ begin execute 'alter table public.notes disable row level security'; end $$;
create function public.f() returns void language plpgsql as $body$ begin grant all on public.notes to anon; end $body$;
