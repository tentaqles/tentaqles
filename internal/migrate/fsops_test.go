package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	// Cross-check against a mechanism IsLink does not use.
	assertLinkTarget(t, link, target)
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(target)) {
		t.Fatalf("IsLink target = %q, want %q", got, target)
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

// TestMakeLinkRefusesBadTarget: IsLink legitimately returns (true, "") for a
// link whose target it could not read, so an empty target can reach MakeLink by
// accident. Creating a junction to "" (or to a path that does not exist) points
// the user's identity directory at whatever later appears there.
func TestMakeLinkRefusesBadTarget(t *testing.T) {
	base := testutil.TempDir(t)
	for _, c := range []struct{ name, target, want string }{
		{"empty", "", "empty target"},
		{"missing", filepath.Join(base, "nope"), "target"},
		{"a file", filepath.Join(base, "file"), "not a directory"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.name == "a file" {
				if err := os.WriteFile(c.target, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(base, "link-"+c.name)
			err := MakeLink(link, c.target)
			if err == nil {
				t.Fatalf("MakeLink accepted target %q", c.target)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q should mention %q", err, c.want)
			}
			if _, err := os.Lstat(link); err == nil {
				t.Fatal("a link was created anyway")
			}
		})
	}
}

// TestMakeLinkRefusesQuotedPath: the Windows helpers build a cmd.exe command
// line by quoting each path, so a path carrying a quote of its own would break
// out of that quoting.
func TestMakeLinkRefusesQuotedPath(t *testing.T) {
	base := testutil.TempDir(t)
	target := filepath.Join(base, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	err := MakeLink(filepath.Join(base, `we"ird`), target)
	if err == nil || !strings.Contains(err.Error(), "double quote") {
		t.Fatalf("want a refusal naming the double quote, got %v", err)
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
	if !strings.EqualFold(filepath.Clean(tgt), filepath.Clean(target)) {
		t.Fatalf("IsLink target = %q, want %q", tgt, target)
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
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(target)) {
		t.Fatalf("linkTargetViaDir = %q, want %q", got, target)
	}
}

// TestLinkTargetConfusableSiblings: `dir /AL` lists every reparse entry in the
// directory, so a substring match asked for "b" happily returned the target of
// a sibling junction named "ab". That wrong target is what gets written into a
// durable journal, and the reverse then recreates the user's link pointing at
// someone else's directory while reporting success.
func TestLinkTargetConfusableSiblings(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only")
	}
	base := testutil.TempDir(t)
	tgtAB := filepath.Join(base, "tgt-ab")
	tgtB := filepath.Join(base, "tgt-b")
	for _, d := range []string{tgtAB, tgtB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// "ab" is created first, so a first-hit substring match finds it for "b".
	linkAB := filepath.Join(base, "ab")
	linkB := filepath.Join(base, "b")
	if err := mklinkJ(linkAB, tgtAB); err != nil {
		t.Fatal(err)
	}
	if err := mklinkJ(linkB, tgtB); err != nil {
		t.Fatal(err)
	}

	got, err := linkTargetViaDir(linkB)
	if err != nil {
		t.Fatalf("linkTargetViaDir(b): %v", err)
	}
	if strings.EqualFold(filepath.Clean(got), filepath.Clean(tgtAB)) {
		t.Fatalf("linkTargetViaDir(b) returned the sibling ab's target %q", got)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(tgtB)) {
		t.Fatalf("linkTargetViaDir(b) = %q, want %q", got, tgtB)
	}
	// The primary path (the reparse point itself) must agree.
	if _, tgt := IsLink(linkB); !strings.EqualFold(filepath.Clean(tgt), filepath.Clean(tgtB)) {
		t.Fatalf("IsLink(b) = %q, want %q", tgt, tgtB)
	}
	if _, tgt := IsLink(linkAB); !strings.EqualFold(filepath.Clean(tgt), filepath.Clean(tgtAB)) {
		t.Fatalf("IsLink(ab) = %q, want %q", tgt, tgtAB)
	}
	assertLinkTarget(t, linkB, tgtB)
}

// TestPathWithAmpersand: TQ_HOME is user-supplied and Windows account names may
// contain &, which exec.Command does not escape, so cmd.exe used to re-parse it
// as a command separator -- breaking mklink at best and running the rest of the
// path as a command at worst.
func TestPathWithAmpersand(t *testing.T) {
	base := testutil.TempDir(t)
	// Shell metacharacters cmd.exe would otherwise act on, and deliberately no
	// space or tab: exec.Command quotes an argument only when it contains one
	// of those, so a name with a space would be escaped for the wrong reason
	// and the test would pass against the bug.
	odd := filepath.Join(base, "a&b(c)^d")
	target := filepath.Join(odd, "tgt")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "f.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(odd, "ln")

	if err := MakeLink(link, target); err != nil {
		t.Fatalf("MakeLink under %s: %v", odd, err)
	}
	ok, tgt := IsLink(link)
	if !ok {
		t.Fatalf("IsLink(%s) = false", link)
	}
	if !strings.EqualFold(filepath.Clean(tgt), filepath.Clean(target)) {
		t.Fatalf("IsLink target = %q, want %q", tgt, target)
	}
	b, err := os.ReadFile(filepath.Join(link, "f.txt"))
	if err != nil || string(b) != "payload" {
		t.Fatalf("read through link: %q %v", b, err)
	}
	if runtime.GOOS == "windows" {
		got, err := linkTargetViaDir(link)
		if err != nil {
			t.Fatalf("linkTargetViaDir under %s: %v", odd, err)
		}
		if !strings.EqualFold(filepath.Clean(got), filepath.Clean(target)) {
			t.Fatalf("linkTargetViaDir = %q, want %q", got, target)
		}
	}
	if err := RemoveLink(link); err != nil {
		t.Fatalf("RemoveLink under %s: %v", odd, err)
	}
	// MoveDir must work there too -- this is the code path that moves identity
	// directories.
	moved := filepath.Join(odd, "tgt-moved")
	if err := MoveDir(target, moved); err != nil {
		t.Fatalf("MoveDir under %s: %v", odd, err)
	}
	if _, err := os.Stat(filepath.Join(moved, "f.txt")); err != nil {
		t.Fatalf("content lost: %v", err)
	}
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

// TestMoveDirDoesNotBlameCrossVolume: the usual cause of a failed rename on
// Windows is a sharing violation from a process still holding the directory --
// a running Claude Code on ~/.claude, say. Telling that user their volumes
// differ and inviting them to move the identity directory by hand is dangerous
// advice.
func TestMoveDirDoesNotBlameCrossVolume(t *testing.T) {
	base := testutil.TempDir(t)
	from := filepath.Join(base, "held")
	if err := os.MkdirAll(from, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(from, "open.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	err = MoveDir(from, filepath.Join(base, "dest"))
	if err == nil {
		// POSIX renames a directory with open files inside it happily, and so
		// may some Windows configurations. Nothing to assert then.
		t.Skip("rename succeeded despite the open handle")
	}
	if strings.Contains(err.Error(), "different volumes") {
		t.Fatalf("a sharing violation was reported as a cross-volume move: %v", err)
	}
	if !strings.Contains(err.Error(), "sharing violation") {
		t.Fatalf("error should point at what actually happened: %v", err)
	}
}
