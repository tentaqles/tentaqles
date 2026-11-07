#!/usr/bin/env node
// Eval gate for the write-evals and review-loop skills.
//
//   node .claude/hooks/eval-gate.mjs lint <feature>          # is the eval well formed? (any status; run while drafting)
//   node .claude/hooks/eval-gate.mjs check <feature> --run   # run the gate commands, then: does the feature pass its eval?
//   node .claude/hooks/eval-gate.mjs check <feature>         # same, using the last --run (no commands run)
//   node .claude/hooks/eval-gate.mjs status                  # every eval, its risk and its state
//   node .claude/hooks/eval-gate.mjs config                  # the settings in use (.claude/evals.config.json + defaults)
//   node .claude/hooks/eval-gate.mjs hook                    # PreToolUse / Stop hook, reads the hook JSON on stdin
//
// <feature> is the folder name under featuresDir, e.g. `team-invites` for specs/features/team-invites/.
//
// What the hook enforces:
//   - An eval moves to "In review" or "Approved" only when it lints clean (well formed, every requirement
//     covered, the categories its risk needs).
//   - An eval moves to "Approved" only when the user's latest message is a yes.
//   - An approved eval's checks can't be changed or removed (new checks can be added), and its risk can't change.
//     To change it, set it back to "In review" and ask the user again.
//   - An eval moves to "Passing", and an RD to "Shipped", only when `check` passes.
//   - No edits to app code while any eval is waiting for review ("Draft" or "In review").
//   - Specs are edited with Edit/Write, never with shell commands, so the gate always sees the change.
//   - Stopping with "done" in the last message while an approved eval still fails is sent back once.

import { existsSync, mkdirSync, readFileSync, readdirSync, statSync, writeFileSync } from "node:fs";
import { spawnSync } from "node:child_process";
import { join, relative, resolve, sep } from "node:path";

const ROOT = process.env.CLAUDE_PROJECT_DIR ?? resolve(import.meta.dirname, "../..");

// ---------- config

const DEFAULTS = {
  featuresDir: "specs/features",
  evidenceDir: "evidence",
  requirementId: "REQ-\\d+",
  mainBranch: "main",
  codePaths: ["src", "app", "lib", "server", "api", "scripts", "tests", "test"],
  gateCommands: [],
};
function loadConfig() {
  const p = join(ROOT, ".claude/evals.config.json");
  if (!existsSync(p)) return { ...DEFAULTS };
  try {
    return { ...DEFAULTS, ...JSON.parse(readFileSync(p, "utf8")) };
  } catch (e) {
    throw new Error(`.claude/evals.config.json is not valid JSON: ${e.message}`);
  }
}
const CONFIG = loadConfig();
const FEATURES_REL = CONFIG.featuresDir.replace(/\/+$/, "");
const EVIDENCE_REL = CONFIG.evidenceDir.replace(/\/+$/, "");
const FEATURES = join(ROOT, FEATURES_REL);
const EVIDENCE = join(ROOT, EVIDENCE_REL);
const REQ = CONFIG.requirementId;

const CATEGORIES = { U: "use", F: "failure", R: "regression", Q: "quality", S: "security and privacy", D: "design" };
// Which categories an eval must have, by the feature's risk (set by the rate-risk skill).
const REQUIRED = { low: ["U", "F", "Q"], medium: ["U", "F", "R", "Q", "S"], high: ["U", "F", "R", "Q", "S"] };
const HIGH_MIN_SECURITY = 3;
const WAITING = ["draft", "in review"];
const SKIP_DIRS = new Set(["node_modules", ".git", ".next", ".nuxt", ".svelte-kit", ".turbo", "dist", "build", "out", "coverage", "__pycache__", ".venv", "venv"]);

// ---------- parsing

