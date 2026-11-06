#!/usr/bin/env node
// The router's monitor. Deterministic and token-free: the router runs it through Monitor
// every limits.watchMinutes, and hears only problems, one JSON object per line:
//   {"kind":"stale|blocked|failed|drift|app-down","ticket":"012-x","detail":"..."}
// Silence means all is well. Always exits 0. Two agents waiting on each other show up as
// two stale tickets. `stale` is a question for the router, never a reason to kill a job.
//   node .claude/watch/check.mjs [--now <epoch ms>]
import { existsSync, readFileSync, readdirSync, statSync, writeFileSync, mkdirSync } from "node:fs";
import { join, resolve } from "node:path";
import { git, loadProject, readJson, readText, rootOf, section } from "../hooks/lib.mjs";

const REPEAT_MINUTES = 60;
const nowArg = process.argv.indexOf("--now");
const NOW = nowArg >= 0 ? Number(process.argv[nowArg + 1]) : Date.now();
const MIN = 60000;

function checkouts(root) {
  const out = [root];
  const dir = join(root, ".claude", "worktrees");
  if (existsSync(dir)) for (const n of readdirSync(dir)) out.push(join(dir, n));
  return out;
}

function newestActivity(p, status) {
  let t = Date.parse(status.updated ?? "") || 0;
  const head = git(p, ["log", "-1", "--format=%ct"]);
  if (head.ok) t = Math.max(t, Number(head.out) * 1000);
  // Recent edits count as life: a builder between status updates is still working.
  const changed = git(p, ["status", "--porcelain"]);
  if (changed.ok) {
    for (const line of changed.out.split("\n").filter(Boolean).slice(0, 50)) {
      try { t = Math.max(t, statSync(join(p, line.slice(3).trim().replace(/^"|"$/g, ""))).mtimeMs); } catch { /* deleted */ }
    }
  }
  return t;
}

/** Bare path prefixes listed in a section: `internal/guard/`, `- docs/x.md: why`. */
function pathsIn(text) {
  const out = [];
  for (const line of text.split("\n")) {
    const tick = [...line.matchAll(/`([^`\s]+)`/g)].map((m) => m[1]);
    if (tick.length) out.push(...tick);
    else {
      const m = line.match(/^\s*[-*]\s+([\w./-]+[/.][\w./-]*)/);
      if (m) out.push(m[1]);
    }
  }
  return out.filter((p) => !/^(REQ|FR)-/.test(p));
}
const under = (file, prefix) => file === prefix || file.startsWith(prefix.endsWith("/") ? prefix : prefix + "/");

function drift(p, cfg, t, base) {
  const rdFile = join(p, cfg.ticketsDir, t, "rd.md");
  if (!existsSync(rdFile) || !base) return [];
  const rd = readText(rdFile);
  const areas = pathsIn(section(rd, "Affected areas"));
  const forbidden = pathsIn(section(rd, "Do not touch"));
  const diff = git(p, ["diff", "--name-only", `${base}...HEAD`]);
  if (!diff.ok) return [];
  const files = diff.out.split("\n").filter((f) => f && !f.startsWith(`${cfg.ticketsDir}/`) && !f.startsWith(`${cfg.jobDir}/`));
  const out = [];
  for (const f of files) {
    if (forbidden.some((x) => under(f, x))) out.push(`${f} is under Do not touch`);
    else if (areas.length && !areas.some((x) => under(f, x))) out.push(`${f} is outside Affected areas`);
  }
  return out;
}

async function appHealth(cfg) {
  const paths = cfg.app?.healthPaths ?? [];
  const port = cfg.app?.ports?.main;
  if (!paths.length || !port) return [];
  const out = [];
  for (const path of paths) {
    try {
      const r = await fetch(`http://localhost:${port}${path}`, { signal: AbortSignal.timeout(10000) });
      if (r.status >= 500) out.push(`${path} answered ${r.status}`);
    } catch (e) {
      // Nothing listening isn't a fault here: the app may simply not be running.
      const code = e?.cause?.code;
      if (!["ECONNREFUSED", "ENOTFOUND", "EADDRNOTAVAIL", "EHOSTUNREACH"].includes(code)) out.push(`${path}: ${e.message}`);
    }
  }
  return out;
}

async function main() {
  const root = rootOf(process.cwd());
  const cfg = loadProject(root);
  const problems = [];
  const staleAfter = cfg.limits.stepStaleMinutes;
  for (const p of checkouts(root)) {
    const claim = readJson(join(p, cfg.jobDir, "ticket.json"));
    if (!claim?.ticket) continue; // not one of ours
    const t = claim.ticket;
    const status = readJson(join(p, cfg.jobDir, t, "status.json"));
    if (!status) { problems.push({ kind: "stale", ticket: t, detail: `no readable status file in ${p}` }); continue; }
    if (status.step === "done") continue;
    if (status.step === "blocked") { problems.push({ kind: "blocked", ticket: t, detail: status.note ?? "no note" }); continue; }
    if (status.step === "failed") { problems.push({ kind: "failed", ticket: t, detail: status.note ?? "no note" }); continue; }
    const quiet = Math.round((NOW - newestActivity(p, status)) / MIN);
    if (quiet >= staleAfter) problems.push({ kind: "stale", ticket: t, detail: `no activity for ${quiet} minutes, step ${status.step ?? "?"}. Ask before stopping it: a long test run is quiet too` });
    for (const d of drift(p, cfg, t, claim.base)) problems.push({ kind: "drift", ticket: t, detail: d });
  }
  for (const d of await appHealth(cfg)) problems.push({ kind: "app-down", ticket: "-", detail: d });

  // Repeat a problem only after a while, so a rejected alarm doesn't come back every round.
  const stateFile = join(root, cfg.jobDir, "watch-state.json");
  const seen = readJson(stateFile, {});
  const next = {};
  for (const pr of problems) {
    const key = `${pr.kind}|${pr.ticket}|${pr.detail.replace(/\d+ minutes/, "")}`;
    const last = seen[key] ?? 0;
    if (NOW - last >= REPEAT_MINUTES * MIN) {
      console.log(JSON.stringify(pr));
      next[key] = NOW;
    } else next[key] = last;
  }
  mkdirSync(join(root, cfg.jobDir), { recursive: true });
  writeFileSync(stateFile, JSON.stringify(next, null, 2));
}

main().catch((e) => process.stderr.write(`check.mjs: ${e.message}\n`)).finally(() => { process.exitCode = 0; });
