package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

func TestMakeLinkIsLinkRemoveLink(t *testing.T) {
	base := testutil.TempDir(t)
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")

	if err := MakeLink(link, target); err != nil {
		t.Fatalf("MakeLink: %v", err)
	}
	ok, got := IsLink(link)
	if !ok {
		t.Fatalf("IsLink(%s) = false, want true", link)
	}
	if got == "" {
		t.Fatalf("IsLink target empty")
	}
	t.Logf("GOOS=%s link target reported as %q", runtime.GOOS, got)

	// The link resolves to the target's contents.
	b, err := os.ReadFile(filepath.Join(link, "a.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("read through link: %q %v", b, err)
	}

	if err := RemoveLink(link); err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("link still present after RemoveLink: %v", err)
	}
	// Target survived.
	if _, err := os.Stat(filepath.Join(target, "a.txt")); err != nil {
		t.Fatalf("target damaged: %v", err)
	}
}

func TestIsLinkOnPlainDirAndMissing(t *testing.T) {
	base := testutil.TempDir(t)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if ok, _ := IsLink(plain); ok {
		t.Fatal("plain dir reported as link")
	}
	if ok, _ := IsLink(filepath.Join(base, "nope")); ok {
		t.Fatal("missing path reported as link")
	}
}

func TestRemoveLinkRefusesNonLink(t *testing.T) {
	base := testutil.TempDir(t)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLink(plain); err == nil {
		t.Fatal("RemoveLink on plain dir: want error")
	}
	if _, err := os.Stat(plain); err != nil {
		t.Fatalf("plain dir removed anyway: %v", err)
	}
}

// TestJunctionWindows creates a real junction with mklink /J and checks that
// detection, target reading, and removal all work.
func TestJunctionWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	base := testutil.TempDir(t)
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "jn")
	if err := mklinkJ(link, target); err != nil {
		t.Fatalf("mklink /J: %v", err)
	}
	lst, lerr := os.Lstat(link)
	if lerr != nil {
		t.Fatalf("lstat: %v", lerr)
	}
	rl, rlErr := os.Readlink(link)
	t.Logf("junction: Lstat mode=%v symlinkbit=%v; os.Readlink=%q err=%v",
		lst.Mode(), lst.Mode()&os.ModeSymlink != 0, rl, rlErr)

	ok, tgt := IsLink(link)
	if !ok {
		t.Fatal("IsLink on junction = false")
	}
	if tgt == "" {
		t.Fatal("IsLink returned empty target for junction")
	}
	if err := RemoveLink(link); err != nil {
		t.Fatalf("RemoveLink junction: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatalf("junction survived removal: %v", err)
	}
}

// TestSymlinkPOSIX exercises the os.Symlink branch on non-Windows.
func TestSymlinkPOSIX(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix-only")
	}
	base := testutil.TempDir(t)
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "sl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	ok, tgt := IsLink(link)
	if !ok || tgt != target {
		t.Fatalf("IsLink = %v, %q; want true, %q", ok, tgt, target)
	}
}

func TestLinkTargetFallbackDirAL(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	base := testutil.TempDir(t)
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "jn2")
	if err := mklinkJ(link, target); err != nil {
		t.Fatalf("mklink /J: %v", err)
	}
	got, err := linkTargetViaDir(link)
	if err != nil {
		t.Fatalf("linkTargetViaDir: %v", err)
	}
	if got == "" {
		t.Fatal("linkTargetViaDir returned empty")
	}
	t.Logf("dir /AL fallback reported target %q", got)
}

func TestMoveDir(t *testing.T) {
	base := testutil.TempDir(t)
	from := filepath.Join(base, "from")
	if err := os.MkdirAll(filepath.Join(from, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(from, "sub", "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(base, "to")
	if err := MoveDir(from, to); err != nil {
		t.Fatalf("MoveDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, "sub", "f")); err != nil {
		t.Fatalf("moved content missing: %v", err)
	}
	if _, err := os.Stat(from); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
}

func TestMoveDirRefusesLink(t *testing.T) {
	base := testutil.TempDir(t)
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := MakeLink(link, target); err != nil {
		t.Fatal(err)
	}
	err := MoveDir(link, filepath.Join(base, "dest"))
	if err == nil {
		t.Fatal("MoveDir on a link: want error")
	}
	if ok, _ := IsLink(link); !ok {
		t.Fatal("link disturbed by refused MoveDir")
	}
}

func TestMoveDirRefusesExistingDest(t *testing.T) {
	base := testutil.TempDir(t)
	from := filepath.Join(base, "from")
	to := filepath.Join(base, "to")
	for _, d := range []string{from, to} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := MoveDir(from, to); err == nil {
		t.Fatal("want error moving onto an existing destination")
	}
}