const rel = (p) => relative(ROOT, p);
const norm = (f) => {
  let s = String(f ?? "").trim().replace(/\\/g, "/").replace(/\/(eval|rd)\.md$/, "").replace(/\/+$/, "");
  if (s.startsWith(FEATURES_REL + "/")) s = s.slice(FEATURES_REL.length + 1);
  return /^[\w][\w.-]*$/.test(s) ? s : null;
};
const evalPath = (f) => join(FEATURES, f, "eval.md");
const stampPath = (f) => join(EVIDENCE, f, "gate-run.json");
const read = (p) => (existsSync(p) ? readFileSync(p, "utf8") : null);
const headerField = (md, name) => md?.match(new RegExp(`^\\|\\s*\\*\\*${name}\\*\\*\\s*\\|\\s*(.*?)\\s*\\|\\s*$`, "mi"))?.[1] ?? null;
const clean = (v) => (v ?? "").replace(/[*`_]/g, "").trim();
const evalStatus = (md) => clean(headerField(md, "Eval status")).toLowerCase();
const riskOf = (md) => clean(headerField(md, "Risk")).toLowerCase().match(/^(low|medium|high)\b/)?.[1] ?? null;
const rdShipped = (md) => /^shipped\b/i.test(clean(headerField(md, "Status")));
const empty = (v) => !v || /^(—|-|–|n\/a|none|pending|todo|tbd)?$/i.test(clean(v));

function parseChecks(md) {
  const heads = [...md.matchAll(/^###\s+([A-Z])-(\d+)\b[^\n]*$/gm)];
  return heads.map((h, i) => {
    const body = md.slice(h.index + h[0].length, heads[i + 1]?.index ?? md.length).split(/^##\s/m)[0];
    const field = (name) => body.match(new RegExp(`^-\\s*\\*\\*${name}\\*\\*:?\\s*(.*)$`, "mi"))?.[1]?.trim() ?? null;
    return {
      id: `${h[1]}-${h[2]}`,
      cat: h[1],
      title: h[0].replace(/^###\s+/, ""),
      covers: field("Covers"),
      method: field("Method"),
      steps: /^-\s*\*\*Steps\*\*/mi.test(body),
      expect: field("Expect"),
      result: field("Result"),
      evidence: field("Evidence"),
      // Everything the user approved: the check minus its Result and Evidence lines.
      signature: (h[0] + body)
        .split("\n")
        .filter((l) => !/^-\s*\*\*(Result|Evidence)\*\*/i.test(l.trim()))
        .join(" ")
        .replace(/\s+/g, " ")
        .trim(),
    };
  });
}

const methodOk = (m) => /^(test\s+\S|browser\b|command\b|inspect\b)/i.test(clean(m));
const executed = (m) => /^(test\s+\S|browser\b|command\b)/i.test(clean(m));

// ---------- lint: is the eval written well enough to build against?

function lintEval(f, md = read(evalPath(f))) {
  const problems = [];
  if (!md) return [`${rel(evalPath(f))} does not exist`];
  const rd = read(join(FEATURES, f, "rd.md"));
  if (!rd) problems.push(`${FEATURES_REL}/${f}/rd.md does not exist; the eval needs a requirements doc next to it`);
  if (!headerField(md, "Eval status")) problems.push('header table has no "Eval status" row');
  const risk = riskOf(md);
  if (!risk) problems.push('header table needs a "Risk" row of Low, Medium or High (run the rate-risk skill)');
  const mockup = headerField(md, "Mockup");
  if (mockup === null) problems.push('header table has no "Mockup" row (link mockup.html, or write "None: no UI")');
  const hasMockup = mockup && /mockup\.html/.test(mockup);
  if (hasMockup && !existsSync(join(FEATURES, f, "mockup.html"))) problems.push(`Mockup is linked but ${FEATURES_REL}/${f}/mockup.html is missing`);

  const checks = parseChecks(md);
  const ids = new Set();
  for (const c of checks) {
    if (ids.has(c.id)) problems.push(`${c.id} is used twice`);
    ids.add(c.id);
    if (!CATEGORIES[c.cat]) problems.push(`${c.id}: unknown category ${c.cat} (use ${Object.keys(CATEGORIES).join(", ")})`);
    for (const k of ["covers", "method", "expect", "result"]) if (c[k] === null) problems.push(`${c.id}: missing "- **${k[0].toUpperCase() + k.slice(1)}**:" line`);
    if (!c.steps) problems.push(`${c.id}: missing "- **Steps**:" list`);
    if (c.method && !methodOk(c.method)) problems.push(`${c.id}: Method must start with "test <name>", "browser", "command" or "inspect"`);
  }
  for (const cat of REQUIRED[risk ?? "medium"]) if (!checks.some((c) => c.cat === cat)) problems.push(`no ${CATEGORIES[cat]} checks (${cat}-nn)${risk ? `; ${risk} risk features need them` : ""}`);
  if (hasMockup && !checks.some((c) => c.cat === "D")) problems.push("feature has a mockup but no design checks (D-nn)");
  if (risk === "high") {
    const s = checks.filter((c) => c.cat === "S");
    if (s.length < HIGH_MIN_SECURITY) problems.push(`high risk features need at least ${HIGH_MIN_SECURITY} security and privacy checks (has ${s.length})`);
    if (s.length && !s.some((c) => executed(c.method))) problems.push("high risk: at least one S check must actually attack the running app (Method test, browser or command), not only read code");
  }

  // Every requirement in the RD must be covered by at least one check.
  if (rd) {
    const reqs = new Set(rd.match(new RegExp(`\\b${REQ}\\b`, "g")) ?? []);
    if (!reqs.size) problems.push(`rd.md has no numbered requirements (ids matching /${REQ}/, e.g. REQ-1)`);
    const covered = checks.map((c) => c.covers ?? "").join(" ");
    const missing = [...reqs].filter((r) => !new RegExp(`\\b${r.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}(?![\\w.])`).test(covered));
    if (missing.length) problems.push(`RD requirements no check covers: ${missing.join(", ")}`);
  }
  return problems;
}

// ---------- check: does the build pass the eval right now?

function newestMtime(abs) {
  if (!existsSync(abs)) return 0;
  const s = statSync(abs);
  if (!s.isDirectory()) return s.mtimeMs;
  if (SKIP_DIRS.has(abs.split(sep).pop()) || abs === EVIDENCE) return 0;
  return Math.max(0, ...readdirSync(abs).map((n) => newestMtime(join(abs, n))));
}
const codeChangedAt = () => Math.max(0, ...CONFIG.codePaths.map((p) => newestMtime(join(ROOT, p))));

function runGateCommands(f) {
  const results = [];
  for (const cmd of CONFIG.gateCommands) {
    process.stderr.write(`eval-gate: running ${cmd} …\n`);
    const r = spawnSync(cmd, { cwd: ROOT, shell: true, encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
    const out = `${r.stdout ?? ""}${r.stderr ?? ""}`.trim().split("\n");
    results.push({ cmd, exit: r.status, tail: out.slice(-15).join("\n") });
    if (r.status !== 0) break;
  }
  mkdirSync(join(EVIDENCE, f), { recursive: true });
  writeFileSync(stampPath(f), JSON.stringify({ at: new Date().toISOString(), results }, null, 2));
}

function checkEval(f, { md = read(evalPath(f)), run = false } = {}) {
  if (run && md) runGateCommands(f);
  const problems = lintEval(f, md);
  if (!md) return problems;
  if (CONFIG.confirmed !== true) problems.push('.claude/evals.config.json isn\'t set up for this project yet ("confirmed" is not true): fill in gateCommands and the app commands (write-evals step 0) so the gate runs your real lint, build and tests');
  else if (!CONFIG.gateCommands.length) problems.push(".claude/evals.config.json has no gateCommands; list the lint, build and test commands that must pass");
  const status = evalStatus(md);
  if (!["approved", "passing"].includes(status)) problems.push(`Eval status is "${status || "missing"}"; the user must approve the eval before it can pass`);
  if (empty(headerField(md, "Approved"))) problems.push('"Approved" row is empty; it records who approved the eval and when');

  for (const c of parseChecks(md)) {
    if (!/^pass\b/i.test(clean(c.result))) problems.push(`${c.id} is not PASS (Result: ${c.result || "empty"})`);
    if (empty(c.evidence)) problems.push(`${c.id} has no evidence`);
    for (const [, p] of (c.evidence ?? "").matchAll(/`([^`\s]+)`/g))
      if ((p.startsWith(EVIDENCE_REL + "/") || p.startsWith(FEATURES_REL + "/")) && !existsSync(join(ROOT, p))) problems.push(`${c.id}: evidence file ${p} does not exist`);
  }

  // Results must be of the current code: recorded, and the gate commands run, after the last code change.
  const codeAt = codeChangedAt();
  if (existsSync(evalPath(f)) && statSync(evalPath(f)).mtimeMs < codeAt) problems.push("code changed after the eval's results were recorded; verify again");
  if (CONFIG.gateCommands.length) {
    const stamp = read(stampPath(f));
    if (!stamp) problems.push(`the gate commands haven't run for this feature; run \`node .claude/hooks/eval-gate.mjs check ${f} --run\``);
    else {
      const s = JSON.parse(stamp);
      const failed = s.results.find((r) => r.exit !== 0);
      if (failed) problems.push(`\`${failed.cmd}\` failed (exit ${failed.exit}):\n${failed.tail.split("\n").slice(-8).join("\n")}`);
      else if (s.results.length < CONFIG.gateCommands.length) problems.push("not every gate command ran; run `check --run` again");
      if (statSync(stampPath(f)).mtimeMs < codeAt) problems.push(`code changed after the gate commands last ran; run \`node .claude/hooks/eval-gate.mjs check ${f} --run\``);
    }
  }
  return problems;
}

