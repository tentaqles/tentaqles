"""Text checks of the onboarding skill (ticket 021, ADR-0014, ADR-0015, ADR-0017).

The skill is text, so these tests read it and check what it says. How a model reads it is not
provable here; the owner's U-02 check proves the walk works once. The guard hook is what
enforces, and the skill never changes it.

Each rule is checked inside its own section, so removing the rule fails the test.
"""

import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SKILL = ROOT / "skills" / "onboard" / "SKILL.md"

# Names a skill step must never read (REQ-1). A line may name one only to forbid it.
PROTECTED = [".tentaqles.yaml", "manifest.yaml", ".claude.json", "~/.tentaqles"]
NEGATION = re.compile(r"\b(never|not|don't|do not|cannot)\b", re.IGNORECASE)

STEP1 = ("## 1.", "## 2.")
WALK = ("## 2.", "## 3.")
CLI = ("### a. CLI", "### b. Connection")
CONNECTION = ("### b. Connection", "### c. Credential")
CREDENTIAL = ("### c. Credential", "### d. Other providers")
OTHER = ("### d. Other providers", "## 3.")
TOKENS = ("## 3.", "## 4.")
VERIFY = ("## 4.", "## 5.")
TABLE = ("## 5.", None)


def body():
    return SKILL.read_text(encoding="utf-8")


def lines():
    return body().splitlines()


def section(bounds):
    """The text from the heading bounds[0] up to the heading bounds[1], line wraps joined by one space."""
    src = body()
    start, end = bounds
    i = src.index(start)
    j = len(src) if end is None else src.index(end, i + len(start))
    return " ".join(src[i:j].split())


def code_blocks(src):
    return re.findall(r"^```[^\n]*\n(.*?)^```", src, re.M | re.S)


def frontmatter():
    ls = lines()
    assert ls and ls[0] == "---", "the skill starts with frontmatter"
    end = ls.index("---", 1)
    fields = {}
    for line in ls[1:end]:
        key, sep, value = line.partition(":")
        assert sep and re.fullmatch(r"[a-z-]+", key), "one key: value per line: %r" % line
        fields[key] = value.strip()
    assert fields.get("name") == "onboard", fields
    desc = fields.get("description", "")
    assert desc and desc[0].isalpha(), "a plain description scalar"
    assert ": " not in desc and " #" not in desc, "the description parses as a YAML scalar"
    return fields


def test_onboard_skill_walks_every_step():
    src = body()
    for name in ("tentaqles status --json", "tentaqles install", "tentaqles connect"):
        assert name in src, name
    cred = section(CREDENTIAL)
    positions = [cred.index(m) for m in ("OAuth", "Token", "MCP")]
    assert positions == sorted(positions), "the credential methods run in the order OAuth, token, MCP"
    assert re.search(r"^\s*! \S", src, re.M), "a login is printed with the ! prefix"
    assert "! gh auth login --web" in src
    assert "| Provider | CLI | Credential | Connections | Verified |" in src, "the five table columns"
    desc = frontmatter()["description"]
    for trigger in ("onboard", "connect my tools", "log me in", "is everything connected", "first session", "new client"):
        assert trigger in desc, trigger


def test_onboard_skill_walks_providers_in_order():
    step1 = section(STEP1)
    assert "tentaqles status --json" in step1
    assert "`providers`" in step1 and "`scope`" in step1
    assert "Walk them in that order." in step1, "the providers are walked in the order given"


def test_onboard_skill_walks_one_provider_at_a_time_and_reruns():
    walk = section(WALK)
    assert "one at a time" in walk, "the walk is one provider at a time"
    assert "then the next provider" in walk, "a provider is finished before the next"
    assert "A re-run works the same way" in walk, "a re-run skips what is in place and redoes what is gone"
    assert "Skip what is in place" in walk


