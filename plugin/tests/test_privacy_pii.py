"""Personal data is masked at the memory exits, by the workspace's declared classes.

Every identifier below is a published test value with a valid checksum, not a
real person's data:

* CPF 529.982.247-25 and CNPJ 11.222.333/0001-81 (standard Brazilian examples)
* card 4111 1111 1111 1111 (the Visa test number)
* IBAN GB82 WEST 1234 5698 7654 32 (the ECBS example IBAN)

E-mail addresses use the reserved example.com domain.
"""

from __future__ import annotations

import json
import time
from pathlib import Path

import pytest

from tentaqles import privacy
from tentaqles.memory import jev_gate
from tentaqles.memory.consolidator import MemoryConsolidator
from tentaqles.memory.ingest import ingest_markdown_dirs
from tentaqles.memory.store import MemoryStore
from tentaqles.memory.vault_export import export_workspace
from tentaqles.knowledge import KnowledgeConfig

CPF = "529.982.247-25"
CNPJ = "11.222.333/0001-81"
CARD = "4111 1111 1111 1111"
IBAN = "GB82 WEST 1234 5698 7654 32"
EMAIL = "ana.silva@example.com"
GH_TOKEN = "ghp_" + "A1b2C3d4E5" * 3 + "F6g7H8"  # 36 alphanumerics after ghp_
STRICTEST = ["checksummed", "pii", "financial"]


@pytest.fixture(autouse=True)
def _reset():
    privacy.set_default_classes(["checksummed"])
    yield
    privacy.set_default_classes(["checksummed"])


@pytest.fixture(autouse=True)
def _no_embeddings(monkeypatch):
    monkeypatch.setattr(MemoryStore, "_embed", lambda self, text: None)


def _workspace(root: Path, data_block: str | None) -> Path:
    root.mkdir(parents=True, exist_ok=True)
    body = "schema: tentaqles-client-v2\nclient: acme\n"
    if data_block is not None:
        body += data_block
    (root / ".tentaqles.yaml").write_text(body, encoding="utf-8")
    return root


# --- detectors -------------------------------------------------------------


def test_checksummed_by_default_leaves_email():
    out, events = privacy.redact_text(f"cpf {CPF} mail ana@example.com card {CARD}")
    assert "[REDACTED:cpf]" in out and "[REDACTED:card]" in out and "ana@example.com" in out
    assert "529" not in out and "4111" not in out
    assert "cpf" in events and "card" in events


def test_cnpj_and_iban_are_checksummed():
    out, events = privacy.redact_text(f"cnpj {CNPJ} iban {IBAN}")
    assert out == "cnpj [REDACTED:cnpj] iban [REDACTED:iban]"
    assert events == ["cnpj", "iban"]


def test_invalid_checksums_are_not_masked():
    text = "id 52998224726 order 4111111111111112 cnpj 11.222.333/0001-82 iban GB83 WEST 1234 5698 7654 32"
    out, events = privacy.redact_text(text, classes=["checksummed", "financial"])
    assert out == text and events == []


def test_failed_checksum_does_not_hide_a_card_after_it():
    out, events = privacy.redact_text(f"ref 5512 {CARD}")
    assert out == "ref 5512 [REDACTED:card]" and events == ["card"]


def test_ordinary_numbers_are_not_masked():
    text = "build 20261021 took 1234567 ms; 3.14159; port 8080; version 1.22.3"
    out, events = privacy.redact_text(text, classes=["all"])
    assert out == text and events == []


def test_pii_class_masks_email_and_phone():
    out, events = privacy.redact_text(f"{EMAIL} +55 11 91234-5678", classes=["pii"])
    assert out.count("[REDACTED:") == 2
    assert "ana.silva" not in out and "91234" not in out
    assert sorted(events) == ["email", "phone"]


def test_allow_marker_and_secrets_always():
    text = f"row {CPF} tq:allow-pii\nkey {GH_TOKEN}\nrow {CPF}"
    out, events = privacy.redact_text(text)
    first, second, third = out.split("\n")
    assert CPF in first
    assert "[REDACTED:github_pat]" in second and "ghp_" not in out
    assert third == "row [REDACTED:cpf]"
    assert events == ["github_pat", "cpf"]


