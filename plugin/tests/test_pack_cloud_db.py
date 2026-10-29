# plugin/tests/test_pack_cloud_db.py
"""Spec D, plugin side (§15, §17, §18): the cloud and db pack reviewers, skills, templates,
review checklists and on-ramp notes. Content checks only: no tq, no database, no network."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

from tentaqles.privacy import redact_text

PLUGIN = Path(__file__).resolve().parents[1]
SKILLS = PLUGIN / "skills"
AGENTS = PLUGIN / "agents"
TEMPLATES = PLUGIN / "templates" / "packs"
ONRAMPS = SKILLS / "pack" / "references" / "onramps"

ONRAMP_NAMES = ("build", "feature", "fix", "kickoff", "dod", "ship")
ONRAMP_FILES = ("cloud-iac", "cloud-apply", "db-migrations", "db-rls", "db-drill")
NEW_SKILLS = ("iac-policy", "rls-tests", "restore-drill")
WRITE_TOOLS = {"Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "PowerShell", "Agent"}

SNIPPET = ('_tqe="${CLAUDE_PLUGIN_ROOT:-}"; [ -z "$_tqe" ] && _tqe="$(for _d in "${CLAUDE_CONFIG_DIR:-$HOME/.claude}'
           '/plugins/cache"/*/tentaqles/*/ "$HOME/.claude/plugins/cache"/*/tentaqles/*/; do [ -f "${_d}.claude-plugin/'
           'plugin.json" ] && printf \'%s\\n\' "${_d%/}"; done | awk -F/ \'{print $NF" "$0}\' | sort -s -t. -k1,1nr '
           '-k2,2nr -k3,3nr | head -n1 | cut -d" " -f2-)"; . "$_tqe/scripts/tq_env.sh" 2>/dev/null || true')

CLOUD_RULES = ([f"IAC00{i}" for i in range(1, 8)] + [f"IAM00{i}" for i in range(1, 8)]
               + [f"AZR00{i}" for i in range(1, 5)] + ["GCP001"])
PG_RULES = [f"PG{i:03d}" for i in range(1, 13)]
MS_RULES = [f"MS00{i}" for i in range(1, 9)]
RLS_RULES = [f"RLS{i:03d}" for i in range(1, 11)]
CLOUD_CHECKS = ("cloud/iac-policy", "cloud/iam", "cloud/plan-fresh", "cloud/no-plan-files", "cloud/checkov",
                "cloud/trivy", "cloud/conftest", "cloud/tflint", "cloud/bicep-lint", "cloud/cfn-lint")
DB_CHECKS = ("db/migration-lint", "db/migration-rollback", "db/rls", "db/rls-tests", "db/squawk", "db/sqlfluff",
             "db/pgtap", "db/restore-drill")
GUARDS = ("tq/iac-apply-env", "tq/iac-identity-drift", "tq/db-migrate-env", "tq/restore-target", "tq/plan-read",
          "tq/plan-print", "tq/plan-commit", "tq/hub-store-write")
HUMAN_ONLY = ("tq hub answer", "tq pack baseline", "build-gate approve", "tq allow", "--allow-failing", "--no-drill")


def _read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def _flat(text: str) -> str:
    return " ".join(text.split())


def _fm(p: Path) -> tuple[dict, str]:
    text = _read(p)
    assert text.startswith("---\n"), p
    end = text.index("\n---\n", 4)
    return yaml.safe_load(text[4:end]), text[end + 5:]


def _bash_blocks(text: str) -> list[str]:
    return re.findall(r"```bash\n(.*?)```", text, re.S)


def _sections(text: str) -> dict[str, str]:
    out: dict[str, str] = {}
    cur = None
    for line in text.splitlines():
        m = re.match(r"^## (\S+)\s*$", line)
        if m:
            cur = m.group(1)
            out[cur] = ""
        elif cur:
            out[cur] += line + "\n"
    return out


def _owned_files() -> list[Path]:
    files = [AGENTS / "iac-reviewer.md", AGENTS / "db-reviewer.md",
             SKILLS / "db-migration" / "references" / "review-checklist.md"]
    for s in NEW_SKILLS:
        files += [p for p in (SKILLS / s).rglob("*") if p.is_file()]
    files += [p for p in TEMPLATES.rglob("*") if p.is_file()]
    files += [ONRAMPS / f"{n}.md" for n in ONRAMP_FILES]
    return files


# ---------------------------------------------------------------------------
# agents (§15)
# ---------------------------------------------------------------------------

AGENT_CHECKLIST = {
    "iac-reviewer": ("skills/iac-policy/references/checklist.md", "I"),
    "db-reviewer": ("skills/db-migration/references/review-checklist.md", "D"),
}


@pytest.mark.parametrize("name", list(AGENT_CHECKLIST))
def test_pack_reviewers_are_read_only_and_return_the_contract(name):
    fm, body = _fm(AGENTS / f"{name}.md")
    assert fm["name"] == name and re.fullmatch(r"[a-z][a-z0-9-]*-reviewer", name)
    assert 40 <= len(fm["description"]) <= 1024
    assert {t.strip() for t in fm["tools"].split(",")} == {"Read", "Glob", "Grep"}
    assert WRITE_TOOLS <= {t.strip() for t in fm["disallowedTools"].split(",")}
    for token in ("RESULT: PASS | FAIL | BLOCKED", "BUILT-BY-ME:", "CANNOT JUDGE", "file:line"):
        assert token in body, (name, token)
    assert "VERDICT:" not in body
    ref, prefix = AGENT_CHECKLIST[name]
    assert ref in body and f"`{prefix}1`" in body and f"`{prefix}12`" in body


def test_iac_reviewer_reads_summaries_never_raw_plans():
    _, body = _fm(AGENTS / "iac-reviewer.md")
    flat = _flat(body)
    for token in ("tq pack cloud show", "cloud/*", "never a raw plan", "sensitive = true", "stateful",
                  "deletion protection", "tq/plan-read", "iac", "iam", "destructive"):
        assert token in flat, token
    assert "never the secret value" in flat


def test_db_reviewer_covers_locks_rls_drill_and_index_advisor():
    _, body = _fm(AGENTS / "db-reviewer.md")
    flat = _flat(body)
    for token in ("db/migration-lint", "db/rls", "db/rls-tests", "db/restore-drill", "index-advisor",
                  "expand and contract", "deploy order", "tq:contract", "### Rollback", "schema", "migration",
                  "rls", "data_loss", "db_migration", "rls_change", "never quote row data"):
        assert token in flat, token


# ---------------------------------------------------------------------------
# checklists
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("rel,prefix", [("iac-policy/references/checklist.md", "I"),
                                        ("db-migration/references/review-checklist.md", "D")])
def test_checklist_rows_are_stable_and_present_once(rel, prefix):
    text = _read(SKILLS / rel)
    for i in range(1, 13):
        assert text.count(f"| {prefix}{i} |") == 1, (rel, i)
    assert f"| {prefix}13 |" not in text


def test_iac_checklist_points_at_rule_ids():
    text = _read(SKILLS / "iac-policy" / "references" / "checklist.md")
    for rid in ("IAC001", "IAC002", "IAC003", "IAC005", "IAC006", "IAM001", "IAM005", "IAM006", "AZR001", "GCP001"):
        assert rid in text, rid


def test_db_checklist_points_at_rule_ids():
    text = _read(SKILLS / "db-migration" / "references" / "review-checklist.md")
    for rid in ("PG001", "PG002", "PG008", "PG009", "PG011", "MS001", "MS005", "RLS001", "RLS004", "RLS007"):
        assert rid in text, rid


# ---------------------------------------------------------------------------
# skills (§17)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name", NEW_SKILLS)
def test_new_skill_frontmatter_and_bash_blocks(name):
    path = SKILLS / name / "SKILL.md"
    fm, body = _fm(path)
    assert fm["name"] == name
    assert 100 <= len(fm["description"]) <= 1024 and "Use when" in fm["description"]
    assert body.lstrip().startswith("# ")
    assert len(body.splitlines()) <= 200
    blocks = _bash_blocks(body)
    assert blocks, name
    for b in blocks:
        if re.search(r"(^|\s)tq ", b):
            assert b.startswith(SNIPPET), (name, b[:80])
        for line in b.splitlines():
            if re.match(r"^\s*(tq|\$_to tq) ", line):
                assert line.lstrip().startswith("$_to tq "), (name, line)
            assert "git add -A" not in line and "git add ." not in line
    for line in body.splitlines():
        if re.search(r"(?<![\w.])\.env", line) and "tq dotenv" not in line:
            assert re.search(r"\bnever\b", line, re.I), (name, line)
    for rel in re.findall(r"\]\(((?:references|templates)/[^)#\s]+)\)", body):
        assert (SKILLS / name / rel).is_file(), (name, rel)


def _human_only_lines_are_forbidden(text: str, where: str) -> None:
    for line in text.splitlines():
        for cmd in HUMAN_ONLY:
            if cmd in line:
                assert re.search(r"\b(never|not|human|human's|humans|refuse[sd]?|only the human|own terminal)\b",
                                 line, re.I), (where, cmd, line)


@pytest.mark.parametrize("name", NEW_SKILLS)
def test_new_skills_never_run_human_only_commands(name):
    _human_only_lines_are_forbidden(_read(SKILLS / name / "SKILL.md"), name)


def test_iac_policy_skill_names_checks_rules_commands_and_guards():
    text = _read(SKILLS / "iac-policy" / "SKILL.md") + _read(SKILLS / "iac-policy" / "references" / "rules.md")
    flat = _flat(text)
    for token in CLOUD_CHECKS + tuple(CLOUD_RULES):
        assert token in flat, token
    for token in ("tq pack check --pack cloud", "tq pack cloud plan --env", "tq pack cloud show",
                  "tq pack cloud approve", "tq pack cloud prune", "tq pack approvals --pack cloud",
                  "tq-allow = \"IAM002:", "tq/iac-apply-env", "tq/iac-identity-drift", "tq/plan-read",
                  "tq/plan-print", "tq/plan-commit", "never answer", "references/checklist.md", "iac-reviewer",
                  "packs.cloud.tools", "baseline-grew", "baseline_change"):
        assert token in flat, token


def test_rls_tests_skill_uses_the_templates_and_local_only_target():
    body = _read(SKILLS / "rls-tests" / "SKILL.md")
    flat = _flat(body)
    for token in ("templates/packs/db/rls_table.test.sql", "templates/packs/db/tests_tq_helpers.sql",
                  "packs.db.rls.tests", "db/rls-tests", "db/pgtap", "db/rls", "tests_tq", "tester-a", "tester-b",
                  "supabase test db", "pg_prove", "test_db is not local", "--linked", "--db-url", "tq dotenv",
                  "never real data", "rls_<table>.test.sql"):
        assert token in flat, token


def test_restore_drill_skill_covers_every_source_kind_and_the_evidence():
    body = _read(SKILLS / "restore-drill" / "SKILL.md")
    flat = _flat(body)
    for token in ("tq pack db drill", "--keep", "db/restore-drill", "docs/ops/restore-drills.jsonl", "freshness",
                  "file", "supabase-dump", "azure-sql-bacpac", "command", "path_env", "tq/restore-target",
                  "host_kind", "backup_taken_at", "migration_version", "tool_versions", "never row content",
                  "--data-only", "sqlpackage", "pg_restore", "templates/packs/db/restore-drills.jsonl"):
        assert token in flat, token


# ---------------------------------------------------------------------------
# shared skills: the links and rows of §17
# ---------------------------------------------------------------------------

def test_db_migration_links_rule_ids_contract_marker_rollback_and_checklist():
    body = _read(SKILLS / "db-migration" / "SKILL.md")
    flat = _flat(body)
    for rid in PG_RULES + MS_RULES:
        assert rid in flat, rid
    for token in ("-- tq:contract", "### Rollback", "## Migration plan", "references/review-checklist.md",
                  "db/migration-lint", "db/migration-rollback", "-- tq:allow", "db-reviewer"):
        assert token in flat, token
    assert len(body.split("---", 2)[2].splitlines()) <= 200


def test_auth_security_points_rls_rows_to_the_pack():
    flat = _flat(_read(SKILLS / "auth-security" / "SKILL.md"))
    assert "db/rls" in flat and "rls-tests" in flat


def test_ci_pipeline_has_the_optional_pack_step():
    flat = _flat(_read(SKILLS / "ci-pipeline" / "SKILL.md"))
    assert "tq pack check --base origin/<default> --json" in flat
    assert "the plan stays the project's step" in flat


def test_supply_chain_has_the_gitignore_rows():
    body = _read(SKILLS / "supply-chain" / "SKILL.md")
    for row in ("*.tfplan", "*.tfstate*", ".terraform/", "cdk.out/"):
        assert row in body, row


# ---------------------------------------------------------------------------
# templates
# ---------------------------------------------------------------------------

def test_template_files_exist():
    for rel in ("cloud/policy-example.rego", "cloud/policy-example_test.rego", "cloud/packs-cloud.yaml",
                "cloud/gitignore.txt", "db/rls_table.test.sql", "db/tests_tq_helpers.sql",
                "db/restore-drills.jsonl", "db/packs-db.yaml"):
        assert (TEMPLATES / rel).is_file(), rel


def test_restore_drills_template_is_empty():
    assert (TEMPLATES / "db" / "restore-drills.jsonl").read_bytes() == b""


def test_rego_example_is_v1_and_emits_addr_and_rule():
    text = _read(TEMPLATES / "cloud" / "policy-example.rego")
    assert re.search(r"^package tq\.terraform$", text, re.M)
    assert "import rego.v1" in text and "deny contains" in text and " if {" in text
    assert '"addr":' in text and '"rule":' in text and '"msg":' in text
    test = _read(TEMPLATES / "cloud" / "policy-example_test.rego")
    assert "package tq.terraform_test" in test and "import rego.v1" in test
    assert re.search(r"^test_\w+ if \{", test, re.M)


def _manifest_snippet(name: str) -> dict:
    doc = yaml.safe_load(_read(TEMPLATES / name))
    assert set(doc) == {"packs"}
    return doc["packs"]


def test_cloud_manifest_snippet_matches_spec_keys():
    cloud = _manifest_snippet("cloud/packs-cloud.yaml")["cloud"]
    allowed = {"enabled", "tools", "iac", "accounts", "tenants", "conftest", "baseline", "require_plan",
               "default_environment", "environments", "tags", "timeouts"}
    assert set(cloud) <= allowed
    kinds = {"terraform", "tofu", "bicep", "arm", "cdk", "cloudformation", "pulumi"}
    assert all(e["kind"] in kinds for e in cloud["iac"])
    match_keys = {"workspaces", "dirs", "var_files", "stacks", "resource_groups", "subscriptions", "accounts",
                  "profiles", "dotenv"}
    policies = {"auto", "ask", "approve", "deny"}
    envs = cloud["environments"]
    assert envs["prod"]["apply"] == "approve" and envs["prod"]["destroy"] == "deny"
    for env in envs.values():
        assert set(env["match"]) <= match_keys
        for k in ("apply", "destroy"):
            assert env.get(k, "ask") in policies
    assert cloud["default_environment"] in envs


def test_db_manifest_snippet_matches_spec_keys():
    db = _manifest_snippet("db/packs-db.yaml")["db"]
    allowed = {"enabled", "tools", "migrations", "small_tables", "rls", "test_db", "drill", "environments",
               "edition", "sqlfluff_fail", "timeouts"}
    assert set(db) <= allowed
    migrators = {"supabase", "prisma", "drizzle", "golang-migrate", "dbmate", "flyway", "sqitch", "alembic",
                 "efcore", "dacpac", "raw"}
    assert all(m["engine"] in ("postgres", "sqlserver") and m["migrator"] in migrators for m in db["migrations"])
    assert db["test_db"].get("url_env") or db["test_db"].get("start")
    for d in db["drill"]:
        assert d["source"]["kind"] in ("file", "supabase-dump", "azure-sql-bacpac", "command")
    match_keys = {"dotenv", "hosts", "project_refs", "servers", "subscriptions", "accounts", "profiles"}
    for env in db["environments"].values():
        assert set(env["match"]) <= match_keys
        assert env.get("migrate", "ask") in ("auto", "ask", "approve", "deny")
    assert db["environments"]["prod"]["migrate"] == "approve"


def test_gitignore_template_rows():
    lines = {line.strip() for line in _read(TEMPLATES / "cloud" / "gitignore.txt").splitlines()}
    assert {"*.tfplan", "*.tfstate", "*.tfstate.*", ".terraform/", "cdk.out/"} <= lines


def _sql_statements(sql: str) -> list[str]:
    no_comments = re.sub(r"--[^\n]*", "", sql)
    return [s.strip() for s in no_comments.split(";") if s.strip()]


def test_rls_template_plan_matches_its_assertions():
    sql = _read(TEMPLATES / "db" / "rls_table.test.sql")
    m = re.search(r"select plan\((\d+)\)", sql)
    assert m
    asserts = re.findall(r"\b(results_eq|throws_ok|is_empty|isnt_empty|ok|lives_ok)\(", sql)
    assert int(m.group(1)) == len(asserts)
    stmts = [s.lower() for s in _sql_statements(sql.replace("$$", "'"))]
    assert stmts[0] == "begin" and stmts[-1] == "rollback"
    assert "select * from finish()" in stmts
    for token in ("tests_tq.as_user('tester-a')", "tests_tq.as_anon()", "tests_tq.as_service()",
                  "tests_tq.create_user('tester-a')", "'42501'", "relrowsecurity"):
        assert token in sql, token
    assert "{{table}}" in sql


def test_helper_schema_defines_every_helper_and_commits():
    sql = _read(TEMPLATES / "db" / "tests_tq_helpers.sql")
    for fn in ("create_user", "uid", "as_user", "as_anon", "as_service"):
        assert re.search(rf"create or replace function tests_tq\.{fn}\(", sql), fn
    for token in ("create schema if not exists tests_tq", "set local role authenticated", "request.jwt.claims",
                  "request.jwt.claim.sub", "auth.users", "select plan(1)", "commit", "set search_path"):
        assert token in sql, token
    assert "security definer" not in sql.lower()
    assert "rollback" not in sql.lower()


# ---------------------------------------------------------------------------
# on-ramps (§18)
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("name", ONRAMP_FILES)
def test_onramp_file_follows_the_convention(name):
    path = ONRAMPS / f"{name}.md"
    text = _read(path)
    assert re.fullmatch(r"(cloud|db)-[a-z0-9-]+", name)
    pack = name.split("-", 1)[0]
    assert text.startswith(f"# {pack}: ")
    secs = _sections(text)
    assert secs and set(secs) <= set(ONRAMP_NAMES), set(secs)
    if "kickoff" in secs:
        first = secs["kickoff"].strip().splitlines()[0]
        assert re.match(r"^(Only when|When) the stack ADR", first), first
        assert f"packs.{pack}" in secs["kickoff"]
        assert "never edit" in _flat(secs["kickoff"]) or "never the manifest" in _flat(secs["kickoff"])
    _human_only_lines_are_forbidden(text, name)
    for b in _bash_blocks(text):
        assert b.startswith(SNIPPET), name
        for line in b.splitlines():
            if re.match(r"^\s*(tq|\$_to tq) ", line):
                assert line.lstrip().startswith("$_to tq "), (name, line)
    assert "crew:" not in text.lower()


def test_onramps_cover_the_table_of_section_18():
    secs = {n: _sections(_read(ONRAMPS / f"{n}.md")) for n in ONRAMP_FILES}
    cloud_kickoff = _flat(secs["cloud-iac"]["kickoff"])
    for token in ("Terraform", "OpenTofu", "Bicep", "CDK", "CloudFormation", "Pulumi", "prod", "approve",
                  "docs/privacy-manifest-proposal.yaml", ".gitignore"):
        assert token in cloud_kickoff, token
    db_kickoff = _flat(secs["db-migrations"]["kickoff"] + secs["db-rls"]["kickoff"] + secs["db-drill"]["kickoff"])
    for token in ("Postgres", "Supabase", "SQL Server", "restore-drill", "tests_tq", "docs/privacy-manifest-proposal.yaml"):
        assert token in db_kickoff, token
    feature = _flat(secs["cloud-iac"]["feature"] + secs["db-rls"]["feature"])
    assert "the dev plan shows no stateful delete" in feature
    assert "every new exposed table has RLS and a pgTAP file" in feature
    assert "tq pack crew" in _flat(secs["cloud-iac"]["feature"] + secs["db-migrations"]["feature"])
    hotfix = _flat(secs["cloud-apply"]["fix"])
    assert "--hotfix" in hotfix and "never the apply approval" in hotfix
    assert "pgTAP" in _flat(secs["db-rls"]["fix"]) and "fixture plan JSON" in _flat(secs["cloud-iac"]["fix"])
    dod = _flat(secs["cloud-iac"]["dod"] + secs["db-migrations"]["dod"])
    assert "pack:cloud/" in dod and "pack:db/" in dod and "plan summary id" in _flat(secs["cloud-apply"]["dod"])
    ship = _flat(secs["cloud-apply"]["ship"] + secs["db-drill"]["ship"])
    assert "Packs" in ship and "drill age" in ship and "never" in ship


# ---------------------------------------------------------------------------
# hygiene
# ---------------------------------------------------------------------------

@pytest.mark.parametrize("path", _owned_files(), ids=lambda p: p.relative_to(PLUGIN).as_posix())
def test_owned_files_hold_no_secret_shaped_values_or_real_names(path):
    text = _read(path)
    _, events = redact_text(text)
    assert not events, (path, events)
    for bad in ("sources/",):
        assert bad not in text.replace("Nothing comes from `sources/`", ""), path
    for m in re.findall(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b", text):
        if path.suffix in (".yaml", ".sql", ".rego"):
            assert len(set(m.replace("-", ""))) == 1, (path, m)  # zeros or repeated digits only


def test_guard_ids_are_spelled_as_spec_d_defines_them():
    text = " ".join(_read(p) for p in _owned_files())
    for g in re.findall(r"(?<![\w.])tq/[a-z0-9-]+", text):
        assert g in GUARDS + ("tq/iac-apply", "tq/iac-destroy", "tq/cloud-delete", "tq/remote-migration",
                              "tq/migration-edit", "tq/sql-destructive-shell", "tq/mcp-sql-write",
                              "tq/pii-fixture-dump", "tq/pack-config-change", "tq/mcp-apply-migration"), g
    for c in re.findall(r"(?<![\w/.-])(?:cloud|db)/[a-z][a-z0-9-]*(?![\w/])", text):
        assert c in CLOUD_CHECKS + DB_CHECKS + ("db/mssql",), c


def test_rules_reference_is_json_free_of_values():
    # the rules page lists ids, never sample secrets or account ids that look real
    text = _read(SKILLS / "iac-policy" / "references" / "rules.md")
    assert not re.search(r"\b\d{12}\b", text.replace("111111111111", ""))