def test_onboard_skill_stops_when_not_a_client():
    step1 = section(STEP1)
    assert "neutral" in step1 and "untrusted" in step1
    assert "tentaqles reach" in step1 and "tq hooks verify" in step1
    assert re.search(r"\bstop\b", step1, re.IGNORECASE), "it stops in both cases"


def test_onboard_skill_never_picks_an_installer():
    src = body()
    for pat in (r"\bwinget\b", r"\bbrew\b", r"\bapt(-get)?\b", r"npm install", r"pip install",
                r"\.msi\b", r"\.exe\b", r"\bcurl\b", r"\bwget\b"):
        assert not re.search(pat, src, re.IGNORECASE), pat
    assert "manual, download from" in src
    assert "vendor" in src and "page" in src, "a manual install gives the vendor's page"


def test_onboard_skill_relays_every_install_line_and_checks_after():
    cli = section(CLI)
    assert "Show the developer every" in cli and "then:" in cli, "every install line is relayed, then: too"
    assert "restart Claude Code" in cli and "run this skill again" in cli
    assert "print no login" in cli, "no login is printed before the CLI resolves"
    assert "`cli_resolves`" in cli
    assert "`installed <version>`" in cli and "`already present <version>`" in cli


def test_onboard_skill_manual_and_failed_installs_print_no_login():
    """Each branch that leaves cli_resolves false is its own bullet: no login for it, this run."""
    cli = section(CLI)
    bullets = {
        "manual": re.search(r"`manual, download from <url>`:.*?(?=`install failed|$)", cli),
        "failed": re.search(r"`install failed: <reason>`:.*", cli),
    }
    for name, m in bullets.items():
        assert m, "the %s branch is named" % name
        text = m.group(0)
        assert "cli_resolves" in text, "%s checks cli_resolves again" % name
        assert "no login" in text, "%s prints no login while the CLI does not resolve" % name
        assert "restart Claude Code" in text or "fix the install" in text, name


def test_onboard_skill_handles_no_catalog_server():
    src = body()
    assert "no MCP server in the catalog" in src
    assert "claude.ai" in src and "connector" in src
    conn = section(CONNECTION)
    assert "claude.ai" in conn and "connector" in conn
    assert "done" in conn and "check" in conn, "the developer does the step, then it is checked"


def test_onboard_skill_connector_settles_without_asking_again():
    conn = section(CONNECTION)
    assert "read-only check" in conn and "passes" in conn, "a connector whose check passes is connected"
    cred = section(CREDENTIAL)
    assert "passes" in cred and "skip the login and the token" in cred, "a provider signed in is not asked again"


def test_onboard_skill_asks_no_cli_login_without_a_cli():
    cred = section(CREDENTIAL)
    assert "(no `cli`, or notion or trello)" in cred and "skip" in cred, "no CLI-login question for a provider with no CLI"


def test_onboard_skill_is_short():
    assert len(lines()) <= 150, len(lines())
    frontmatter()


def test_onboard_skill_has_no_old_flow():
    src = body()
    for old in ("tq doctor --json", "tq bundle sync", "tq allow", "tq onboard"):
        assert old not in src, old


def test_onboard_skill_reads_no_protected_path():
    src = body()
    for block in code_blocks(src):
        for name in PROTECTED:
            assert name not in block, "a command block names %s" % name
    for line in lines():
        for name in PROTECTED:
            if name in line:
                assert NEGATION.search(line), "a line names %s without forbidding it: %r" % (name, line)
    step1 = section(STEP1)
    assert "tentaqles status --json" in step1
    assert "Never read" in src


def test_onboard_skill_reads_the_login_from_providers_show():
    walk = section(WALK)
    cred = section(CREDENTIAL)
    assert "tentaqles providers show" in walk and "tentaqles providers show" in cred
    assert "`login` block" in cred and "<login.args>" in cred
    assert "--help" not in body(), "no login is guessed from a CLI's help"


