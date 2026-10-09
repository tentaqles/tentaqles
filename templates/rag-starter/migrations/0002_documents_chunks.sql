-- 0002: documents, chunks, record manager
--
-- EMBEDDING DIMENSION: choose it once per project. Never mix models in a table.
--   pgvector HNSW index limits: vector -> 2,000 dims, halfvec -> 4,000 dims.
--   This template uses halfvec(1536). It costs half the storage of vector and
--   recall is practically the same. Models over 4,000 dims must be truncated
--   (OpenAI `dimensions=`, Matryoshka models) or they cannot use HNSW at all.
--   To change N, edit it here AND in 0003_hybrid_search.sql AND in your
--   Embedder(dim=N). rag_starter.embeddings.check_dim enforces the 4,000 cap.
--
-- FULL-TEXT LANGUAGE: 'english'. For Portuguese content, use 'portuguese' here
-- AND in 0003 (the query side must use the same config).

create table if not exists public.documents (
  id          uuid primary key default gen_random_uuid(),
  namespace   text not null default 'default',
  source_id   text not null,
  title       text not null default '',
  metadata    jsonb not null default '{}'::jsonb,
  created_at  timestamptz not null default now(),
  updated_at  timestamptz not null default now(),
  unique (namespace, source_id)
);

create table if not exists public.chunks (
  id            bigint generated always as identity primary key,
  document_id   uuid not null references public.documents (id) on delete cascade,
  namespace     text not null default 'default',
  chunk_index   int not null,
  content       text not null,
  content_hash  text not null,
  metadata      jsonb not null default '{}'::jsonb,
  embedding     extensions.halfvec(1536) not null,
  fts           tsvector generated always as (to_tsvector('english'::regconfig, content)) stored,
  created_at    timestamptz not null default now(),
  unique (document_id, chunk_index)
);

-- Vector kNN (cosine). m / ef_construction are pgvector defaults. Raise
-- `hnsw.ef_search` (default 40) per session if recall@k is low.
create index if not exists chunks_embedding_hnsw
  on public.chunks using hnsw (embedding extensions.halfvec_cosine_ops)
  with (m = 16, ef_construction = 64);

-- Keyword search
create index if not exists chunks_fts_gin on public.chunks using gin (fts);
-- Metadata filters (`metadata @> '{"document_type":"reference"}'`)
create index if not exists chunks_metadata_gin on public.chunks using gin (metadata jsonb_path_ops);
create index if not exists chunks_document_id on public.chunks (document_id);
create index if not exists chunks_namespace on public.chunks (namespace);
create index if not exists chunks_content_hash on public.chunks (content_hash);

-- Record manager: one row per source document. The content hash decides
-- skip (unchanged) / re-ingest (changed); rows missing from a full sync are deleted.
create table if not exists public.record_manager (
  namespace     text not null default 'default',
  source_id     text not null,
  content_hash  text not null,
  updated_at    timestamptz not null default now(),
  primary key (namespace, source_id)
);
