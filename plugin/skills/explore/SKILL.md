---
name: explore
description: Find where a question about the codebase is answered before reading files — runs `tq decide explore`, which keyword-filters the repo and has Jev rank ~40-line spans, then reads only the top few ranges. Use when the user asks "where is X handled", "how does Y work", "what calls Z", "find the code that…", when starting a bug hunt in unfamiliar code, or before opening many files to locate something. Prefer this over reading whole files or broad grep-and-read loops.
---

# Explore

Locate the code that answers `$ARGUMENTS` with as little reading as possible.

## Process

1. If `$ARGUMENTS` is empty, ask what the user wants to find. Phrase the
   question with the identifiers, file names, error text or domain words
   that are likely to appear in the code ("where does the breaker trip after
   a jev failure", not "how does it work").

2. Run explore (plain `tq`, no Python needed):

   ```bash
   tq decide explore "$ARGUMENTS" --top 5
   ```

   Useful flags: `--path DIR` to search a subdirectory, `--candidates 50`
   to widen the keyword stage, `--json` for machine-readable output,
   `--no-jev` to skip the network entirely.

3. Read **only** the ranges printed, top first, with the Read tool's
   `offset`/`limit` (for `internal/decide/judge.go:228-284`, read offset 228,
   limit 57). Stop as soon as the question is answered. Do not open other
   files unless the top ranges point to them (a call, an import, a type).

4. Interpret the header line:
   - `jev ranked N candidate spans` — scores are Jev's relevance
     probabilities; anything below ~0.3 is probably noise.
   - `keyword ranking … (jev unavailable: …)` — Jev is off, has no key, or
     failed; the order is by keyword hits only. Still read the top ranges
     first, but expect to check one or two more.
   - `no keyword hits` — rephrase with concrete identifiers or an error
     string, or fall back to Grep for an exact symbol.

5. If `tq` is not installed (`command not found`), fall back to Grep with
   the key terms, then read only the matching regions.

## Notes

- Jev only re-ranks what the keyword stage found; it never adds files.
- The keyword stage honours every git ignore source (and fails closed on
  patterns it cannot parse: a `note: skipped N subtree(s)` line), never
  reads `.env*`, key material, credential files or dumps, and never follows
  symlinks; span text sent to Jev is redacted by tq first.
- If the code you need sits in an ignored or skipped area, read it directly
  instead; explore will not surface it by design.
- Explore is enabled per workspace by the manifest's `decision:` block
  (`backend: typesafe`); without it you get the keyword ranking.
