-- 0004: row level security + a SELECT-only role for the agent's SQL tool.
--
-- Model: one Supabase project (or database) per client, namespaces for
-- sub-tenants inside it.
--   * service_role / owner: ingestion. Bypasses RLS and writes everything.
--   * authenticated users:  read chunks of namespaces they are members of.
--   * anon:                 nothing.
--   * rag_agent_readonly:   SELECT on documents + chunks, EXECUTE hybrid_search,
--                           read-only transactions, short statement timeout.
--                           No access to record_manager or namespace_members.

alter table public.documents       enable row level security;
alter table public.chunks          enable row level security;
alter table public.record_manager  enable row level security; -- no policies: service role only

create table if not exists public.namespace_members (
  namespace  text not null,
  user_id    uuid not null,
  primary key (namespace, user_id)
);
alter table public.namespace_members enable row level security;

-- Supabase-only pieces (auth.uid(), anon/authenticated roles), guarded so the
-- file also applies on plain Postgres for local tests.
do $$
begin
  if exists (select 1 from pg_roles where rolname = 'anon') then
    execute 'revoke all on public.documents, public.chunks, public.record_manager, public.namespace_members from anon';
    execute 'revoke execute on function public.hybrid_search(text, extensions.halfvec, int, jsonb, text, float8, float8, int, int) from anon, public';
  end if;

  if exists (select 1 from pg_namespace where nspname = 'auth')
     and exists (select 1 from pg_roles where rolname = 'authenticated') then
    execute 'revoke all on public.record_manager from authenticated';
    execute 'grant select on public.documents, public.chunks, public.namespace_members to authenticated';
    execute 'grant execute on function public.hybrid_search(text, extensions.halfvec, int, jsonb, text, float8, float8, int, int) to authenticated';

    execute 'drop policy if exists members_read_own on public.namespace_members';
    execute $p$create policy members_read_own on public.namespace_members
      for select to authenticated using (user_id = (select auth.uid()))$p$;

    execute 'drop policy if exists documents_member_read on public.documents';
    execute $p$create policy documents_member_read on public.documents
      for select to authenticated using (
        namespace in (select m.namespace from public.namespace_members m where m.user_id = (select auth.uid()))
      )$p$;

    execute 'drop policy if exists chunks_member_read on public.chunks';
    execute $p$create policy chunks_member_read on public.chunks
      for select to authenticated using (
        namespace in (select m.namespace from public.namespace_members m where m.user_id = (select auth.uid()))
      )$p$;
  end if;
end
$$;

-- Agent role. NOLOGIN group role: create a login role that inherits it
-- (see README), so the password never lives in a migration.
do $$
begin
  if not exists (select 1 from pg_roles where rolname = 'rag_agent_readonly') then
    create role rag_agent_readonly nologin;
  end if;
end
$$;

grant usage on schema public to rag_agent_readonly;
grant usage on schema extensions to rag_agent_readonly;
grant select on public.documents, public.chunks to rag_agent_readonly;
revoke all on public.record_manager, public.namespace_members from rag_agent_readonly;
grant execute on function public.hybrid_search(text, extensions.halfvec, int, jsonb, text, float8, float8, int, int)
  to rag_agent_readonly;

-- The agent reads the whole index of this client's database. If several
-- tenants share one database, create one agent role per namespace and scope
-- each policy with `using (namespace = '<ns>')`. Do NOT scope by a session
-- setting: a SELECT can call set_config() and change it.
drop policy if exists chunks_agent_read on public.chunks;
create policy chunks_agent_read on public.chunks for select to rag_agent_readonly using (true);
drop policy if exists documents_agent_read on public.documents;
create policy documents_agent_read on public.documents for select to rag_agent_readonly using (true);