def test_onboard_skill_verifies_before_it_asks():
    cred = section(CREDENTIAL)
    assert "verify first" in cred and "ask only if that fails" in cred
    assert "already signed in" in cred
    assert "print no command" in cred, "a provider with no login block gets no invented command"


def shared_login(provider):
    """The rule the skill states: a provider with cli set (not notion or trello) and no identity env.

    provider is a dict shaped like `tentaqles providers show`: id, cli (or None), identity.env (or None).
    """
    if provider.get("id") in ("notion", "trello"):
        return False
    return provider.get("cli") is not None and not (provider.get("identity") or {}).get("env")


def test_onboard_skill_shared_login_rule_matches_only_a_cli_with_no_env():
    cred = section(CREDENTIAL)
    assert "A provider whose `cli` is set (not notion or trello) and whose `identity` in `tentaqles providers show` has no `env` entries" in cred
    jira = {"id": "jira", "cli": {"command": "acli"}, "identity": {"env": None}}
    gh = {"id": "gh", "cli": {"command": "gh"}, "identity": {"env": {"GH_CONFIG_DIR": "{dir}"}}}
    linear = {"id": "linear", "cli": None, "identity": {"env": None}}
    notion = {"id": "notion", "cli": {"command": "ntn"}, "identity": {"env": None}}
    assert shared_login(jira), "jira is a shared login"
    assert not shared_login(gh), "gh has its own config home"
    assert not shared_login(linear), "linear has no CLI, so its token is per client and no shared-login warning is given"
    assert not shared_login(notion), "notion's CLI is not used for its connector, so no shared-login warning"


def test_onboard_skill_notion_skips_the_cli_step_and_the_shared_login():
    a = section(CLI)
    walk = section(WALK)
    cred = section(CREDENTIAL)
    assert "notion" in a and "skip step a" in walk, "notion skips step a"
    assert "a missing `ntn` is not an error" in walk
    assert "(not notion or trello)" in cred, "notion is not a shared login"
    assert "or notion or trello" in cred, "notion has no CLI-login question"


def test_onboard_skill_bang_only_for_the_one_browser_flow_the_repo_shows():
    allowed = {"! gh auth login --web"}
    printed = set(m.strip() for m in re.findall(r"^\s*(! \S.*?)\s*$", body(), re.M))
    assert printed <= allowed, "a login runs under ! only when the repo or the ticket shows it is a browser flow: %s" % (printed - allowed)
    assert "! acli" not in body() and "! az " not in body(), "acli and az are not shown to be browser flows"
    assert "known browser flow" in body(), "the rule that names the browser flows is stated"


def test_onboard_skill_makes_no_private_config_claim():
    src = body().lower()
    assert "private config" not in src and "stays in the client" not in src


def test_onboard_skill_aws_login_is_not_under_bang():
    src = body()
    cred = section(CREDENTIAL)
    assert "! aws" not in src, "a key prompt never runs in the session"
    assert "`tq login <identity> <id>`" in cred, "a provider with an identity env logs in with tq login"
    assert "`--sso`" in cred and "`--sso-session <name>`" in cred, "an SSO client gets its SSO login"
    assert "known browser flow" in cred


def test_onboard_skill_connector_only_providers_get_the_connector_step():
    walk = section(WALK)
    conn = section(CONNECTION)
    assert "notion and trello" in walk and "connection" in walk
    assert "the provider is notion or trello" in conn, "notion and trello run connect and get the connector step"
    assert "`no MCP server in the catalog`" in conn


def test_onboard_skill_sign_in_is_never_a_docs_page():
    cred = section(CREDENTIAL)
    assert "`docs` page" not in cred and "docs page" not in cred, "no docs page is offered to sign in"
    assert "`notes`" not in cred, "the notes are not relayed: they can send the developer to a manifest edit"
    assert "step b is its sign-in" in cred
    assert "print no command" in cred
    assert "the table reads `missing`" in cred, "without a connector the row reads missing and the walk goes on"


