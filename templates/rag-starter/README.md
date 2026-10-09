# RAG starter (Supabase / pgvector)

A copyable starting point for client RAG systems. It encodes the failure modes seen in real builds: agents over-filtering, tool loops, no evals, and re-embedding unchanged files. The core is stdlib-only Python. Postgres access is the optional `[pg]` extra.

```
ingest:   SourceDocument -> chunk -> hash -> record manager (skip | re-ingest | delete) -> embed -> chunks
retrieve: query -> embed -> hybrid_search (FTS + vector, RRF k=60) -> [no hits with filters? retry unfiltered]
                -> reranker (Jev | Cohere | none) -> top_n chunks with per-stage scores
route:    query -> Jev choice "cheap or strong?" -> model (fallback: strong)
agent:    run_tool_loop(max_steps, repeat guard, forced no-tools synthesis)
evals:    golden.jsonl -> recall@k, MRR, hit@k, faithfulness hook -> exit 1 below thresholds (CI)
```

## Layout

| path | what |
|---|---|
| `migrations/0001_extensions.sql` | Installs pgvector (>= 0.7 for `halfvec`) into the `extensions` schema. |
| `migrations/0002_documents_chunks.sql` | Creates `documents`, `chunks` and `record_manager`. `chunks` has a `halfvec(1536)` column with an HNSW index, a generated `tsvector` with a GIN index, and a GIN index on metadata. |
| `migrations/0003_hybrid_search.sql` | `hybrid_search()`: FTS and kNN fused with RRF. It returns `similarity`, `fts_rank` and `rrf_score` per chunk. |
| `migrations/0004_rls_roles.sql` | Turns on RLS everywhere, adds namespace membership for `authenticated`, and creates the SELECT-only `rag_agent_readonly` role. |
| `rag_starter/ingest.py`, `record_manager.py`, `chunking.py` | Incremental ingestion. |
| `rag_starter/embeddings.py` | The `Embedder` protocol, `HashEmbedder` (offline) and `OpenAICompatibleEmbedder`. |
| `rag_starter/retrieval.py`, `store.py`, `rrf.py` | The retrieval pipeline, an in-memory backend that mirrors the SQL, and RRF. |
| `rag_starter/pg.py` | `PostgresStore` and `PostgresRecordManager` (psycopg 3). |
| `rag_starter/rerank.py` | `JevReranker`, `CohereReranker` and `NoopReranker`. |
| `rag_starter/router.py` | `ModelRouter`, which uses a Jev choice question. |
| `rag_starter/loop.py` | `run_tool_loop` with loop guards. |
| `rag_starter/evals/` | Golden format, metrics, faithfulness hooks and the CLI (`python -m rag_starter.evals`). |
| `examples/corpus/`, `examples/golden.jsonl` | A five-document demo corpus and a ten-question golden set. |

## Quick start (offline)

```powershell
cd templates/rag-starter
python -m venv .venv; .venv\Scripts\python -m pip install -e ".[test]"
.venv\Scripts\python -m rag_starter.demo "how long do refunds take"
.venv\Scripts\python -m rag_starter.demo --filter document_type=article "API rate limit"   # filter fallback
.venv\Scripts\python -m rag_starter.evals --golden examples/golden.jsonl --k 3 --min-recall 0.8 --min-mrr 0.6
.venv\Scripts\python -m pytest
```

## With real keys

Keys only ever come from the environment:
- `TYPESAFE_API_KEY` for Jev
- `COHERE_API_KEY`
- `OPENAI_API_KEY` for embeddings
- `DATABASE_URL`

Never read or print the `.env`. Let `tq` inject it:

```powershell
tq dotenv run -- .venv\Scripts\python -m rag_starter.demo --reranker jev --route "compare the Team and Enterprise plans"
tq dotenv run -- .venv\Scripts\python -m rag_starter.evals --golden examples/golden.jsonl --reranker jev --k 5 --min-recall 0.8
tq dotenv run -- .venv\Scripts\python -m rag_starter.evals --backend postgres --embedder openai --golden golden.jsonl --min-recall 0.85 --min-mrr 0.7
```

## Database setup (Supabase)

1. **Pick the embedding model and dimension once.** pgvector's HNSW index supports up to **2,000 dims for `vector`** and **4,000 dims for `halfvec`**. This template uses `halfvec(1536)`: half the storage of `vector`, with practically the same recall.
   - For `text-embedding-3-large` (3,072 dims), either use `halfvec(3072)` or request `dimensions=1536`.
   - Anything over 4,000 dims must be truncated, or it cannot be indexed with HNSW.
   - Change `1536` in `0002`, in `0003` and in `Embedder(dim=...)`. `check_dim()` refuses anything over 4,000.
