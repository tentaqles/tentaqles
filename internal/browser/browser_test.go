package browser

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
)

func TestValidateURL(t *testing.T) {
	for _, ok := range []string{"https://example.com", "http://example.com/a?b=1&c=2#x", "HTTPS://EXAMPLE.COM", "https://localhost:3000/",
		"https://en.wikipedia.org/wiki/O'Reilly", "https://fonts.googleapis.com/css?family=A|B", "https://x/a^b<c>d`e"} {
		if _, err := ValidateURL(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "-x", "--user-data-dir=x", "file:///etc/passwd", "javascript:alert(1)", "a & calc", "ftp://x/y",
		"https://", "https:example.com", "https://a b", "https://a\"b", "https://a\nb", "data:text/html,x", "chrome://settings", `https://x\y`, "//example.com"} {
		if _, err := ValidateURL(bad); err == nil {
			t.Errorf("%q must be refused", bad)
		}
	}
}

func TestPick(t *testing.T) {
	list := []manifest.Browser{{Name: "chrome", Profile: "P1"}, {Name: "edge", Profile: "P2"}}
	if b, err := Pick(list, ""); err != nil || b.Name != "chrome" {
		t.Fatalf("%+v %v", b, err)
	}
	if b, err := Pick(list, "Edge"); err != nil || b.Name != "edge" {
		t.Fatalf("%+v %v", b, err)
	}
	if _, err := Pick(list[:1], "edge"); err == nil || !strings.Contains(err.Error(), "not one of this client's browsers") {
		t.Fatalf("%v", err)
	}
	if _, err := Pick(nil, ""); err == nil || err.Error() != "no browser is set for this client" {
		t.Fatalf("%v", err)
	}
}

func TestOpen_StartsOnlyWhenEverythingHolds(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "chrome")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevL, prevS := Locate, Start
	t.Cleanup(func() { Locate, Start = prevL, prevS })
	var started [][]string
	Start = func(path string, args []string) error {
		started = append(started, append([]string{path}, args...))
		return nil
	}
	Locate = func(name string) []string {
		if name == "chrome" {
			return []string{filepath.Join(dir, "missing"), exe}
		}
		return []string{filepath.Join(dir, "nothing")}
	}
	list := []manifest.Browser{{Name: "chrome", Profile: "Profile 2"}, {Name: "edge", Profile: "Default"}}
	if _, err := Open(list, "", "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	want := []string{exe, "--profile-directory=Profile 2", "https://example.com/"}
	if len(started) != 1 || !slices.Equal(started[0], want) {
		t.Fatalf("started %v, want %v", started, want)
	}
	if _, err := Open(list, "edge", "https://example.com/"); err == nil || err.Error() != "edge is not installed" {
		t.Fatalf("%v", err)
	}
	if _, err := Open(list, "", "file:///x"); err == nil {
		t.Fatal("a file URL opened")
	}
	if len(started) != 1 {
		t.Fatalf("a refusal started a browser: %v", started)
	}
}

func TestProfileExists(t *testing.T) {
	root := t.TempDir()
	prev := UserDataRoot
	UserDataRoot = func(string) string { return root }
	t.Cleanup(func() { UserDataRoot = prev })
	os.Mkdir(filepath.Join(root, "Profile 2"), 0o755)
	if ex, known := ProfileExists("chrome", "Profile 2"); !ex || !known {
		t.Fatal("an existing profile")
	}
	for _, p := range []string{"Profile 9", "", "..", "a/b"} {
		if ex, _ := ProfileExists("chrome", p); ex {
			t.Errorf("%q exists", p)
		}
	}
	UserDataRoot = func(string) string { return "" }
	if _, known := ProfileExists("chrome", "x"); known {
		t.Fatal("known with no user data folder")
	}
}