const allEvals = () =>
  existsSync(FEATURES)
    ? readdirSync(FEATURES)
        .filter((d) => existsSync(evalPath(d)))
        .map((f) => {
          const md = read(evalPath(f));
          return { f, status: evalStatus(md), risk: riskOf(md) };
        })
    : [];

// ---------- hook

function transcript(path) {
  if (!path || !existsSync(path)) return [];
  return readFileSync(path, "utf8")
    .split("\n")
    .filter(Boolean)
    .map((l) => {
      try {
        return JSON.parse(l);
      } catch {
        return null;
      }
    })
    .filter(Boolean);
}

const stripReminders = (t) => t.replace(/<system-reminder>[\s\S]*?<\/system-reminder>/g, "").trim();
const textOf = (c) =>
  typeof c === "string" ? c : Array.isArray(c) ? c.map((x) => (typeof x === "string" ? x : x?.text ?? (typeof x?.content === "string" ? x.content : textOf(x?.content)))).join("\n") : "";

// The user's latest words: a typed message, or the answers they picked in an AskUserQuestion dialog.
function lastUserWords(entries) {
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i];
    if (e.type !== "user" || e.isMeta || e.isCompactSummary) continue;
    const content = e.message?.content;
    const results = Array.isArray(content) ? content.filter((x) => x?.type === "tool_result") : [];
    if (results.length) {
      const t = textOf(results.map((x) => x.content));
      if (/User has answered/i.test(t)) return [...t.matchAll(/="([^"]*)"/g)].map((m) => m[1]).join("\n") || t;
      continue;
    }
    const t = stripReminders(textOf(content));
    if (t && !t.startsWith("<")) return t;
  }
  return "";
}

