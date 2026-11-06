#!/usr/bin/env node
// The build loop's gate: builder hooks plus a CLI. These are speed bumps against a
// model's mistakes and shortcuts, not a security boundary. See .claude/README.md.
//
//   lint <t>                 is the ticket well formed?
//   approve <t>              record the human's yes (run by /write-tickets only)
//   approval <t>             does the approval still match?
//   board <t> <state>        draft | ready | parked | landed
//   claim <t>                this worktree works on <t>
//   step <t> <step> [--round N] [note]
//   block <t> <note>         a question only a person can answer
//   run <t or fix-slug> round  run the round gate on this exact tree
//   stamped <fix-slug>       has it passed for this tree? (a fix's lighter landing gate)
//   risky <base> [head]      files on High-risk paths in the range; exit 1 if any
//   record <t> <file>        store check results against this exact tree
//   check <t>                is this exact tree proven?
//   carry <t> --from <dir>   copy job state from another worktree
//   human <t> <id> pass|fail [note]
//   release [--run]          open human checks, then the full gate
//   park <t>                 set parked and commit it
//   rule <line>              append an approved rule to REVIEW.md and commit it
//   job <key> k=v ...        the router's job list; jobs prints it
//   status | doctor
//   hook pre | hook stop     builder hooks
import { spawnSync } from "node:child_process";
import { appendFileSync, cpSync, existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, isAbsolute, join, relative, resolve } from "node:path";
import {
  UserError, field, findBash, git, listTickets, loadProject, normalizeEol, parseChecks, parseResults,
  readJson, readText, resolveTicket, riskLevel, rootOf, section, sha256, ticketNumber, treeOf,
} from "./lib.mjs";

const BOARD_STATES = ["draft", "ready", "parked", "landed"];
const METHODS = ["test", "command", "inspect", "human", "browser"];
const REQUIRED = { Low: ["U", "F", "Q"], Medium: ["U", "F", "Q", "R", "S"], High: ["U", "F", "Q", "R", "S"] };
// {what happens} is a template's placeholder. {"ok":true} and %{http_code} are real output.
const PLACEHOLDER = /(?<![%$])\{[A-Za-z][\w -]*\}/;

const paths = (root, cfg, t) => {
  const dir = join(root, cfg.ticketsDir, t);
  const job = join(root, cfg.jobDir, t);
  return {
    dir, job, rd: join(dir, "rd.md"), eval: join(dir, "eval.md"), approval: join(dir, "approval.json"),
    human: join(dir, "human.json"), status: join(job, "status.json"), marker: join(job, "hook-marker.json"),
    blocks: join(job, "stop-blocks.json"), board: join(root, cfg.ticketsDir, "BOARD.md"),
    claim: join(root, cfg.jobDir, "ticket.json"),
  };
};
const write = (p, s) => {
  mkdirSync(dirname(p), { recursive: true });
  writeFileSync(p, typeof s === "string" ? s : JSON.stringify(s, null, 2) + "\n");
};

