-- want: RLS001:error:2
create table public.notes (id bigint primary key, owner_id uuid, body text);
create table private.audit (id bigint primary key);
