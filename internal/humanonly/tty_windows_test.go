//go:build windows

package humanonly

import (
	"os"
	"path/filepath"
	"testing"
)

// The pipe-name probe tells an anonymous pipe and a plain file from a Cygwin
// pty; only the latter counts as a terminal.
func TestPipeNameAndCygwinPty(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	for _, f := range []*os.File{r, w} {
		name, ok := pipeName(f)
		if !ok {
			t.Fatalf("an anonymous pipe has a name: %q", name)
		}
		if cygwinPtyName(name) || cygwinPty(f) {
			t.Fatalf("an anonymous pipe is no Cygwin pty: %q", name)
		}
		if isTerminal(f) {
			t.Fatal("an anonymous pipe is no terminal")
		}
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, ok := pipeName(f); ok {
		t.Fatal("a file is not a pipe")
	}
	if cygwinPty(f) || isTerminal(f) {
		t.Fatal("a file is no terminal")
	}
	if cygwinPty(nil) || isTerminal(nil) {
		t.Fatal("nil is no terminal")
	}
}