// ---- lint
export function lint(root, cfg, t) {
  const p = paths(root, cfg, t);
  const bad = [];
  if (!existsSync(p.rd)) bad.push("rd.md is missing");
  if (!existsSync(p.eval)) bad.push("eval.md is missing");
  if (bad.length) return bad;
  const rd = readText(p.rd);
  const ev = readText(p.eval);
  const risk = riskLevel(rd);
  if (!field(rd, "Status")) bad.push("rd.md has no **Status:** line");
  if (!risk) bad.push("rd.md has no **Risk:** line starting Low, Medium or High");
  if (PLACEHOLDER.test(rd)) bad.push("rd.md still has a {placeholder}");
  const checks = parseChecks(ev);
  if (!checks.length) bad.push("eval.md has no checks");
  const seen = new Set();
  for (const c of checks) {
    if (seen.has(c.id)) bad.push(`${c.id}: duplicate id`);
    seen.add(c.id);
    for (const [name, v] of [["Covers", c.covers], ["Method", c.method], ["Steps", c.steps], ["Expect", c.expect]]) {
      if (!v) bad.push(`${c.id}: ${name} is empty`);
      else if (PLACEHOLDER.test(v)) bad.push(`${c.id}: ${name} still has a {placeholder}`);
    }
    const method = (c.method ?? "").split(/\s+/)[0];
    if (c.method && !METHODS.includes(method)) bad.push(`${c.id}: Method must be one of ${METHODS.join(", ")}`);
    if (method === "browser" && !cfg.capabilities.browser) bad.push(`${c.id}: browser method, but the browser capability is off in project.json`);
  }
  const cats = new Set(checks.map((c) => c.id.split("-")[0]));
  for (const need of REQUIRED[risk] ?? []) if (!cats.has(need)) bad.push(`${risk} risk needs at least one ${need} check`);
  if (risk === "High") {
    const s = checks.filter((c) => c.id.startsWith("S-"));
    if (s.length < 3) bad.push(`High risk needs at least 3 S checks, found ${s.length}`);
    if (s.length >= 3 && !s.some((c) => !["inspect", "human"].includes((c.method ?? "").split(/\s+/)[0]))) bad.push("High risk needs one S check that runs and attacks it, not only inspect");
  }
  const ui = field(rd, "UI") ?? "";
  if (cfg.capabilities.mockups && /mockup/i.test(ui)) {
    if (!existsSync(join(p.dir, "mockup.html"))) bad.push("rd.md names a mockup but mockup.html is missing");
    if (!cats.has("D")) bad.push("a mockup needs a D check");
  }
  const covered = checks.map((c) => c.covers ?? "").join(" ");
  const reqs = section(rd, "Requirements").match(new RegExp(cfg.requirementId, "g")) ?? [];
  for (const r of new Set(reqs)) if (!new RegExp(`\\b${r}\\b`).test(covered)) bad.push(`${r} isn't covered by any check`);
  return bad;
}

// ---- approval
const rdHash = (rd) => sha256(rd.replace(/^\*\*Status:\*\*.*\n?/m, ""));
function approvalProblems(root, cfg, t) {
  const p = paths(root, cfg, t);
  const a = readJson(p.approval);
  if (!a?.rd || !a?.eval) return ["no approval recorded: the ticket needs the owner's yes"];
  if (!existsSync(p.rd) || !existsSync(p.eval)) return ["rd.md or eval.md is missing"];
  const bad = [];
  if (rdHash(readText(p.rd)) !== a.rd) bad.push("rd.md changed after approval");
  const ev = Buffer.from(readText(p.eval));
  if (ev.length < a.eval.bytes || sha256(ev.subarray(0, a.eval.bytes)) !== a.eval.sha256) bad.push("approved checks in eval.md changed; new checks may only be appended");
  const mockup = join(p.dir, "mockup.html");
  if ((a.mockup ?? null) !== (existsSync(mockup) ? sha256(readText(mockup)) : null)) bad.push("mockup.html changed after approval");
  return bad;
}

// ---- board
function setBoard(root, cfg, t, state) {
  const p = paths(root, cfg, t);
  if (existsSync(p.rd)) {
    const rd = readFileSync(p.rd, "utf8");
    writeFileSync(p.rd, rd.replace(/^\*\*Status:\*\*.*$/m, `**Status:** ${state}`));
  }
  if (existsSync(p.board)) {
    const n = ticketNumber(t);
    const out = readFileSync(p.board, "utf8").split("\n").map((line) => {
      if (!new RegExp(`^\\|\\s*${n}\\s*\\|`).test(line)) return line;
      const cells = line.split("|");
      const i = cells.findIndex((c, idx) => idx > 1 && BOARD_STATES.includes(c.trim()));
      if (i > 0) cells[i] = ` ${state} `;
      return cells.join("|");
    });
    writeFileSync(p.board, out.join("\n"));
  }
}

// ---- status
const readStatus = (p) => readJson(p.status, {});
function setStatus(p, patch) {
  const now = new Date().toISOString();
  const cur = readStatus(p);
  const next = { ...cur, ...patch, updated: now };
  if (patch.step && patch.step !== cur.step) next.stepStarted = now;
  write(p.status, next);
  return next;
}

