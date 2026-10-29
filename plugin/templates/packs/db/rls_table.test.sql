-- RLS test for {{table}} (tentaqles db pack template, MIT).
-- The rls-tests skill fills the placeholders and writes the file as
-- rls_<table>.test.sql under packs.db.rls.tests:
--   {{table}}      schema-qualified table, for example public.notes
--   {{owner_col}}  the column the policies compare with auth.uid(), for example owner_id
--   {{col}}        one more writable column, for example body
-- Add the other NOT NULL columns of the table to the inserts.
-- Needs the tests_tq helpers (000_tests_tq_helpers.sql). Synthetic users only;
-- never real data.

begin;
select plan(6);

-- two synthetic users
select tests_tq.create_user('tester-a'), tests_tq.create_user('tester-b');

-- seed one row per user as the service role
select tests_tq.as_service();
insert into {{table}} ({{owner_col}}, {{col}})
values (tests_tq.uid('tester-a'), 'a'), (tests_tq.uid('tester-b'), 'b');

select tests_tq.as_user('tester-a');
select results_eq('select count(*)::int from {{table}}', array[1], 'tester-a sees only own rows');
select throws_ok(
  $$insert into {{table}} ({{owner_col}}, {{col}}) values (tests_tq.uid('tester-b'), 'x')$$,
  '42501', null, 'tester-a cannot write as tester-b');
select is_empty(
  $$update {{table}} set {{col}} = 'y' where {{owner_col}} = tests_tq.uid('tester-b') returning 1$$,
  'tester-a cannot update tester-b rows');

select tests_tq.as_anon();
select is_empty('select 1 from {{table}}', 'anon sees nothing');

select tests_tq.as_service();
select isnt_empty('select 1 from {{table}}', 'service sees all');
select ok((select relrowsecurity from pg_class where oid = '{{table}}'::regclass), 'rls enabled');

select * from finish();
rollback;
