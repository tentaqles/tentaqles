-- Smoke test for migrations/ against a disposable pgvector container (see README).
-- Fails loudly (ERROR) on any broken expectation.
\set ON_ERROR_STOP on

-- Tiny 1536-d vectors: mostly zeros with a marker dimension.
create or replace function pg_temp.v(i int) returns extensions.halfvec language sql as $$
  select (('[' || array_to_string(array(select case when g = i then 1 else 0 end from generate_series(1, 1536) g), ',') || ']')::extensions.halfvec)
$$;

insert into public.documents (namespace, source_id, title, metadata) values
  ('default', 'refund-policy', 'Refund policy', '{"document_type":"policy"}'),
  ('default', 'pricing-plans', 'Pricing', '{"document_type":"reference"}'),
  ('other',   'secret-doc',    'Other tenant', '{}');

insert into public.chunks (document_id, namespace, chunk_index, content, content_hash, metadata, embedding)
select d.id, d.namespace, 0, x.content, md5(x.content), d.metadata, pg_temp.v(x.dim)
from public.documents d
join (values
  ('refund-policy', 'Customers can request a refund within 30 days of purchase.', 1),
  ('pricing-plans', 'The Team plan costs 49 dollars per month.', 2),
  ('secret-doc',    'Refund refund refund for the other tenant.', 1)
) as x(source_id, content, dim) on x.source_id = d.source_id;

do $$
declare r record; n int;
begin
  -- both branches agree -> refund-policy first, with both stage scores
  select * into r from public.hybrid_search('refund days', pg_temp.v(1), 5) limit 1;
  if r.source_id <> 'refund-policy' or r.similarity is null or r.fts_rank is null then
    raise exception 'hybrid_search top hit wrong: %', row_to_json(r);
  end if;
  if abs(r.rrf_score - (1.0/61 + 1.0/61)) > 1e-9 then
    raise exception 'rrf_score % != 2/61', r.rrf_score;
  end if;

  -- OR semantics: a question with words absent from the chunk still gets an FTS rank
  select * into r from public.hybrid_search('how many days for a refund', pg_temp.v(1), 5) limit 1;
  if r.source_id <> 'refund-policy' or r.fts_rank is null then
    raise exception 'partial-term FTS match missing: %', row_to_json(r);
  end if;

  -- namespace isolation
  select count(*) into n from public.hybrid_search('refund', pg_temp.v(1), 10) where source_id = 'secret-doc';
  if n <> 0 then raise exception 'namespace leak'; end if;

  -- metadata filter that matches nothing -> zero rows (the app then retries unfiltered)
  select count(*) into n from public.hybrid_search('refund', pg_temp.v(1), 10, '{"document_type":"article"}');
  if n <> 0 then raise exception 'filter should exclude everything, got %', n; end if;

  -- vector-only match still returns (full outer join)
  select count(*) into n from public.hybrid_search('zzzz', pg_temp.v(2), 10) where source_id = 'pricing-plans';
  if n <> 1 then raise exception 'vector-only hit missing'; end if;
end
$$;

-- agent role: can read and search, cannot write or see the record manager
set role rag_agent_readonly;
select count(*) as agent_visible_chunks from public.chunks;
select source_id from public.hybrid_search('refund', pg_temp.v(1), 3) limit 1;
do $$
begin
  begin
    insert into public.record_manager (source_id, content_hash) values ('x', 'y');
    raise exception 'agent could write record_manager';
  exception when insufficient_privilege then null;
  end;
  begin
    perform count(*) from public.record_manager;
    raise exception 'agent could read record_manager';
  exception when insufficient_privilege then null;
  end;
  begin
    delete from public.chunks;
    raise exception 'agent could delete chunks';
  exception when insufficient_privilege then null;
  end;
end
$$;
reset role;

-- cascade: deleting a document removes its chunks
delete from public.documents where source_id = 'pricing-plans';
do $$ begin
  if exists (select 1 from public.chunks c join public.documents d on d.id = c.document_id where d.source_id = 'pricing-plans') then
    raise exception 'cascade failed';
  end if;
end $$;

-- index sanity
do $$ begin
  if not exists (select 1 from pg_indexes where indexname = 'chunks_embedding_hnsw' and indexdef ilike '%hnsw%halfvec_cosine_ops%') then
    raise exception 'hnsw index missing';
  end if;
  if not exists (select 1 from pg_indexes where indexname = 'chunks_fts_gin') then
    raise exception 'fts gin index missing';
  end if;
end $$;

select 'SMOKE OK' as result;