// ---- run
function isRunning(file, cfg) {
  const r = readJson(file);
  if (!r) return false;
  return Date.now() - (r.at ?? 0) < cfg.limits.runningStaleMinutes * 60000;
}
function clean(root) {
  const s = git(root, ["status", "--porcelain"]);
  return s.ok && s.out === "";
}
function runGate(root, cfg, stampDir, kind) {
  const cmds = cfg.gates[kind] ?? [];
  const tree = treeOf(root);
  if (!tree.ok) throw new UserError("can't read the current tree");
  if (!clean(root)) throw new UserError(`the tree has uncommitted changes: commit first, since a ${kind} gate stamp names an exact tree`);
  if (!cmds.length) throw new UserError(`project.json has no gates.${kind} commands`);
  const running = join(stampDir, `gate-${kind}.running`);
  write(running, { pid: process.pid, at: Date.now() });
  const bash = findBash();
  const results = [];
  let ok = true;
  try {
    for (const cmd of cmds) {
      const start = Date.now();
      const r = spawnSync(bash, ["-c", cmd], {
        cwd: root, encoding: "utf8", timeout: cfg.limits.commandTimeoutMinutes * 60000,
        maxBuffer: 64 * 1024 * 1024, windowsHide: true,
      });
      const output = `${r.stdout ?? ""}${r.stderr ?? ""}`;
      results.push({ cmd, code: r.status, ms: Date.now() - start, tail: output.slice(-2000) });
      console.log(`${r.status === 0 ? "ok  " : "FAIL"} ${cmd}`);
      if (r.status !== 0) {
        ok = false;
        console.log(output.slice(-2000));
        break;
      }
    }
  } finally {
    write(join(stampDir, `gate-${kind}.json`), { tree: tree.out, ok, at: new Date().toISOString(), results });
    try { writeFileSync(running, "{}"); } catch { /* the stamp is what counts */ }
  }
  return ok;
}
function stampOk(stampDir, kind, tree) {
  const s = readJson(join(stampDir, `gate-${kind}.json`));
  return !!s && s.ok === true && s.tree === tree;
}

// ---- check
export function checkTicket(root, cfg, t) {
  const p = paths(root, cfg, t);
  const bad = [];
  if (!cfg.confirmed) bad.push("project.json isn't confirmed by the owner");
  if (!cfg.gates.round?.length) bad.push("project.json has no gates.round commands");
  bad.push(...lint(root, cfg, t), ...approvalProblems(root, cfg, t));
  if (bad.length && !existsSync(p.eval)) return bad;
  const status = readStatus(p);
  if (status.released) bad.push("the builder's stop gate was released after repeated blocks: this ticket failed");
  if (!existsSync(p.marker)) bad.push("no record of the builder's hook running: run `gate doctor`, and check the folder is trusted");
  const tree = treeOf(root);
  if (!tree.ok) return [...bad, "can't read the current tree"];
  if (!stampOk(p.job, "round", tree.out)) bad.push("the round gate hasn't passed for this exact tree");
  const file = join(p.job, "results", `${tree.out}.md`);
  if (!existsSync(file)) return [...bad, "no results for this tree: the verifier has to run on it"];
  const results = parseResults(readFileSync(file, "utf8"));
  for (const c of parseChecks(readText(p.eval))) {
    const r = results[c.id];
    const human = (c.method ?? "").startsWith("human");
    if (!r) { bad.push(`${c.id}: no result`); continue; }
    if (r.status === "FAIL") bad.push(`${c.id}: FAIL: ${r.evidence}`);
    else if (r.status === "HUMAN") { if (!human) bad.push(`${c.id}: marked HUMAN, but its method is ${c.method}`); }
    else if (r.status !== "PASS") bad.push(`${c.id}: unknown result ${r.status}`);
    else {
      if (human) bad.push(`${c.id}: only a person can pass a human check`);
      if (!r.evidence) bad.push(`${c.id}: PASS with no evidence`);
      for (const m of r.evidence.matchAll(/`([^`]+)`/g)) {
        const path = m[1].replace(/:\d+(-\d+)?$/, ""); // file:line means the file
        if (/[\\/]/.test(path) && !/\s/.test(path) && !existsSync(resolve(root, path))) bad.push(`${c.id}: evidence ${path} doesn't exist`);
      }
    }
  }
  return bad;
}

