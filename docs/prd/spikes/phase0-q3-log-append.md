# Phase 0, question 3: do parallel appends tear a line?

The question, from the [core page](../vertical-lobe.md), section 8: do twenty short-lived processes appending to
one log on Windows ever tear a line, and what does a lock cost? The store ([F-01](../lobe-foundation.md)) and the
event log ([S-07](../lobe-sense.md)) were designed on the guess that they do not. This page replaces the guess
with a measurement. The test stays in the repository as `TestLobeLogAppendParallel`
(`internal/lobe/eventlog`), so the answer is checked again on every run.

## What was run

Twenty processes, each appending 500 lines to one file, 10,000 lines in all. Each append is one write call of a
whole line with its line break, on a file opened in append mode, with no flush to disk. Two ways:

- **No lock.** Each process appends and never touches the store lock.
- **As the hook does.** Each process waits up to 50 ms for the store lock, then appends whether or not it got it.

A torn line is one that does not parse as JSON. A lost line is one a process wrote and the file does not hold.

## Result on Windows

Windows 11, the development machine, 2026-11-09. Two runs gave the same counts.

| Way | Lines | Torn | Lost | Twice |
|---|---|---|---|---|
| No lock | 10,000 | 0 | 0 | 0 |
| As the hook does | 10,000 | 0 | 0 | 0 |

Time of one append, measured over the 10,000 calls made as the hook does, with twenty processes writing at once:
half of the calls took under 6 ms, 95 of 100 took under 48 ms, and the slowest took 52 ms (42 ms and 52 ms in the
second run). The 50 ms limit shows in those numbers: under that much contention many calls waited the whole
limit and then appended without the lock.

Two related measurements from the store's own tests: with the lock held by another process for two seconds, an
append returned after 51 ms with its line written; after the holder of the lock was killed, the next process got
the lock within 3 ms.

## What it means

- **No line tears without the lock.** A whole-line append is safe on its own on Windows. The lock is not needed
  to keep the log well formed.
- **The lock therefore serves the duplicate filter only** (feature 007, S-06), which must read the end of the
  file and decide before it writes. Until that feature exists, taking the lock around an event append buys
  nothing and costs a wait under heavy contention. It is kept, because 007 needs it in the same place.
- **The cost is bounded by the 50 ms limit.** Twenty writers at once is far more than a session produces. With
  one or two hooks at a time the lock is free and an append takes a few milliseconds. The worst case, 52 ms,
  fits the capture budget of 150 ms (core page 5.2), but it is a third of it; feature 012 measures the whole
  hook.
- **A reader still skips a last line with no line break** (S-07 step 8). Nothing here shows it is needed, but a
  process can be killed in the middle of a write, which this test does not do.

## Still open

- **Linux and macOS.** The same test runs in CI on both. Its lines in the CI log start with `phase0-q3`. This
  page is updated with those numbers after the first run.
- **A process killed during its write.** Not tested here. The repair of a torn last line is feature 011.
