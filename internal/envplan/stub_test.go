package envplan

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The mask's tq-stub.exe is the tq binary itself, never the program that called the render
// (here the test binary, as a harness or a setup tool would be): with no tq on PATH there is
// no stub program, with one the stub is that file.
func TestPathLayer_StubIsTheTQBinary(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the .exe stubs are Windows'")
	}
	prev := StubSource
	StubSource = DefaultStubSource
	t.Cleanup(func() { StubSource = prev })
	t.Setenv("TQ_HOME", testutil.TempDir(t))

	empty := testutil.TempDir(t)
	t.Setenv("PATH", empty)
	if _, err := DefaultStubSource(); err == nil {
		t.Fatal("a stub source without tq on PATH (the caller is not tq)")
	}
	if _, err := LayerFor(companyX(t, true)).WriteMask(); err == nil {
		t.Fatal("WriteMask linked the stub to the calling program")
	}

	bin := filepath.Join(testutil.TempDir(t), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tq := filepath.Join(bin, "tq.exe")
	if err := os.WriteFile(tq, []byte("MZ fake tq binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	l := LayerFor(companyX(t, true))
	if _, err := l.WriteMask(); err != nil {
		t.Fatal(err)
	}
	self, _ := os.Executable()
	sfi, _ := os.Stat(self)
	tfi, _ := os.Stat(tq)
	for _, n := range []string{StubName, "aws.exe", "aws.com"} {
		fi, err := os.Stat(filepath.Join(l.MaskDir(), n))
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(fi, sfi) {
			t.Fatalf("%s is the calling program", n)
		}
		if !os.SameFile(fi, tfi) && fi.Size() != tfi.Size() {
			t.Fatalf("%s is not the tq binary", n)
		}
	}
}