// ---- release
function releaseProblems(root, cfg) {
  const open = [];
  for (const t of listTickets(root, cfg)) {
    const p = paths(root, cfg, t);
    if (!existsSync(p.rd) || !existsSync(p.eval) || field(readText(p.rd), "Status") !== "landed") continue;
    const human = readJson(p.human, {});
    for (const c of parseChecks(readText(p.eval))) {
      if (!(c.method ?? "").startsWith("human")) continue;
      if (human[c.id]?.result !== "pass") open.push({ t, id: c.id, title: c.title, failed: human[c.id]?.result === "fail" });
    }
  }
  return open;
}

// ---- hooks
function claimOf(root, cfg) {
  const c = readJson(join(root, cfg.jobDir, "ticket.json"));
  return c?.ticket ? c : null;
}
const deny = (reason) => process.stdout.write(JSON.stringify({
  hookSpecificOutput: { hookEventName: "PreToolUse", permissionDecision: "deny", permissionDecisionReason: `build loop gate: ${reason}` },
}));
const BLOCKED_SHELL = [
  [/(^|[\s;&|(])git(\s+-\S+(\s+[^\s-]\S*)?)*\s+push(?![\w-])/, "builders don't push: the lander does"],
  [/(^|[\s;&|(])git(\s+-\S+(\s+[^\s-]\S*)?)*\s+merge(?![\w-])/, "builders don't merge: the lander does"],
  [/(^|[\s;&|(])git(\s+-\S+(\s+[^\s-]\S*)?)*\s+rebase(?![\w-])/, "builders don't rebase: report it, and the lander decides"],
  [/(^|[\s;&|(])git(\s+-\S+(\s+[^\s-]\S*)?)*\s+reset\s+--hard\b/, "builders don't reset --hard"],
  [/(^|[\s;&|(])gh\s+pr\b/, "builders don't touch PRs"],
];
// Quoted text isn't a command: `git commit -m "push it"`, `echo "git push"`.
const stripQuoted = (s) => s.replace(/"(?:[^"\\]|\\.)*"|'[^']*'/g, '""');

function hookPre(input) {
  const cwd = input.cwd || process.cwd();
  const root = rootOf(cwd, 1500);
  const cfg = loadProject(root);
  const tool = input.tool_name ?? "";
  const claim = claimOf(root, cfg);
  if (claim) {
    try { write(paths(root, cfg, claim.ticket).marker, { at: new Date().toISOString(), agent: input.agent_type ?? null }); } catch { /* the marker is advisory here; check enforces it */ }
  }
  if (tool === "Bash" || tool === "PowerShell") {
    const cmd = stripQuoted(String(input.tool_input?.command ?? ""));
    if (/^\s*echo\b/.test(cmd)) return;
    for (const [re, why] of BLOCKED_SHELL) if (re.test(cmd)) return deny(why);
    return;
  }
  if (!["Edit", "Write", "MultiEdit", "NotebookEdit"].includes(tool)) return;
  if (!claim) return deny("no ticket is claimed in this worktree: the ticket-lead runs `gate claim <ticket>` first");
  const raw = String(input.tool_input?.file_path ?? input.tool_input?.notebook_path ?? "");
  const target = raw.replace(/\\/g, "/");
  const p = paths(root, cfg, claim.ticket);
  const abs = isAbsolute(raw) || /^[A-Za-z]:/.test(raw) ? resolve(raw) : resolve(cwd, target);
  const rel = relative(root, abs).replace(/\\/g, "/");
  const own = relative(root, p.dir).replace(/\\/g, "/");
  if ([`${own}/rd.md`, `${own}/eval.md`, `${own}/approval.json`, `${own}/human.json`].includes(rel)) {
    return deny(`${rel} is the owner's approved ticket: builders don't change it. If it's wrong, block with a note`);
  }
  const bad = approvalProblems(root, cfg, claim.ticket);
  if (bad.length) return deny(`${claim.ticket}: ${bad.join("; ")}`);
}

function hookStop(input) {
  const cwd = input.cwd || process.cwd();
  const root = rootOf(cwd, 1500);
  const cfg = loadProject(root);
  const claim = claimOf(root, cfg);
  const block = (why) => {
    process.stderr.write(`build loop gate: ${why}\n`);
    process.exit(2);
  };
  if (!claim) {
    // No ticket to record failure against, but this still must not block forever.
    const f = join(root, cfg.jobDir, "stop-blocks-unclaimed.json");
    const seen = readJson(f, { n: 0 });
    if (seen.n >= cfg.limits.stopBlocks) return;
    write(f, { n: seen.n + 1 });
    return block("no ticket is claimed in this worktree: the ticket-lead runs `gate claim <ticket>` first");
  }
  const p = paths(root, cfg, claim.ticket);
  const status = readStatus(p);
  if (status.step === "blocked" && status.note) return;
  if (status.released) return;
  const problem = (() => {
    if (!clean(root)) return "uncommitted changes: commit your work so the tree is clean";
    const ahead = git(root, ["rev-list", "--count", `${claim.base}..HEAD`], 1500);
    if (!ahead.ok || Number(ahead.out) < 1) return `no commit ahead of the base ${claim.base.slice(0, 8)}`;
    const tree = treeOf(root);
    if (!tree.ok || !stampOk(p.job, "round", tree.out)) return `the round gate hasn't passed for this tree: run \`node .claude/hooks/gate.mjs run ${claim.ticket} round\` and fix what fails`;
    const round = status.round ?? 0;
    if (!round || !existsSync(join(p.job, `round-${round}`, "builder.md"))) return `write your report to ${cfg.jobDir}/${claim.ticket}/round-${round || "N"}/builder.md`;
    return null;
  })();
  if (!problem) return;
  // A gate still running isn't the builder's fault, so it doesn't use up a block.
  if (isRunning(join(p.job, "gate-round.running"), cfg)) return block(`the round gate is still running: wait for it. (${problem})`);
  const counts = readJson(p.blocks, { n: 0 });
  if (counts.n >= cfg.limits.stopBlocks) {
    setStatus(p, { released: true, step: "failed", note: `stop gate released after ${counts.n} blocks: ${problem}` });
    return;
  }
  write(p.blocks, { n: counts.n + 1 });
  block(`${problem}. Then write your report and stop. If only a person can decide, run \`node .claude/hooks/gate.mjs block ${claim.ticket} "<the question>"\``);
}

async function readStdin(ms) {
  return new Promise((done) => {
    let buf = "";
    const timer = setTimeout(() => done(buf), ms);
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (d) => { buf += d; });
    process.stdin.on("end", () => { clearTimeout(timer); done(buf); });
    process.stdin.on("error", () => { clearTimeout(timer); done(buf); });
  });
}
async function runHook(kind) {
  // A crash here lets the call through: this is process, not security. `gate check`
  // at landing fails closed, so a broken gate can't land anything. Never stalls.
  const raw = await readStdin(1000);
  let log = join(process.cwd(), ".job", "gate-errors.log");
  try {
    const input = JSON.parse(raw);
    if (input.cwd) log = join(rootOf(input.cwd, 1500), ".job", "gate-errors.log");
    if (kind === "pre") hookPre(input);
    else if (kind === "stop") hookStop(input);
  } catch (e) {
    try {
      mkdirSync(dirname(log), { recursive: true });
      appendFileSync(log, `${new Date().toISOString()} hook ${kind}: ${e.message}\n`);
    } catch { /* nowhere left to log */ }
  }
  process.exit(0);
}

// ---- doctor
function doctor(root) {
  const bad = [];
  const say = (ok, msg) => { console.log(`${ok ? "ok  " : "FAIL"} ${msg}`); if (!ok) bad.push(msg); };
  const [major, minor] = process.versions.node.split(".").map(Number);
  say(major > 20 || (major === 20 && minor >= 11), `node ${process.versions.node} (20.11+)`);
  let cfg = null;
  try { cfg = loadProject(root); say(true, ".claude/project.json parses"); } catch (e) { say(false, e.message); }
  if (cfg) {
    say(cfg.confirmed, "project.json is confirmed by the owner");
    say((cfg.gates.round ?? []).length > 0, "gates.round has commands");
    say((cfg.gates.release ?? []).length > 0, "gates.release has commands");
  }
  const settings = readJson(join(root, ".claude", "settings.json"), {});
  say(settings?.worktree?.baseRef === "head", 'settings.json sets worktree.baseRef to "head", so workers branch from your checked-out commit');
  const bash = findBash();
  const v = spawnSync(bash, ["-c", "echo ok"], { encoding: "utf8", timeout: 5000, windowsHide: true });
  say(v.stdout?.trim() === "ok", `bash runs (${bash})`);
  // The trailing slash matters: a directory pattern doesn't match a path that doesn't exist yet.
  const ignored = (p) => git(root, ["check-ignore", "-q", `${p}/`]).ok;
  say(ignored(cfg?.jobDir ?? ".job"), `${cfg?.jobDir ?? ".job"}/ is gitignored`);
  say(ignored(".claude/worktrees"), ".claude/worktrees/ is gitignored");
  console.log('Note: agent hooks only run in a trusted folder. `gate check` refuses a ticket whose builder hook never ran.');
  return bad.length ? 1 : 0;
}

// ---- cli
function main(argv) {
  const [cmd, ...a] = argv;
  if (cmd === "hook") { runHook(a[0]); return null; }
  const root = rootOf(process.cwd());
  if (cmd === "doctor") return doctor(root);
  const cfg = loadProject(root);
  const ticket = () => {
    if (!a[0]) throw new UserError(`usage: gate ${cmd} <ticket> ...`);
    return resolveTicket(root, cfg, a[0]);
  };
  const report = (bad, okMsg) => {
    if (bad.length) { bad.forEach((b) => console.log(`- ${b}`)); return 1; }
    console.log(okMsg);
    return 0;
  };
  switch (cmd) {
    case "lint": { const t = ticket(); return report(lint(root, cfg, t), `${t}: well formed`); }
    case "approval": { const t = ticket(); return report(approvalProblems(root, cfg, t), `${t}: approval matches`); }
    case "approve": {
      const t = ticket();
      const p = paths(root, cfg, t);
      const bad = lint(root, cfg, t);
      if (bad.length) return report(bad, "");
      const ev = Buffer.from(readText(p.eval));
      const mockup = join(p.dir, "mockup.html");
      write(p.approval, {
        rd: rdHash(readText(p.rd)), eval: { bytes: ev.length, sha256: sha256(ev) },
        mockup: existsSync(mockup) ? sha256(readText(mockup)) : null, at: new Date().toISOString(),
      });
      console.log(`${t}: approved`);
      return 0;
    }
    case "board": {
      const t = ticket();
      const state = a[1];
      if (!BOARD_STATES.includes(state)) throw new UserError(`state must be one of ${BOARD_STATES.join(", ")}`);
      if (state === "ready") { const bad = approvalProblems(root, cfg, t); if (bad.length) return report(bad, ""); }
      if (state === "landed") {
        const bad = checkTicket(root, cfg, t);
        if (bad.length) return report(bad, "");
        const p = paths(root, cfg, t);
        const tree = treeOf(root).out;
        cpSync(join(p.job, "results", `${tree}.md`), join(p.dir, "results.md"));
        if (existsSync(join(p.job, "evidence"))) cpSync(join(p.job, "evidence"), join(p.dir, "evidence"), { recursive: true });
      }
      setBoard(root, cfg, t, state);
      console.log(`${t}: ${state}`);
      return 0;
    }
    case "claim": {
      const t = ticket();
      const bad = approvalProblems(root, cfg, t);
      if (bad.length) return report(bad, "");
      const p = paths(root, cfg, t);
      write(p.claim, { ticket: t, base: git(root, ["rev-parse", "HEAD"]).out });
      setStatus(p, { ticket: t, step: "claimed", branch: git(root, ["branch", "--show-current"]).out });
      console.log(`${t}: claimed`);
      return 0;
    }
    case "step": {
      const t = ticket();
      const rest = a.slice(2);
      const patch = { step: a[1] };
      const i = rest.indexOf("--round");
      if (i >= 0) { patch.round = Number(rest[i + 1]); rest.splice(i, 2); }
      if (rest.length) patch.note = rest.join(" ");
      setStatus(paths(root, cfg, t), patch);
      return 0;
    }
    case "block": {
      const t = ticket();
      if (!a[1]) throw new UserError("block needs a note: the question only a person can answer");
      setStatus(paths(root, cfg, t), { step: "blocked", note: a.slice(1).join(" ") });
      return 0;
    }
    case "run": {
      // A small bug fix has no ticket: `fix-<slug>` keeps its stamp in the job folder.
      const t = /^fix-[\w.-]+$/.test(a[0] ?? "") ? a[0] : ticket();
      if (a[1] !== "round") throw new UserError("usage: gate run <ticket or fix-slug> round");
      return runGate(root, cfg, paths(root, cfg, t).job, "round") ? 0 : 1;
    }
    case "stamped": {
      // Has the round gate passed for this exact tree? The lighter landing gate for a fix.
      if (!/^fix-[\w.-]+$/.test(a[0] ?? "")) throw new UserError("usage: gate stamped fix-<slug>");
      const tree = treeOf(root);
      const good = tree.ok && stampOk(paths(root, cfg, a[0]).job, "round", tree.out);
      console.log(good ? `${a[0]}: round gate passed for this tree` : `${a[0]}: the round gate hasn't passed for this tree`);
      return good ? 0 : 1;
    }
    case "risky": {
      // Which files in a range are on High-risk paths? Exit 1 if any: a fix there needs a ticket.
      if (!a[0]) throw new UserError("usage: gate risky <base> [head]");
      const diff = git(root, ["diff", "--name-only", `${a[0]}...${a[1] ?? "HEAD"}`]);
      if (!diff.ok) throw new UserError(`can't diff ${a[0]}: ${diff.err}`);
      const hits = diff.out.split("\n").filter((f) => f && cfg.highRiskPaths.some((h) => {
        if (h.endsWith("*")) return f.startsWith(h.slice(0, -1));
        return f === h || f.startsWith(h.endsWith("/") ? h : `${h}/`);
      }));
      hits.forEach((f) => console.log(f));
      return hits.length ? 1 : 0;
    }
    case "record": {
      const t = ticket();
      if (!a[1] || !existsSync(a[1])) throw new UserError("usage: gate record <ticket> <results file>");
      const tree = treeOf(root);
      if (!tree.ok) throw new UserError("can't read the current tree");
      write(join(paths(root, cfg, t).job, "results", `${tree.out}.md`), normalizeEol(readFileSync(a[1], "utf8")));
      console.log(`${t}: results recorded for tree ${tree.out.slice(0, 8)}`);
      return 0;
    }
    case "check": { const t = ticket(); return report(checkTicket(root, cfg, t), `${t}: proven for this tree`); }
    case "carry": {
      const t = ticket();
      const from = a[a.indexOf("--from") + 1];
      if (!a.includes("--from") || !from) throw new UserError("usage: gate carry <ticket> --from <worktree>");
      const src = join(resolve(from), cfg.jobDir, t);
      if (!existsSync(src)) throw new UserError(`nothing to carry at ${src}`);
      cpSync(src, join(root, cfg.jobDir, t), { recursive: true });
      console.log(`${t}: job state carried from ${from}`);
      return 0;
    }
    case "human": {
      const t = ticket();
      const [id, result, ...note] = a.slice(1);
      if (!id || !["pass", "fail"].includes(result)) throw new UserError("usage: gate human <ticket> <id> pass|fail [note]");
      const p = paths(root, cfg, t);
      write(p.human, { ...readJson(p.human, {}), [id]: { result, note: note.join(" "), at: new Date().toISOString() } });
      console.log(`${t} ${id}: ${result}`);
      return 0;
    }
    case "release": {
      const open = releaseProblems(root, cfg);
      if (open.length) {
        console.log("Human checks still open. Run each one, then record it:");
        for (const o of open) console.log(`  ! node .claude/hooks/gate.mjs human ${o.t} ${o.id} pass   # ${o.failed ? "FAILED: " : ""}${o.title}`);
        return 1;
      }
      const job = join(root, cfg.jobDir, "release");
      if (a.includes("--run")) return runGate(root, cfg, job, "release") ? 0 : 1;
      const tree = treeOf(root);
      if (tree.ok && stampOk(job, "release", tree.out)) { console.log("release gate passed for this tree"); return 0; }
      console.log("No human checks open. The release gate hasn't passed for this tree: run `gate release --run`.");
      return 1;
    }
    case "park": {
      // The router can't edit or use git, and parking is its call to relay: this commits itself.
      const t = ticket();
      const p = paths(root, cfg, t);
      setBoard(root, cfg, t, "parked");
      const rel = [p.rd, p.board].filter(existsSync).map((f) => relative(root, f));
      git(root, ["add", "--", ...rel]);
      const c = git(root, ["commit", "-m", `Park ${t}`, "--", ...rel], 15000);
      if (!c.ok) throw new UserError(`couldn't commit: ${c.err || c.out}`);
      console.log(`${t}: parked`);
      return 0;
    }
    case "rule": {
      // A rule the owner said yes to. Appends to REVIEW.md and commits only that file.
      const line = a.join(" ").trim();
      if (!line) throw new UserError("usage: gate rule <one line the owner approved>");
      const f = join(root, "REVIEW.md");
      if (!existsSync(f)) throw new UserError("no REVIEW.md in this checkout");
      const text = readFileSync(f, "utf8");
      if (!/^## Rules learned over time/m.test(text)) throw new UserError('REVIEW.md has no "## Rules learned over time" section');
      writeFileSync(f, `${text.replace(/\s*$/, "")}\n- ${line}\n`);
      git(root, ["add", "--", "REVIEW.md"]);
      const c = git(root, ["commit", "-m", "docs(review): add a learned rule", "--", "REVIEW.md"], 15000);
      if (!c.ok) throw new UserError(`couldn't commit: ${c.err || c.out}`);
      console.log("rule added");
      return 0;
    }
    case "job": {
      if (!a[0]) throw new UserError("usage: gate job <key> field=value ...");
      const f = join(root, cfg.jobDir, "router-state.json");
      const all = readJson(f, {});
      const patch = Object.fromEntries(a.slice(1).map((kv) => { const i = kv.indexOf("="); return [kv.slice(0, i), kv.slice(i + 1)]; }));
      all[a[0]] = { ...all[a[0]], ...patch, updated: new Date().toISOString() };
      write(f, all);
      return 0;
    }
    case "jobs": console.log(JSON.stringify(readJson(join(root, cfg.jobDir, "router-state.json"), {}), null, 2)); return 0;
    case "status": {
      for (const t of listTickets(root, cfg)) {
        const p = paths(root, cfg, t);
        if (!existsSync(p.rd)) { console.log(`${t}  (no rd.md)`); continue; }
        const rd = readText(p.rd);
        console.log(`${t}  ${field(rd, "Status") ?? "?"}  ${riskLevel(rd) ?? "?"}  ${readStatus(p).step ?? ""}`.trimEnd());
      }
      return 0;
    }
    default:
      throw new UserError(`unknown command ${cmd ?? "(none)"}: see the top of .claude/hooks/gate.mjs`);
  }
}

try {
  const code = main(process.argv.slice(2));
  if (code !== null) process.exitCode = code;
} catch (e) {
  if (!(e instanceof UserError)) throw e;
  console.error(e.message);
  process.exitCode = 1;
}