2. **Pick the full-text language.** It is `'english'` in both `0002` and `0003`. For Portuguese content, use `'portuguese'` in both places.
3. **Apply the migrations.** Copy them into `supabase/migrations/` with timestamps and run `supabase db push`. Supabase setups are usually **remote**, so say so in the client's CLAUDE.md, or agents will assume a local DB.
4. **Create the agent's login.** Run this once in the SQL editor, never in a migration:
   ```sql
   create role rag_agent login password '<generate-one>' in role rag_agent_readonly;
   alter role rag_agent set default_transaction_read_only = on;
   alter role rag_agent set statement_timeout = '5s';
   ```
   Give the agent's SQL tool that login's connection string, and nothing else. It can SELECT `documents` and `chunks` and call `hybrid_search`. It cannot write, and it cannot see `record_manager`.
5. **Ingest with the service role**, which bypasses RLS:
   ```python
   from rag_starter import Ingestor, OpenAICompatibleEmbedder
   from rag_starter.pg import connect, PostgresStore, PostgresRecordManager
   conn = connect()  # DATABASE_URL
   Ingestor(PostgresStore(conn, "acme"), OpenAICompatibleEmbedder(), PostgresRecordManager(conn)).sync(docs, "acme")
   ```

The RLS model is one Supabase project per client, with namespaces for sub-tenants inside it. If several tenants share one database, give each tenant its own agent role and policy. Do not scope by a session setting, because a SELECT can call `set_config()`.

## Design notes

- **Record manager:** the hash covers text, title and metadata, after whitespace normalization.
  - Unchanged documents are skipped, with no embedding spend.
  - Changed documents have their chunks deleted and are re-ingested.
  - With `delete_missing=True`, documents missing from a full sync are deleted. Use `delete_missing=False` for partial batches.
- **Hybrid search:**
  - The FTS side ORs the query terms. AND semantics (`websearch_to_tsquery`) miss natural-language questions.
  - Each branch pulls `match_count x 4` candidates before fusion.
  - RRF `k = 60` follows the original paper.
  - On pgvector >= 0.8, `set hnsw.iterative_scan = relaxed_order` helps filtered queries.
- **Metadata filters** are optional. If a filtered search returns nothing, `Retriever` retries unfiltered and sets `filter_fallback=True`, so you can log it and tune the prompt.
- **Jev reranker:** about 40 candidates, `batch_size=20` noul questions per request (about 300 ms each), and `min_score=0.3`.
  - Recall matters more than precision for RAG. Tune `min_score` on the golden set, not by eye.
  - Reranking 60 chunks (about 30k tokens) costs about $0.0013.
  - Chunks and query are redacted and framed as untrusted.
  - If any batch fails, the whole call falls back to `NoopReranker`, so score scales are never mixed.
- **Router:** a Jev `choice` between `cheap` and `strong`. It falls back to `strong` on error, on an unknown tier, or when confidence is below `min_confidence`.
- **Loop guards:**
  - `max_steps` (default 10), with a "last round" nudge.
  - A repeat guard: the same calls twice in a row end the loop.
  - A forced final call with tools off, so the agent never ends on a search with no answer.
  - `tool_log` is ready to persist as JSONB.
- **Pinned model:** `jev-1.13.0`. Re-run evals before bumping it.

## Evals (module 9)

Golden set is JSONL. See `rag_starter/evals/golden.py`.

```json
{"id": "sso-plan", "question": "Which plan includes SSO?", "relevant": ["pricing-plans", "sso-setup"], "filters": {}, "tags": ["multi-doc"]}
```

- **Metrics:**
  - `relevant` holds **source ids**, not chunk ids, so re-chunking does not invalidate the set.
  - Questions with `"relevant": []` (answer not in the corpus) are kept in the report but excluded from recall and MRR.
- **Faithfulness:** pass `--answers answers.jsonl` (`{"id", "answer"}` produced by your app) with `--faithfulness jev --min-faithfulness 0.8`. In code, use `TieredFaithfulness(JevFaithfulness(...), your_llm_judge)` to call the judge only when Jev is unsure.
- **CI:** the command exits 0 on pass, 1 below thresholds and 2 on bad input. It can also write a JSON report with `--json report.json`.

## Testing the SQL

`tests/sql/smoke.sql` checks the following:
- RRF math
- OR-semantics FTS
- namespace isolation
- empty filter behaviour
- the agent role's privileges
- cascades and indexes

Run it in a throwaway container:

```powershell
docker run -d --rm --name rag-smoke -p 127.0.0.1:55432:5432 -e POSTGRES_HOST_AUTH_METHOD=trust pgvector/pgvector:pg17
(Get-Content migrations\*.sql -Raw) + (Get-Content tests\sql\smoke.sql -Raw) | docker exec -i rag-smoke psql -U postgres -v ON_ERROR_STOP=1 -q
$env:RAG_STARTER_TEST_DATABASE_URL = "postgresql://postgres@127.0.0.1:55432/postgres"   # trust auth, no password
.venv\Scripts\python -m pip install -e ".[pg,test]"; .venv\Scripts\python -m pytest tests/test_pg_integration.py
docker stop rag-smoke
```

Supabase-only pieces (`auth.uid()` policies, `anon` and `authenticated` grants) are guarded, so they are skipped on plain Postgres. Test those on a Supabase branch.
