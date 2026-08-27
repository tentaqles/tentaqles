from __future__ import annotations

from pathlib import Path

import pytest

from sidecar.services.safe_io import BackupManager, FileTransaction, atomic_write

# ---------------------------------------------------------------------------
# atomic_write tests
# ---------------------------------------------------------------------------


def test_atomic_write_creates_file(tmp_path: Path) -> None:
    target = tmp_path / "hello.txt"
    atomic_write(target, "world")
    assert target.read_text(encoding="utf-8") == "world"


def test_atomic_write_overwrites_existing(tmp_path: Path) -> None:
    target = tmp_path / "file.txt"
    target.write_text("old", encoding="utf-8")
    atomic_write(target, "new")
    assert target.read_text(encoding="utf-8") == "new"


def test_atomic_write_creates_parent_dirs(tmp_path: Path) -> None:
    target = tmp_path / "a" / "b" / "c" / "file.txt"
    atomic_write(target, "deep")
    assert target.read_text(encoding="utf-8") == "deep"


def test_atomic_write_no_temp_file_left_on_success(tmp_path: Path) -> None:
    target = tmp_path / "file.txt"
    atomic_write(target, "content")
    files = list(tmp_path.iterdir())
    assert files == [target], f"Unexpected files in tmp_path: {files}"


# ---------------------------------------------------------------------------
# BackupManager tests
# ---------------------------------------------------------------------------


def test_backup_create_and_restore(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    mgr = BackupManager(backup_dir)

    file_a = tmp_path / "a.txt"
    file_b = tmp_path / "b.txt"
    file_a.write_text("original_a", encoding="utf-8")
    file_b.write_text("original_b", encoding="utf-8")

    backup_id = mgr.create("test", [file_a, file_b])

    # Modify originals
    file_a.write_text("modified_a", encoding="utf-8")
    file_b.write_text("modified_b", encoding="utf-8")

    mgr.restore(backup_id)

    assert file_a.read_text(encoding="utf-8") == "original_a"
    assert file_b.read_text(encoding="utf-8") == "original_b"


def test_backup_handles_missing_files(tmp_path: Path) -> None:
    """Backup a mix of existing and missing files.  Restore should delete the 'absent' file."""
    backup_dir = tmp_path / "backups"
    mgr = BackupManager(backup_dir)

    existing = tmp_path / "exists.txt"
    missing = tmp_path / "missing.txt"
    existing.write_text("data", encoding="utf-8")
    # missing does not exist at backup time

    backup_id = mgr.create("mixed", [existing, missing])

    # Now create the file that was absent at backup time
    missing.write_text("should be gone after restore", encoding="utf-8")

    mgr.restore(backup_id)

    assert existing.read_text(encoding="utf-8") == "data"
    assert not missing.exists(), "Absent file should have been deleted on restore"


def test_backup_list_newest_first(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    mgr = BackupManager(backup_dir)

    f = tmp_path / "f.txt"
    f.write_text("x", encoding="utf-8")

    id1 = mgr.create("first", [f])
    id2 = mgr.create("second", [f])

    backups = mgr.list_backups()
    assert len(backups) == 2
    # Newest (id2) should appear first
    assert backups[0].backup_id == id2
    assert backups[1].backup_id == id1
    assert backups[0].timestamp >= backups[1].timestamp


def test_backup_prune_keeps_newest(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    mgr = BackupManager(backup_dir)

    f = tmp_path / "f.txt"
    f.write_text("x", encoding="utf-8")

    ids = [mgr.create(f"backup_{i}", [f]) for i in range(5)]

    mgr.prune(keep=2)

    remaining = mgr.list_backups()
    assert len(remaining) == 2
    # The two newest should survive
    remaining_ids = {b.backup_id for b in remaining}
    assert ids[-1] in remaining_ids
    assert ids[-2] in remaining_ids


# ---------------------------------------------------------------------------
# FileTransaction tests
# ---------------------------------------------------------------------------


def test_transaction_commits_on_success(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    target = tmp_path / "file.txt"
    target.write_text("before", encoding="utf-8")

    with FileTransaction("commit", backup_dir=backup_dir) as tx:
        tx.write(target, "after")

    assert target.read_text(encoding="utf-8") == "after"


def test_transaction_rollback_on_exception(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    target = tmp_path / "file.txt"
    target.write_text("original", encoding="utf-8")

    with pytest.raises(ValueError):
        with FileTransaction("rollback", backup_dir=backup_dir) as tx:
            tx.write(target, "changed")
            raise ValueError("oops")

    # File must be unchanged — no writes were executed
    assert target.read_text(encoding="utf-8") == "original"


def test_transaction_delete_on_success(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    target = tmp_path / "to_delete.txt"
    target.write_text("goodbye", encoding="utf-8")

    with FileTransaction("delete", backup_dir=backup_dir) as tx:
        tx.delete(target)

    assert not target.exists()


def test_transaction_delete_rollback_on_exception(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    target = tmp_path / "keep_me.txt"
    target.write_text("stay", encoding="utf-8")

    with pytest.raises(RuntimeError):
        with FileTransaction("delete_rollback", backup_dir=backup_dir) as tx:
            tx.delete(target)
            raise RuntimeError("abort")

    # File must still exist — no deletes were executed
    assert target.exists()
    assert target.read_text(encoding="utf-8") == "stay"


def test_transaction_multi_file(tmp_path: Path) -> None:
    backup_dir = tmp_path / "backups"
    file_a = tmp_path / "a.txt"
    file_b = tmp_path / "b.txt"
    file_a.write_text("a_old", encoding="utf-8")
    file_b.write_text("b_old", encoding="utf-8")

    with FileTransaction("multi", backup_dir=backup_dir) as tx:
        tx.write(file_a, "a_new")
        tx.write(file_b, "b_new")

    assert file_a.read_text(encoding="utf-8") == "a_new"
    assert file_b.read_text(encoding="utf-8") == "b_new"