def test_onboard_skill_pasted_token_hint_names_tq_secret_set_only_for_a_tracker():
    tokens = section(TOKENS)
    assert "should not be shared here" in tokens and "do not use" in tokens
    assert "If the provider is a tracker" in tokens
    assert "never to `tq secret set`" in tokens, "a non-tracker is pointed to its own login or connector"


def test_onboard_skill_table_settles_a_connector_that_passed():
    table = section(TABLE)
    assert "passed in this run" in table and "`present`" in table
    assert "Connections" in table and "Credential" in table


def test_onboard_skill_names_the_shared_login():
    cred = section(CREDENTIAL)
    assert "shared" in cred.lower() and "identity" in cred and "`env`" in cred
    assert "not isolated per client" in cred and "the guard cannot check it" in cred
    assert "replaces any other client's login" in cred
    assert "jira" in cred
    assert "confirm that the signed-in account is this client's" in cred, "the account is confirmed even when verified"


def test_onboard_skill_task_provider_keeps_its_cli_step():
    walk = section(WALK)
    assert "A task provider" in walk
    assert "install, connection and login like any other" in walk
    assert "read-only check" in walk, "the task limit is on tasks, the one read-only check only"


def test_onboard_skill_opens_the_connector_page_with_tentaqles_open():
    conn = section(CONNECTION)
    assert "tentaqles open <url>" in conn and "https://claude.ai/settings/connectors" in conn
    assert "print that URL" in conn or "print the URL" in conn
    other = section(OTHER)
    assert "tentaqles open <url>" in other, "the other provider's page opens with tentaqles open"


def test_onboard_skill_names_a_read_only_call_per_connector():
    verify = section(VERIFY)
    for call in ("GitHub", "Atlassian", "Linear", "Asana", "Notion", "Trello", "Slack", "Teams", "Gmail", "Outlook"):
        assert call in verify, call
    for name in ("discord", "zoom", "google-meet"):
        assert name in verify and "not checked" in verify, name
    assert "nothing else" in verify and "Never send, post, create," in verify


def test_onboard_skill_uses_verified_and_the_named_call():
    verify = section(VERIFY)
    assert "`verified`" in verify and "true" in verify
    assert "read-only" in verify
    assert "A task provider gets only its one call" in verify


def test_onboard_skill_prints_no_bang_before_tq_secret_or_login():
    src = body()
    assert not re.search(r"!\s*tq\s+(secret|login)\b", src), "tq secret and tq login are never printed with !"
    assert "`tq secret set`" in src
    tokens = section(TOKENS)
    assert "own terminal" in tokens and "no `!`" in body()
    assert "tq secret set" in body()


def test_onboard_skill_keeps_scope_and_browser():
    src = body()
    assert re.search(r"outside[^\n]*\bscope\b", src), "a provider outside scope is named"
    assert "refuse" in src.lower()
    assert "tentaqles open" in src
    for pat in (r"start http", r"open http", r"\bchrome\b", r"msedge"):
        assert not re.search(pat, src, re.IGNORECASE), pat


def test_onboard_skill_never_handles_a_token():
    src = body()
    low = src.lower()
    assert re.search(r"never ask for[^\n]*token", low), "it never asks for a token"
    assert re.search(r"never[^\n]*repeat", low) and "repeat" in low, "it never repeats a token"
    assert re.search(r"never[^\n]*store", low), "it never stores a token"
    assert "pasted" in low and "not be shared here" in low, "a pasted token is named and not used"
    assert "do not use" in low or "not use it" in low
    assert "tq secret set" in src


def test_onboard_skill_says_other_is_not_isolated():
    other = section(OTHER)
    assert "other_providers" in body()
    assert "not isolated per client" in other
    assert "guard cannot check" in other
    assert "tentaqles open" in other, "the other provider's page opens with tentaqles open"
    assert "not checked" in other
