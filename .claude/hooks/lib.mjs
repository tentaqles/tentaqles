// Shared helpers for gate.mjs and watch/check.mjs. Node 20.11+, no dependencies.
import { spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";

export const DEFAULTS = {
  confirmed: false,
  stack: "unknown",
  capabilities: { browser: false, mockups: false, screenshots: false, ports: false, testAccounts: false, teams: false },
  ticketsDir: "docs/tickets",
  jobDir: ".job",
  mainBranch: "main",
  requirementId: "REQ-\\d+",
  highRiskPaths: [],
  gates: { round: [], release: [] },
  limits: { parallelTickets: 3, roundCheckpoint: 5, stopBlocks: 3, watchMinutes: 10, stepStaleMinutes: 45, commandTimeoutMinutes: 30, runningStaleMinutes: 60 },
  app: {},
};

export function git(cwd, args, timeout = 5000) {
  const r = spawnSync("git", args, { cwd, encoding: "utf8", timeout, windowsHide: true });
  return { ok: r.status === 0, out: (r.stdout ?? "").trim(), err: (r.stderr ?? "").trim() };
}

/** The checkout this directory belongs to: state follows cwd, not CLAUDE_PROJECT_DIR. */
export function rootOf(cwd, timeout = 5000) {
  const r = git(cwd, ["rev-parse", "--show-toplevel"], timeout);
  return r.ok ? resolve(r.out) : resolve(cwd);
}

export function loadProject(root) {
  const p = join(root, ".claude", "project.json");
  if (!existsSync(p)) throw new UserError(`no .claude/project.json in ${root}: run /write-tickets to create the profile`);
  let raw;
  try {
    raw = JSON.parse(readFileSync(p, "utf8"));
  } catch (e) {
    throw new UserError(`.claude/project.json is not valid JSON: ${e.message}`);
  }
  return {
    ...DEFAULTS, ...raw,
    capabilities: { ...DEFAULTS.capabilities, ...raw.capabilities },
    gates: { ...DEFAULTS.gates, ...raw.gates },
    limits: { ...DEFAULTS.limits, ...raw.limits },
    app: { ...DEFAULTS.app, ...raw.app },
  };
}

export class UserError extends Error {}

export const sha256 = (s) => createHash("sha256").update(s).digest("hex");
export const normalizeEol = (s) => s.replace(/\r\n/g, "\n");
export const readText = (p) => normalizeEol(readFileSync(p, "utf8"));

export function readJson(p, fallback = null) {
  try {
    return JSON.parse(readFileSync(p, "utf8"));
  } catch {
    return fallback;
  }
}

/** Finds a real bash. On Windows `bash` can be the WSL launcher in System32. */
export function findBash() {
  if (process.platform !== "win32") return "bash";
  const candidates = [process.env.CLAUDE_CODE_GIT_BASH_PATH];
  const exec = git(process.cwd(), ["--exec-path"]);
  if (exec.ok) {
    // <git>/mingw64/libexec/git-core -> <git>/bin/bash.exe
    candidates.push(join(dirname(dirname(dirname(exec.out))), "bin", "bash.exe"));
  }
  candidates.push("C:\\Program Files\\Git\\bin\\bash.exe");
  for (const c of candidates) if (c && existsSync(c)) return c;
  return "bash";
}

export function listTickets(root, cfg) {
  const dir = join(root, cfg.ticketsDir);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((n) => /^\d+[-\w.]*$/.test(n) && statSync(join(dir, n)).isDirectory())
    .sort();
}

export function resolveTicket(root, cfg, arg) {
  const all = listTickets(root, cfg);
  if (all.includes(arg)) return arg;
  const hits = all.filter((n) => n.startsWith(arg + "-") || n === arg);
  if (hits.length === 1) return hits[0];
  throw new UserError(hits.length ? `ticket ${arg} is ambiguous: ${hits.join(", ")}` : `no ticket ${arg} under ${cfg.ticketsDir}`);
}

export const ticketNumber = (t) => t.split("-")[0];
export const treeOf = (root, rev = "HEAD") => git(root, ["rev-parse", `${rev}^{tree}`]);

/** Parses the checks of an eval.md. */
export function parseChecks(text) {
  const out = [];
  const parts = text.split(/^### /m).slice(1);
  for (const part of parts) {
    const m = part.match(/^([A-Z]+-\d+)\b(.*)/);
    if (!m) continue;
    const field = (name) => {
      const f = part.match(new RegExp(`^- \\*\\*${name}\\*\\*:(.*)$`, "m"));
      return f ? f[1].trim() : null;
    };
    const steps = part.match(/^- \*\*Steps\*\*:[^\n]*\n((?:[ \t]+\S[^\n]*\n?)*)/m);
    const inline = field("Steps");
    out.push({
      id: m[1], title: m[2].trim(), covers: field("Covers"), method: field("Method"),
      steps: (inline || (steps ? steps[1].trim() : "")) || "", expect: field("Expect"),
    });
  }
  return out;
}

export function field(text, name) {
  const m = text.match(new RegExp(`^\\*\\*${name}:\\*\\*\\s*(.*)$`, "m"));
  return m ? m[1].trim() : null;
}
export const riskLevel = (rd) => (field(rd, "Risk") ?? "").match(/^(Low|Medium|High)\b/)?.[1] ?? null;

export function section(text, heading) {
  const m = text.match(new RegExp(`^## ${heading}[^\\n]*\\n([\\s\\S]*?)(?=^## |(?![\\s\\S]))`, "m"));
  return m ? m[1] : "";
}

/** Parses a results file: `ID | PASS or FAIL or HUMAN | evidence`. */
export function parseResults(text) {
  const out = {};
  for (const line of normalizeEol(text).split("\n")) {
    const m = line.match(/^\s*([A-Z]+-\d+)\s*\|\s*([A-Za-z]+)\s*\|?\s*(.*)$/);
    if (m) out[m[1]] = { status: m[2].toUpperCase(), evidence: m[3].trim() };
  }
  return out;
}