def test_secrets_win_over_pii_on_overlap():
    out, events = privacy.redact_text("svc deploy@acme-prod.iam.gserviceaccount.com", classes=["pii"])
    assert "[REDACTED:gcp_service_account]" in out
    assert events == ["gcp_service_account"]


def test_audit_log_context_never_holds_pii(tmp_path: Path):
    log = tmp_path / "audit.jsonl"
    out, events = privacy.redact_text(f"{CPF} {EMAIL} key {GH_TOKEN} {CPF}", classes=["pii"], audit_log_path=log)
    raw = log.read_text(encoding="utf-8")
    assert CPF not in raw and EMAIL not in raw and "ghp_" not in raw
    records = [json.loads(line) for line in raw.splitlines()]
    assert [r["pattern"] for r in records] == events
    assert "[REDACTED:github_pat]" in records[events.index("github_pat")]["context"]


# --- classes ----------------------------------------------------------------


def test_exit_classes_from_manifest():
    assert privacy.exit_classes(None) == ["checksummed"]
    assert privacy.exit_classes({}) == ["checksummed"]
    assert privacy.exit_classes({"data": {"classes": []}}) == ["checksummed"]
    assert privacy.exit_classes({"data": {"classes": ["pii", "financial"]}}) == STRICTEST
    assert privacy.exit_classes({"data": {"classes": ["children"]}}) == ["checksummed", "pii"]
    assert privacy.exit_classes({"data": {"classes": ["sensitive", "secrets"]}}) == ["checksummed", "pii"]
    assert privacy.exit_classes({"data": {"classes": ["all"]}}) == STRICTEST


@pytest.mark.parametrize("data", ["pii", {"classes": "pii"}, {"classes": ["pii", "biometric"]}, {"classes": [3]}])
def test_exit_classes_unreadable_data_block_is_strictest(data):
    assert privacy.exit_classes({"data": data}) == STRICTEST


@pytest.mark.parametrize("bad", [[], ["biometric"], ["pii", "nope"]])
def test_unknown_or_empty_class_list_raises(bad):
    with pytest.raises(ValueError):
        privacy.redact_text("x", classes=bad)
    with pytest.raises(ValueError):
        privacy.set_default_classes(bad)


def test_all_and_secrets_classes():
    text = f"{CPF} {EMAIL} +55 11 91234-5678 {CARD}"
    out, events = privacy.redact_text(text, classes=["all"])
    assert sorted(events) == ["card", "cpf", "email", "phone"]
    out, events = privacy.redact_text(text, classes=["secrets"])
    assert sorted(events) == ["card", "cpf"] and EMAIL in out


def test_redact_str_fails_closed():
    assert privacy.redact_str(None) is None
    assert privacy.redact_str("") == ""
    # An unknown class falls back to the strictest masking, never raw text.
    assert privacy.redact_str(f"mail {EMAIL}", classes=["biometric"]) == "mail [REDACTED:email]"
    assert privacy.redact_str(f"mail {EMAIL}", classes=[]) == "mail [REDACTED:email]"
    # Anything else that goes wrong yields a redaction marker, not the input.
    assert privacy.redact_str(object()) == "[REDACTED:error]"


def test_workspace_classes(tmp_path: Path):
    assert privacy.workspace_classes(_workspace(tmp_path / "a", None)) == ["checksummed"]
    assert privacy.workspace_classes(_workspace(tmp_path / "b", "data:\n  classes: [pii]\n")) == ["checksummed", "pii"]
    broken = tmp_path / "c"
    broken.mkdir()
    (broken / ".tentaqles.yaml").write_text("schema: [unclosed\n", encoding="utf-8")
    assert privacy.workspace_classes(broken) == STRICTEST


