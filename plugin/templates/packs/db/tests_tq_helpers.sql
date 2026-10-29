-- tests_tq: pgTAP helpers for RLS tests (tentaqles db pack, MIT).
-- Test-only: copy this file into the tests directory (packs.db.rls.tests) as
-- 000_tests_tq_helpers.sql so it runs before the table tests. Never put it under
-- the app's migrations. It commits, so the helpers stay for the later test files;
-- every table test runs in its own transaction and rolls it back.
-- On Supabase pgTAP lives in the extensions schema; the create below is then a no-op.
--
-- Supabase: auth.users, auth.uid() and the roles anon, authenticated and
-- service_role already exist. Plain Postgres: this file creates a stub auth.users,
-- an auth.uid() that reads request.jwt.claims, and the three roles. Run it as
-- the database owner of a local or ephemeral test database only.
--
-- Users are synthetic (tester-a, tester-b). Never real data.

begin;
create extension if not exists pgtap;
select plan(1);

do $roles$
begin
  if not exists (select 1 from pg_roles where rolname = 'anon') then
    create role anon nologin noinherit;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'authenticated') then
    create role authenticated nologin noinherit;
  end if;
  if not exists (select 1 from pg_roles where rolname = 'service_role') then
    create role service_role nologin noinherit bypassrls;
  end if;
end
$roles$;

do $auth$
begin
  if to_regclass('auth.users') is null then
    create schema if not exists auth;
    create table auth.users (
      id uuid primary key,
      email text unique,
      raw_app_meta_data jsonb not null default '{}'::jsonb,
      raw_user_meta_data jsonb not null default '{}'::jsonb,
      created_at timestamptz not null default now()
    );
    grant usage on schema auth to anon, authenticated, service_role;
  end if;
  if to_regprocedure('auth.uid()') is null then
    execute $fn$
      create function auth.uid() returns uuid
      language sql stable
      set search_path = ''
      as $body$
        select coalesce(
          nullif(current_setting('request.jwt.claim.sub', true), ''),
          nullif(current_setting('request.jwt.claims', true), '')::jsonb ->> 'sub'
        )::uuid
      $body$
    $fn$;
    grant execute on function auth.uid() to anon, authenticated, service_role;
  end if;
end
$auth$;

create schema if not exists tests_tq;
grant usage on schema tests_tq to anon, authenticated, service_role;

-- A stable id per synthetic name, so a test can refer to a user before or after
-- creating it.
create or replace function tests_tq.uid(name text) returns uuid
language sql immutable
set search_path = ''
as $$ select md5('tests_tq:' || name)::uuid $$;

-- Insert a synthetic user into auth.users (idempotent).
create or replace function tests_tq.create_user(name text) returns uuid
language plpgsql
set search_path = ''
as $$
begin
  insert into auth.users (id, email, raw_app_meta_data, raw_user_meta_data)
  values (tests_tq.uid(name), name || '@example.test', '{}'::jsonb, '{}'::jsonb)
  on conflict (id) do nothing;
  return tests_tq.uid(name);
end
$$;

-- Act as a signed-in user for the rest of the transaction.
create or replace function tests_tq.as_user(name text) returns void
language plpgsql
set search_path = ''
as $$
begin
  perform set_config('request.jwt.claims',
    json_build_object('sub', tests_tq.uid(name), 'role', 'authenticated')::text, true);
  perform set_config('request.jwt.claim.sub', tests_tq.uid(name)::text, true);
  set local role authenticated;
end
$$;

-- Act as the anonymous (signed-out) role.
create or replace function tests_tq.as_anon() returns void
language plpgsql
set search_path = ''
as $$
begin
  perform set_config('request.jwt.claims', json_build_object('role', 'anon')::text, true);
  perform set_config('request.jwt.claim.sub', '', true);
  set local role anon;
end
$$;

-- Act as the service role, which bypasses RLS (seeding and "sees all" checks).
create or replace function tests_tq.as_service() returns void
language plpgsql
set search_path = ''
as $$
begin
  perform set_config('request.jwt.claims', json_build_object('role', 'service_role')::text, true);
  perform set_config('request.jwt.claim.sub', '', true);
  set local role service_role;
end
$$;

grant execute on all functions in schema tests_tq to anon, authenticated, service_role;

select ok(to_regprocedure('tests_tq.as_user(text)') is not null, 'tests_tq helpers installed');
select * from finish();
commit;