const isYes = (t) =>
  /\b(yes|yep|yeah|approved?|lgtm|looks good|go ahead|build it|proceed|ship it|sounds good)\b/i.test(t) &&
  !/^\s*no\b|\b(not yet|don't|do not|wait|hold on|but|change|instead|except|reject|request changes)\b/i.test(t);

function lastAssistantText(input, entries) {
  if (typeof input.last_assistant_message === "string") return input.last_assistant_message;
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i];
    if (e.type !== "assistant") continue;
    const t = (e.message?.content ?? []).filter((x) => x?.type === "text").map((x) => x.text).join("\n");
    if (t) return t;
  }
  return "";
}

// File content after the tool call, without performing it.
function afterEdit(tool, ti, before) {
  if (tool === "Write") return ti.content ?? "";
  const edits = tool === "MultiEdit" ? ti.edits ?? [] : [ti];
  let s = before ?? "";
  for (const e of edits) s = e.replace_all ? s.split(e.old_string).join(e.new_string) : s.replace(e.old_string, () => e.new_string);
  return s;
}

const fmt = (problems) => problems.slice(0, 12).map((p) => `  - ${p}`).join("\n") + (problems.length > 12 ? `\n  - …and ${problems.length - 12} more` : "");
const inCode = (path) => CONFIG.codePaths.some((p) => path === p || path.startsWith(p.replace(/\/+$/, "") + "/"));

