package commitmsg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheck(t *testing.T) {
	cases := []struct {
		name, cmd string
		checked   bool
		ok        bool
	}{
		{"conventional", `git commit -m "feat(api): add export endpoint"`, true, true},
		{"conventional no scope", `git commit -m 'fix: handle empty email'`, true, true},
		{"breaking with body", `git commit -m "feat!: drop v1" -m "BREAKING CHANGE: v1 endpoints removed"`, true, true},
		{"breaking without body", `git commit -m "feat!: drop v1"`, true, false},
		{"free text", `git commit -m "updated stuff"`, true, false},
		{"capitalised type", `git commit -m "Feat: x"`, true, false},
		{"unknown type", `git commit -m "feature: x"`, true, false},
		{"chained with add", `git add a.go && git commit -m "refactor(core): split loader"`, true, true},
		{"amend no edit", `git commit --amend --no-edit`, false, true},
		{"fixup", `git commit --fixup HEAD~1`, false, true},
		{"editor", `git commit`, false, true},
		{"not a commit", `git log --oneline`, false, true},
		{"attribution trailer ok", "git commit -m \"docs: readme\" -m \"Co-Authored-By: X <x@y.z>\"", true, true},
	}
	for _, c := range cases {
		r := Check(c.cmd, t.TempDir())
		if r.Checked != c.checked || r.OK != c.ok {
			t.Errorf("%s: got checked=%v ok=%v (%s)", c.name, r.Checked, r.OK, r.Why)
		}
	}
}

func TestCommitMessage_FileMissingAsks(t *testing.T) {
	r := Check(`git commit -F /nonexistent/msg.txt`, t.TempDir())
	if !r.Checked || r.OK {
		t.Fatalf("missing -F file must be checked and not ok: %+v", r)
	}
}

func TestCheck_FileMessage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "msg.txt")
	if err := os.WriteFile(p, []byte("fix(guard): close the gap\n\nwhy here\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Check("git commit -F msg.txt", dir); !r.Checked || !r.OK {
		t.Fatalf("%+v", r)
	}
	if r := Check("git commit --file=msg.txt", dir); !r.Checked || !r.OK {
		t.Fatalf("%+v", r)
	}
}

func TestCheck_QuotingAndHeredoc(t *testing.T) {
	cases := []struct {
		name, cmd string
		checked   bool
		ok        bool
	}{
		{"heredoc conventional", "git commit -m \"$(cat <<'EOF'\nfeat(x): add y\n\nbody; with (parens)\n\nCo-Authored-By: A <a@b.c>\nEOF\n)\"", true, true},
		{"heredoc free text", "git commit -m \"$(cat <<'EOF'\nupdated stuff\nEOF\n)\"", true, false},
		{"semicolon in message", `git commit -m "fix(a): b; c && d"`, true, true},
		{"global options", `git -C repo -c core.quotepath=off commit -m "oops"`, true, false},
		{"stdin file", `git commit -F -`, false, true},
		{"unknown substitution", "git commit -m \"$(make msg)\"", false, true},
		{"combined -am flag", `git commit -am "updated stuff"`, true, false},
		{"combined -am flag ok", `git commit -am "fix(a): b"`, true, true},
	}
	for _, c := range cases {
		r := Check(c.cmd, t.TempDir())
		if r.Checked != c.checked || r.OK != c.ok {
			t.Errorf("%s: got checked=%v ok=%v (%s)", c.name, r.Checked, r.OK, r.Why)
		}
	}
}

func TestCheck_FileBounds(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "adir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if r := Check("git commit -F adir", dir); !r.Checked || r.OK {
		t.Fatalf("directory as -F must be checked and not ok: %+v", r)
	}
	big := append([]byte("fix(a): b\n\n"), make([]byte, 70<<10)...)
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	if r := Check("git commit -F big.txt", dir); !r.Checked || r.OK {
		t.Fatalf("70 KiB file must be checked and not ok: %+v", r)
	}
}

func TestCheck_UnterminatedQuote(t *testing.T) {
	r := Check("cat <<EOF\nit's\nEOF\ngit commit -m \"bad\"", t.TempDir())
	if !r.Checked || r.OK {
		t.Fatalf("%+v", r)
	}
	if r := Check("echo 'open", t.TempDir()); r.Checked {
		t.Fatalf("an unterminated quote without a commit is not a commit check: %+v", r)
	}
}

func TestCheck_EveryCommitJudged(t *testing.T) {
	if r := Check(`git commit -m "feat: a" && git commit -m "bad"`, t.TempDir()); !r.Checked || r.OK {
		t.Fatalf("second commit must fail the check: %+v", r)
	}
	if r := Check(`git commit -m "bad" && git commit -m "feat: a"`, t.TempDir()); !r.Checked || r.OK {
		t.Fatalf("first commit must fail the check: %+v", r)
	}
	if r := Check(`git commit -m "feat: a" && git commit -m "fix: b"`, t.TempDir()); !r.Checked || !r.OK {
		t.Fatalf("%+v", r)
	}
}