def test_redaction_of_a_megabyte_summary_is_fast():
    line = f"Talked to {EMAIL} about order 12345 and CPF {CPF}; call +55 11 91234-5678 later.\n"
    text = line * (1_000_000 // len(line) + 1)
    start = time.perf_counter()
    out = privacy.redact_str(text, classes=["all"])
    elapsed = time.perf_counter() - start
    assert EMAIL not in out and CPF not in out
    assert elapsed < 1.0, f"took {elapsed:.2f}s"


# --- memory.db ---------------------------------------------------------------


def test_store_masks_by_workspace_classes(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, "data:\n  classes: [pii]\n"))
    try:
        s.start_session(tags=[f"client {EMAIL}"], metadata={"who": EMAIL, "n": 1})
        s.end_session(summary=f"talked to {EMAIL} about CPF {CPF}")
        summary = s.get_last_session()["summary"]
        assert "[REDACTED:email]" in summary and "[REDACTED:cpf]" in summary
        assert EMAIL not in summary and CPF not in summary
        tags, meta = s._conn.execute("SELECT tags, metadata FROM sessions").fetchone()
        assert EMAIL not in tags and EMAIL not in meta and "[REDACTED:email]" in meta
        assert json.loads(meta)["n"] == 1
        text = " ".join(str(r) for r in s.search_memory("ana")) + s.get_context_summary()
        assert "ana.silva" not in text and "529.982" not in text
    finally:
        s.close()


def test_store_masks_semantic_facts(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, "data:\n  classes: [pii]\n"))
    try:
        s.record_semantic_fact(fact=f"billing contact is {EMAIL}", source_sessions=["s1"],
                               tags=[EMAIL], query_hint=f"who is {EMAIL}")
        fact, tags, hint = s._conn.execute("SELECT fact, tags, query_hint FROM semantic_memories").fetchone()
        assert fact == "billing contact is [REDACTED:email]"
        assert EMAIL not in tags and EMAIL not in hint
    finally:
        s.close()


def test_store_without_data_block_keeps_email_but_masks_cpf(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, None))
    try:
        s.end_session(summary=f"{EMAIL} {CPF}")
        assert s.get_last_session()["summary"] == f"{EMAIL} [REDACTED:cpf]"
    finally:
        s.close()


def test_store_with_broken_manifest_uses_strictest(tmp_path: Path):
    tmp_path.mkdir(exist_ok=True)
    (tmp_path / ".tentaqles.yaml").write_text("schema: [unclosed\n", encoding="utf-8")
    s = MemoryStore(tmp_path)
    try:
        assert s.classes == STRICTEST
        s.end_session(summary=f"mail {EMAIL}")
        assert s.get_last_session()["summary"] == "mail [REDACTED:email]"
    finally:
        s.close()


def test_stores_do_not_share_classes(tmp_path: Path):
    strict = MemoryStore(_workspace(tmp_path / "pii", "data:\n  classes: [pii]\n"))
    loose = MemoryStore(_workspace(tmp_path / "plain", None))
    try:
        strict.end_session(summary=f"mail {EMAIL}")
        loose.end_session(summary=f"mail {EMAIL}")
        assert strict.get_last_session()["summary"] == "mail [REDACTED:email]"
        assert loose.get_last_session()["summary"] == f"mail {EMAIL}"
        assert privacy.redact_str(f"mail {EMAIL}") == f"mail {EMAIL}"  # module default untouched
    finally:
        strict.close()
        loose.close()


# --- consolidator --------------------------------------------------------------


def _legacy_session(store: MemoryStore, sid: str, summary: str) -> None:
    """A row written before masking existed: raw text already in memory.db."""
    store._conn.execute(
        "INSERT INTO sessions (id, started_at, ended_at, summary) VALUES (?, ?, ?, ?)",
        (sid, "2026-10-01T00:00:00+00:00", "2026-10-01T01:00:00+00:00", summary),
    )
    store._conn.commit()


def test_consolidator_prompt_and_facts_are_masked(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, "data:\n  classes: [pii]\n"))
    prompts: list[str] = []

    def llm(prompt: str) -> str:
        prompts.append(prompt)
        return f"Billing questions go to {EMAIL} || who handles billing"

    try:
        _legacy_session(s, "s1", f"called {EMAIL}, CPF {CPF}")
        ids = MemoryConsolidator(s, llm_fn=llm).run_compaction(["s1"])
        assert ids and prompts
        assert EMAIL not in prompts[0] and CPF not in prompts[0] and "[REDACTED:email]" in prompts[0]
        fact = s._conn.execute("SELECT fact FROM semantic_memories").fetchone()[0]
        assert EMAIL not in fact and "[REDACTED:email]" in fact
    finally:
        s.close()