function preToolUse(input) {
  const tool = input.tool_name;
  const ti = input.tool_input ?? {};

  if (tool === "Bash") {
    const cmd = ti.command ?? "";
    const cwd = input.cwd ?? ROOT;
    const isSpec = (tok) => rel(resolve(cwd, tok.replace(/^["']|["']$/g, ""))).startsWith(FEATURES_REL + "/");
    // Only commands that write into the specs: redirects and tee into them, in place rewrites mentioning them, cp/mv onto them.
    const touches = cmd.split(/&&|\|\||;|\n/).some((seg) => {
      const toks = seg.trim().split(/\s+/);
      const targets = [...seg.matchAll(/(?:>>?|\btee\s+(?:-a\s+)?)\s*(\S+)/g)].map((m) => m[1]);
      if (targets.some(isSpec)) return true;
      if (/\b(sed|perl)\s+-\S*i|\bpython3?\s|\bnode\s+-e/.test(seg) && toks.some(isSpec)) return true;
      return /^(cp|mv)$/.test(toks[0]) && isSpec(toks.at(-1));
    });
    if (touches) return deny("Edit specs (rd.md, eval.md) with Edit/Write, not shell commands, so the eval gate can check the change.");
    return null;
  }

  const file = ti.file_path ?? ti.notebook_path;
  if (!file) return null;
  const path = rel(resolve(ROOT, file));
  const before = read(join(ROOT, path));
  const after = tool === "NotebookEdit" ? null : afterEdit(tool, ti, before);

  const spec = path.startsWith(FEATURES_REL + "/") && path.slice(FEATURES_REL.length + 1).match(/^([^/]+)\/(eval|rd)\.md$/);
  if (spec && after !== null) {
    const [, f, kind] = spec;
    if (kind === "eval") {
      const from = evalStatus(before);
      const to = evalStatus(after);
      // An eval goes to the user, or gets approved, only when it's well formed.
      if (["in review", "approved"].includes(to) && from !== to) {
        const problems = lintEval(f, after);
        if (problems.length) return deny(`${f}'s eval isn't well formed yet, so it can't move to "${to}". Fix these, then try again:\n${fmt(problems)}`);
      }
      if (to === "approved" && from !== "approved") {
        const said = lastUserWords(transcript(input.transcript_path));
        if (!isYes(said))
          return deny(`Only the user can approve ${f}'s eval. Their latest message isn't a clear yes${said ? ` ("${said.slice(0, 120)}")` : ""}. Show them eval.md${existsSync(join(FEATURES, f, "mockup.html")) ? " and mockup.html" : ""} and ask for approval, or apply the changes they asked for and ask again.`);
      }
      if (["approved", "passing"].includes(from) && ["approved", "passing"].includes(to)) {
        const was = parseChecks(before ?? "");
        const now = Object.fromEntries(parseChecks(after).map((c) => [c.id, c]));
        const changed = was.filter((c) => now[c.id]?.signature !== c.signature).map((c) => c.id);
        if (changed.length || riskOf(before) !== riskOf(after))
          return deny(`${f}'s eval is approved, so its checks and risk are fixed (changed or removed: ${[...changed, ...(riskOf(before) !== riskOf(after) ? ["Risk"] : [])].join(", ")}). Only Result and Evidence lines change, and new checks can be added. To change the eval, set "Eval status" back to "In review" and ask the user.`);
      }
      if (to === "passing" && from !== "passing") {
        const problems = checkEval(f, { md: after });
        if (problems.length) return deny(`${f}'s eval doesn't pass yet, so it can't be marked Passing:\n${fmt(problems)}`);
      }
    }
    if (kind === "rd" && rdShipped(after) && !rdShipped(before)) {
      const problems = checkEval(f);
      if (problems.length) return deny(`${f} can't be marked Shipped until its eval passes (\`node .claude/hooks/eval-gate.mjs check ${f} --run\`):\n${fmt(problems)}`);
    }
    return null;
  }

  if (inCode(path)) {
    const waiting = allEvals().filter((e) => WAITING.includes(e.status));
    if (waiting.length)
      return deny(`${waiting.map((e) => e.f).join(", ")} ${waiting.length > 1 ? "have evals" : "has an eval"} waiting for the user's review, so no code changes yet. Get their yes first. If they've set the feature aside, ask them, then set its "Eval status" to Parked.`);
  }
  return null;
}

function stop(input) {
  if (input.stop_hook_active) return null;
  const building = allEvals().filter((e) => e.status === "approved");
  if (!building.length) return null;
  const said = lastAssistantText(input, transcript(input.transcript_path));
  const claims = /\b(done|complete[ds]?|finished|shipped|implemented|ready to (ship|merge)|all (checks|evals?) (pass|passing|passed))\b/i;
  const hedged = /\bnot (yet )?(done|complete|finished|shipped)\b/i;
  if (!claims.test(said) || hedged.test(said)) return null;
  const failing = building.map((e) => ({ ...e, problems: checkEval(e.f) })).filter((e) => e.problems.length);
  if (!failing.length) return null;
  return {
    decision: "block",
    reason:
      failing.map((e) => `${e.f}'s eval is approved but not passing, so the feature isn't done:\n${fmt(e.problems)}`).join("\n\n") +
      "\n\nKeep building until `node .claude/hooks/eval-gate.mjs check <feature> --run` passes, or tell the user plainly what is left. Don't describe the feature as done.",
  };
}

const deny = (reason) => ({ hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: reason } });

// ---------- CLI

const [mode, arg, ...flags] = process.argv.slice(2);
if (mode === "hook") {
  let raw = "";
  for await (const chunk of process.stdin) raw += chunk;
  let out = null;
  try {
    const input = JSON.parse(raw || "{}");
    out = input.hook_event_name === "Stop" ? stop(input) : preToolUse(input);
  } catch (e) {
    // A bug in the gate must not wedge the session; say so on stderr and let the call through.
    console.error(`eval-gate: ${e.stack ?? e}`);
  }
  if (out) process.stdout.write(JSON.stringify(out));
  process.exit(0);
} else if (mode === "lint" || mode === "check") {
  const f = norm(arg);
  if (!f) {
    console.error(`usage: eval-gate.mjs ${mode} <feature>${mode === "check" ? " [--run]" : ""}`);
    process.exit(2);
  }
  const problems = mode === "lint" ? lintEval(f) : checkEval(f, { run: flags.includes("--run") });
  if (problems.length) {
    console.log(`${f} ${mode === "lint" ? "eval is not well formed" : "does not pass its eval"}:\n${fmt(problems)}`);
    process.exit(1);
  }
  console.log(`${f} ${mode === "lint" ? "eval is well formed" : "passes its eval"}.`);
} else if (mode === "status") {
  const evals = allEvals();
  if (!evals.length) console.log(`No evals yet in ${FEATURES_REL}/.`);
  for (const e of evals) console.log(`${e.f.padEnd(32)} ${(e.risk ?? "no risk").padEnd(8)} ${e.status || "(no status)"}`);
} else if (mode === "config") {
  console.log(JSON.stringify(CONFIG, null, 2));
} else {
  console.error("usage: eval-gate.mjs lint <feature> | check <feature> [--run] | status | config | hook");
  process.exit(2);
}
