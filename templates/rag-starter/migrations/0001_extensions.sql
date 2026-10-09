-- 0001: extensions
-- `halfvec` needs pgvector >= 0.7.0. Supabase ships it; on self-hosted Postgres,
-- check with: select extversion from pg_extension where extname = 'vector';
create schema if not exists extensions;
create extension if not exists vector with schema extensions;
create extension if not exists pgcrypto with schema extensions; -- gen_random_uuid() on PG < 13