def test_procedural_memories_are_masked(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, "data:\n  classes: [pii]\n"))
    try:
        for i in range(3):
            s._conn.execute(
                "INSERT INTO decisions (id, session_id, created_at, node_ids, chosen, rejected, rationale, "
                "confidence, tags) VALUES (?, 'untracked', ?, '[]', ?, '[]', 'r', 'medium', ?)",
                (f"d{i}", f"2026-10-0{i + 1}T00:00:00+00:00", f"{EMAIL} owns invoicing {i}", json.dumps([EMAIL])),
            )
        s._conn.commit()
        found = MemoryConsolidator(s).detect_procedural_patterns()
        assert found
        name, steps, trigger, tags = s._conn.execute(
            "SELECT workflow_name, steps, trigger_pattern, tags FROM procedural_memories").fetchone()
        for value in (name, steps, trigger, tags):
            assert EMAIL not in value and "ana.silva" not in value
        assert "[REDACTED:email]" in name
    finally:
        s.close()


# --- ingest, vault, Jev --------------------------------------------------------------


def test_ingest_masks_reference_documents(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path / "ws", "data:\n  classes: [pii]\n"))
    notes = tmp_path / "notes"
    notes.mkdir()
    (notes / "a.md").write_text(f"---\ntitle: Notes on {EMAIL}\n---\nCPF {CPF} and {EMAIL}\n", encoding="utf-8")
    try:
        assert ingest_markdown_dirs(s, [notes])["added"] == 1
        title, text = s._conn.execute("SELECT title, text FROM reference_docs").fetchone()
        assert title == "Notes on [REDACTED:email]"
        assert text == "CPF [REDACTED:cpf] and [REDACTED:email]"
    finally:
        s.close()


def test_vault_export_masks_legacy_rows(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path / "ws", "data:\n  classes: [pii]\n"))
    try:
        _legacy_session(s, "s1", f"called {EMAIL} about card {CARD}")
        manifest = {"client": "acme", "data": {"classes": ["pii"]}}
        written = export_workspace(s, manifest, KnowledgeConfig(vault_dir=tmp_path / "vault"))
        assert written
        blob = "\n".join(p.read_text(encoding="utf-8") for p in written)
        assert EMAIL not in blob and "4111" not in blob
        assert "[REDACTED:email]" in blob and "[REDACTED:card]" in blob
    finally:
        s.close()


def test_jev_capture_payload_masks_by_workspace(tmp_path: Path):
    ws = _workspace(tmp_path, "data:\n  classes: [pii]\n")
    payload = jev_gate.capture_payload(str(ws), "Bash", f"mail {EMAIL} cpf {CPF}", 0)
    assert payload["text"] == "mail [REDACTED:email] cpf [REDACTED:cpf]"
    explicit = jev_gate.capture_payload(str(ws), "Bash", f"mail {EMAIL}", 0, classes=["checksummed"])
    assert explicit["text"] == f"mail {EMAIL}"


def test_jev_recall_payload_masks_by_store_classes(tmp_path: Path):
    s = MemoryStore(_workspace(tmp_path, "data:\n  classes: [pii]\n"))
    try:
        _legacy_session(s, "s1", f"last time {EMAIL}")
        for i in range(7):
            s._conn.execute(
                "INSERT INTO semantic_memories (id, created_at, fact, category, strength, recall_count, "
                "source_sessions, tags) VALUES (?, ?, ?, 'general', 1.0, 0, '[]', '[]')",
                (f"f{i}", "2026-10-01T00:00:00+00:00", f"fact {i} about {EMAIL}"),
            )
        s._conn.commit()
        payload = jev_gate.recall_payload(str(tmp_path), s)
        blob = json.dumps(payload)
        assert EMAIL not in blob and "[REDACTED:email]" in blob
    finally:
        s.close()


def test_meta_memory_masks_summary_by_workspace(tmp_path: Path):
    from tentaqles.memory.meta import MetaMemory

    ws = _workspace(tmp_path / "ws", "data:\n  classes: [pii]\n")
    meta = MetaMemory(db_path=tmp_path / "meta.db")
    try:
        meta.update_workspace("acme", "Acme", str(ws), f"mailed {EMAIL} re CPF {CPF}", [f"notes/{EMAIL}.md"])
        summary, nodes = meta._conn.execute("SELECT last_summary, active_nodes FROM workspace_status").fetchone()
        assert summary == "mailed [REDACTED:email] re CPF [REDACTED:cpf]"
        assert EMAIL not in nodes
    finally:
        meta.close()
