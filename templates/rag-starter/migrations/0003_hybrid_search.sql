-- 0003: hybrid search = full text + vector, fused with Reciprocal Rank Fusion.
--   rrf_score = full_text_weight / (rrf_k + rank_fts) + semantic_weight / (rrf_k + rank_vec)
-- Each branch fetches match_count * candidate_multiplier rows. A chunk missing
-- from one branch contributes 0 for it. Per-stage scores come back so traces
-- can show `rrf / similarity / fts_rank` (and the app adds `rerank`).
--
-- SECURITY INVOKER: RLS on chunks applies to whoever calls it. search_path is
-- empty, so every object is schema-qualified (Supabase linter friendly).
-- Filtered HNSW scans can return fewer rows than asked for. On pgvector >= 0.8,
-- `set hnsw.iterative_scan = relaxed_order;` in the session helps.

create or replace function public.hybrid_search(
  query_text            text,
  query_embedding       extensions.halfvec(1536),
  match_count           int     default 20,
  filter                jsonb   default '{}'::jsonb,
  ns                    text    default 'default',
  full_text_weight      float8  default 1.0,
  semantic_weight       float8  default 1.0,
  rrf_k                 int     default 60,
  candidate_multiplier  int     default 4
)
returns table (
  chunk_id    bigint,
  source_id   text,
  content     text,
  metadata    jsonb,
  similarity  float8,
  fts_rank    float8,
  rrf_score   float8
)
language sql
stable
security invoker
set search_path = ''
as $$
  with params as (
    -- OR the query terms: natural-language questions rarely contain every word of the
    -- answer, and an AND query (plainto/websearch_to_tsquery) would miss it.
    -- ts_rank_cd still ranks chunks that match more terms higher.
    select replace(plainto_tsquery('english'::regconfig, query_text)::text, ' & ', ' | ')::tsquery as q,
           least(match_count, 200) * greatest(candidate_multiplier, 1) as n
  ),
  full_text as (
    select ft.id, ft.fts_rank, row_number() over (order by ft.fts_rank desc, ft.id) as rank_ix
    from (
      select c.id, ts_rank_cd(c.fts, p.q)::float8 as fts_rank
      from public.chunks c, params p
      where c.namespace = ns
        and c.fts @@ p.q
        and c.metadata @> filter
      order by fts_rank desc, c.id
      limit (select n from params)
    ) ft
  ),
  semantic as (
    select s.id, s.similarity, row_number() over (order by s.distance, s.id) as rank_ix
    from (
      select c.id,
             (c.embedding operator(extensions.<=>) query_embedding)::float8 as distance,
             (1 - (c.embedding operator(extensions.<=>) query_embedding))::float8 as similarity
      from public.chunks c
      where c.namespace = ns
        and c.metadata @> filter
      order by c.embedding operator(extensions.<=>) query_embedding
      limit (select n from params)
    ) s
  )
  select
    c.id as chunk_id,
    d.source_id,
    c.content,
    c.metadata,
    sem.similarity,
    ft.fts_rank,
    (coalesce(full_text_weight / (rrf_k + ft.rank_ix), 0.0)
      + coalesce(semantic_weight / (rrf_k + sem.rank_ix), 0.0))::float8 as rrf_score
  from full_text ft
  full outer join semantic sem on sem.id = ft.id
  join public.chunks c on c.id = coalesce(ft.id, sem.id)
  join public.documents d on d.id = c.document_id
  order by rrf_score desc, c.id
  limit least(match_count, 200);
$$;
