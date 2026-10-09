"""Structural checks for the engineering-practice skills and every skill's frontmatter."""

from __future__ import annotations

import re
from pathlib import Path

import pytest
import yaml

from tentaqles.privacy import redact_text

PLUGIN_ROOT = Path(__file__).resolve().parent.parent
SKILLS_DIR = PLUGIN_ROOT / "skills"

ENGINEERING_SKILLS = [
    "db-migration",
    "index-advisor",
    "ci-pipeline",
    "auth-security",
    "system-design-review",
    "grill-me",
]

NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
LINK_RE = re.compile(r"\]\(((?:references|scripts|templates)/[^)#\s]+)\)")
SHA_PIN_RE = re.compile(r"^\s*-?\s*uses:\s*[\w.-]+/[\w./-]+@[0-9a-f]{40}(\s+#.*)?$")


def _frontmatter(path: Path) -> tuple[dict, str]:
    text = path.read_text(encoding="utf-8")
    assert text.startswith("---\n"), f"{path} must start with YAML frontmatter"
    end = text.index("\n---\n", 4)
    meta = yaml.safe_load(text[4:end])
    assert isinstance(meta, dict), f"{path}: frontmatter is not a mapping"
    return meta, text[end + 5:]


def _all_skill_dirs() -> list[Path]:
    return sorted(p for p in SKILLS_DIR.iterdir() if (p / "SKILL.md").is_file())


@pytest.mark.parametrize("skill_dir", _all_skill_dirs(), ids=lambda p: p.name)
def test_every_skill_has_name_and_description(skill_dir: Path) -> None:
    # Line-based on purpose: some older skills use plain scalars that Claude
    # Code accepts but strict YAML rejects (": " inside the description).
    text = (skill_dir / "SKILL.md").read_text(encoding="utf-8")
    assert text.startswith("---\n")
    head = text[4:text.index("\n---\n", 4)]
    fields = dict(line.split(":", 1) for line in head.splitlines() if re.match(r"^[a-z_-]+:", line))
    assert fields.get("name", "").strip() == skill_dir.name
    assert fields.get("description", "").strip()


@pytest.mark.parametrize("name", ENGINEERING_SKILLS)
def test_engineering_skill_frontmatter(name: str) -> None:
    path = SKILLS_DIR / name / "SKILL.md"
    assert path.is_file(), f"missing {path}"
    meta, body = _frontmatter(path)
    assert set(meta) == {"name", "description"}
    assert meta["name"] == name
    assert NAME_RE.match(name) and len(name) <= 64
    desc = meta["description"]
    assert 100 <= len(desc) <= 1024, f"description length {len(desc)}"
    assert "Use when" in desc, "description must say when to trigger"
    assert "<" not in desc and ">" not in desc
    assert body.lstrip().startswith("# "), "body starts with a title"
    assert len(body.splitlines()) <= 200, "keep SKILL.md concise; move detail to references/"


@pytest.mark.parametrize("name", ENGINEERING_SKILLS)
def test_engineering_skill_links_resolve(name: str) -> None:
    skill_dir = SKILLS_DIR / name
    body = (skill_dir / "SKILL.md").read_text(encoding="utf-8")
    for rel in LINK_RE.findall(body):
        assert (skill_dir / rel).is_file(), f"{name}: broken link {rel}"
    assert (skill_dir / "references").is_dir(), f"{name}: long material belongs in references/"


@pytest.mark.parametrize("name", ENGINEERING_SKILLS)
def test_engineering_skill_files_hold_no_secret_shaped_values(name: str) -> None:
    for path in (SKILLS_DIR / name).rglob("*"):
        if path.is_file() and path.suffix in {".md", ".yml", ".yaml", ".json", ".py"}:
            _, events = redact_text(path.read_text(encoding="utf-8"))
            assert not events, f"{path} contains a secret-shaped value: {events}"


def test_grill_me_ships_spec_template() -> None:
    template = SKILLS_DIR / "grill-me" / "references" / "spec-template.md"
    text = template.read_text(encoding="utf-8")
    for section in ("Business rules", "Verification", "Open questions", "Rollout and rollback"):
        assert section in text


def test_skills_point_to_tq_features() -> None:
    def read(name: str) -> str:
        return (SKILLS_DIR / name / "SKILL.md").read_text(encoding="utf-8")

    db = read("db-migration")
    assert "tq/migration-edit" in db and "tq/sql-destructive-shell" in db
    assert "tq decide triage" in db
    assert "tq dotenv run" in read("auth-security")
    assert "tq/sql-destructive-shell" in read("index-advisor")


TEMPLATES = sorted((SKILLS_DIR / "ci-pipeline" / "templates").glob("*.yml"))


def test_ci_templates_exist_per_stack() -> None:
    assert {p.stem for p in TEMPLATES} >= {"node", "python", "go"}


@pytest.mark.parametrize("template", TEMPLATES, ids=lambda p: p.name)
def test_ci_template_is_hardened(template: Path) -> None:
    text = template.read_text(encoding="utf-8")
    doc = yaml.safe_load(text)
    # PyYAML parses the bare key `on` as True.
    triggers = doc.get("on", doc.get(True))
    assert triggers, "workflow has triggers"
    assert "pull_request_target" not in text
    assert doc.get("permissions") == {"contents": "read"}, "least-privilege top-level permissions"
    assert "concurrency" in doc
    uses_lines = [line for line in text.splitlines() if re.match(r"^\s*-?\s*uses:", line)]
    assert uses_lines, "template uses actions"
    for line in uses_lines:
        assert SHA_PIN_RE.match(line), f"action not pinned to a commit SHA: {line.strip()}"
    for job in doc["jobs"].values():
        assert "timeout-minutes" in job
        steps = job["steps"]
        checkout = [s for s in steps if str(s.get("uses", "")).startswith("actions/checkout@")]
        assert checkout and checkout[0].get("with", {}).get("persist-credentials") is False
        assert any("ratchet.py compare" in str(s.get("run", "")) for s in steps)
    assert "|| true" not in text, "a crashed tool must not read as zero findings"
