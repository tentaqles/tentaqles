-- want:
do $$ begin perform pg_sleep(0); end $$;
create function public.granted_at() returns timestamptz language sql as $$ select now() $$;
-- tq:allow RLS011: seeds the policy table only, reviewed
do $$ begin execute 'create policy seed on private.x using (true)'; end $$;
create function public.notes_policy_count() returns bigint language sql as $$ select count(*) from public.policy_audit $$;
