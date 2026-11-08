package envplan

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/testutil"
)

// The PATH layer: the state fits cmd.exe's line, the mask answers every
// spelling, comes back once fixed, and leaves ambiguous names and generic tools alone.

// longPath is a realistic PATH of n entries (vendor folders, as a developer machine has).
func longPath(prefix []string, n int) string {
	es := append([]string{}, prefix...)
	for i := 0; i < n; i++ {
		es = append(es, fmt.Sprintf(`C:\Program Files\Some Vendor %d\Some Product Name\Version 12.3.%d\bin`, i%7, i))
	}
	return strings.Join(es, ";")
}

// TestState_FitsCmdLine: with an 8 KB+ PATH recorded and the identity PATH it dropped one
// entry of and put the mask in front of, __TQ_STATE is far below cmd.exe's 8191-character
// line (one compressed PATH, the PATH tq wrote spelled as a drop list), and decodes to the
// same PATH byte for byte. A state of the previous encoding still decodes.
func TestState_FitsCmdLine(t *testing.T) {
	orig := longPath([]string{`C:\aws`, `C:\shared`}, 120)
	if len(orig) < 8192 {
		t.Fatalf("PATH is only %d bytes", len(orig))
	}
	set := strings.Join(append([]string{`C:\mask`}, strings.Split(orig, ";")[1:]...), ";")
	o := orig
	s := State{WS: "company-x", Prev: map[string]*string{"PATH": &o, "AWS_PROFILE": ptr("prod")}, PathSet: set, Sep: ";", MaskWanted: true}
	enc := s.Encode()
	if len(enc) > 2048 {
		t.Fatalf("state is %d characters for a %d-byte PATH", len(enc), len(orig))
	}
	got := DecodeState(enc)
	if got.PathSet != set || *got.Prev["PATH"] != orig || !got.MaskWanted || got.Sep != ";" || *got.Prev["AWS_PROFILE"] != "prod" {
		t.Fatalf("round trip: %+v", got)
	}
	// A PATH tq wrote that is not orig minus entries plus a mask is carried as it is.
	s.PathSet = `C:\elsewhere;` + orig
	if got := DecodeState(s.Encode()); got.PathSet != s.PathSet {
		t.Fatal("full PathSet lost")
	}
	// Nothing dropped, no mask.
	s.PathSet = orig
	if got := DecodeState(s.Encode()); got.PathSet != orig {
		t.Fatal("identical PathSet lost")
	}
	// Garbage decodes to an empty state, never panics.
	for _, g := range []string{"z1.!!!", "z1." + strings.Repeat("A", 100), "%%%", "eyJXUyI6ImFjbWUifQ"} {
		if got := DecodeState(g); got.WS != "" || got.Prev == nil {
			t.Fatalf("%q: %+v", g, got)
		}
	}
}

func ptr(s string) *string { return &s }

// The mask answers aws however it is spelled: aws, aws.exe, aws.com, aws.bat, aws.cmd,
// aws.ps1, with PATHEXT edited too; tentaqles run's lookup lands on it for aws.exe; a batch file
// that runs aws without `call` keeps going (the .com/.exe stub is a program).
func TestPathLayer_MaskCoversEverySpelling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PATHEXT spellings are Windows'")
	}
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	shared := filepath.Join(testutil.TempDir(t), "shared")
	for _, n := range []string{"aws.exe", "aws.bat", "aws.ps1", "vercel.ps1", "vercel.cmd", "node.exe"} {
		fakeExe(t, shared, n)
	}
	l := LayerFor(companyX(t, true))
	if _, err := l.WriteMask(); err != nil {
		t.Fatal(err)
	}
	l = LayerFor(companyX(t, true))
	if !l.MaskReady() {
		t.Fatal("mask not ready after WriteMask")
	}
	got, _ := l.FilterPath(shared, NativeSyntax())
	for _, n := range []string{"aws", "aws.exe", "aws.com", "aws.bat", "aws.cmd", "aws.ps1", "vercel", "vercel.ps1"} {
		p, err := LookPath(n, pathEnv(got))
		if err != nil || filepath.Dir(p) != l.MaskDir() {
			t.Errorf("%s resolves to %q (%v), want the mask", n, p, err)
		}
	}
	env := func() []string {
		var out []string
		for _, kv := range os.Environ() {
			if k, _, _ := splitKV(kv); envKey(k) != envKey("PATH") && envKey(k) != envKey("PATHEXT") {
				out = append(out, kv)
			}
		}
		return append(out, "PATH="+got+";"+filepath.Join(os.Getenv("SystemRoot"), "System32"), "PATHEXT=.COM;.EXE;.BAT;.CMD;.PS1")
	}
	run := func(what string, c *exec.Cmd) string {
		c.Env = env()
		out, _ := c.CombinedOutput()
		return what + ": " + string(out)
	}
	// aws.exe and aws.com are the stub program: exit 127 and the line.
	c := exec.Command(filepath.Join(l.MaskDir(), "aws.exe"), "ec2", "run-instances")
	out, err := c.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 127 || !strings.Contains(string(out), "aws: command not found (outside the scope of identity company-x)") {
		t.Fatalf("aws.exe stub: %v %q", err, out)
	}
	// A batch file running aws without call goes on after it.
	bat := filepath.Join(testutil.TempDir(t), "deploy.cmd")
	if err := os.WriteFile(bat, []byte("@echo off\r\naws ec2 run-instances\r\necho AFTER=%ERRORLEVEL%\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if s := run("bat", exec.Command(cmdExe, "/c", bat)); !strings.Contains(s, "AFTER=127") || !strings.Contains(s, "command not found") {
		t.Fatalf("batch without call: %s", s)
	}
	for _, spelled := range []string{"aws.exe", "aws.bat", "aws.cmd"} {
		if s := run(spelled, exec.Command(cmdExe, "/c", spelled)); !strings.Contains(s, "command not found") {
			t.Errorf("cmd %s", s)
		}
	}
	if ps, err := exec.LookPath("powershell"); err == nil {
		script := "& { aws ec2 x; 'E1=' + $LASTEXITCODE; aws.exe x; 'E2=' + $LASTEXITCODE; vercel.ps1 x; 'E3=' + $LASTEXITCODE; $env:PATHEXT='.COM;.EXE'; vercel x; 'E4=' + $LASTEXITCODE }"
		s := run("powershell", exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", script))
		for _, e := range []string{"E1=127", "E2=127", "E3=127", "E4=127"} {
			if !strings.Contains(s, e) {
				t.Errorf("powershell: %s missing: %s", e, s)
			}
		}
		if strings.Contains(s, "fake ") {
			t.Errorf("powershell reached a real CLI: %s", s)
		}
	}
}

// TestPathLayer_MaskComesBack: the mask taken off the PATH because it was changed comes back
// at the next prompt once it is as tq wrote it, without a cd or a PATH change; while it is
// missing the notice repeats at every prompt.
func TestPathLayer_MaskComesBack(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	shared := filepath.Join(testutil.TempDir(t), "shared")
	fakeExe(t, shared, "aws.exe")
	fakeExe(t, shared, "node.exe")
	ws := companyX(t, true)
	if _, err := LayerFor(ws).WriteMask(); err != nil {
		t.Fatal(err)
	}
	maskDir := MaskDir("company-x")
	sh := map[string]string{"PATH": shared}
	var st State
	prompt := func() *Notice {
		e := Env{Lookup: env(sh), Names: names(sh), Syntax: NativeSyntax(), PathOK: true}
		w, n := Plan(ws, st, e)
		ops, _ := DiffWant(w, e.Lookup, st, "company-x")
		shellSim(sh, ops)
		st = DecodeState(sh[StateVar])
		return n
	}
	onPath := func() bool { return strings.Contains(sh["PATH"], maskDir) }
	if prompt(); !onPath() {
		t.Fatal("mask not on PATH")
	}
	plainFile(t, maskDir, "git.cmd") // tampered
	if n := prompt(); onPath() || n == nil || n.Kind != NoticeMask {
		t.Fatalf("tampered mask: on=%v notice=%v", onPath(), n)
	}
	if n := prompt(); n == nil {
		t.Fatal("the notice does not repeat")
	}
	if err := os.Remove(filepath.Join(maskDir, "git.cmd")); err != nil {
		t.Fatal(err)
	}
	if n := prompt(); !onPath() || n != nil {
		t.Fatalf("fixed mask not back: on=%v notice=%v PATH=%q", onPath(), n, sh["PATH"])
	}
	// Removed altogether, then rendered again.
	if err := os.RemoveAll(maskDir); err != nil {
		t.Fatal(err)
	}
	if prompt(); onPath() {
		t.Fatal("missing mask on PATH")
	}
	if _, err := LayerFor(ws).WriteMask(); err != nil {
		t.Fatal(err)
	}
	if prompt(); !onPath() {
		t.Fatal("re-rendered mask not back")
	}
}

// Ambiguous names (atlas, render, fly, vc, cn, tea, snow, copilot) are neither dropped nor
// masked: another program may own the word. Their provider's own CLI (flyctl) still is.
func TestPathLayer_AmbiguousNamesLeftAlone(t *testing.T) {
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	onlyAtlas := filepath.Join(testutil.TempDir(t), "atlas")
	fakeExe(t, onlyAtlas, "atlas.exe")
	shared := filepath.Join(testutil.TempDir(t), "bin")
	for _, n := range []string{"render.exe", "fly.exe", "vc.cmd", "node.exe"} {
		fakeExe(t, shared, n)
	}
	flyDir := filepath.Join(testutil.TempDir(t), "fly")
	fakeExe(t, flyDir, "flyctl.exe")
	fakeExe(t, flyDir, "fly.exe")
	l := LayerFor(companyX(t, true))
	got, rep := l.FilterPath(join(onlyAtlas, shared, flyDir), NativeSyntax())
	// flyctl beside an ambiguous fly: the folder is kept and flyctl masked (fly may be another tool).
	if got != join(onlyAtlas, shared, flyDir) || !reflect.DeepEqual(rep.Shared, []string{flyDir}) || len(rep.Dropped) != 0 {
		t.Fatalf("got %q %+v", got, rep)
	}
	for _, n := range []string{"atlas", "render", "fly", "vc", "cn", "tea", "snow", "copilot"} {
		if _, ok := l.MaskFiles()[n]; ok {
			t.Errorf("the mask holds %s", n)
		}
	}
	if _, ok := l.MaskFiles()["flyctl"]; !ok {
		t.Error("the mask lacks flyctl")
	}
}

// A quoted entry is read as its folder; a network folder is never read.
func TestPathLayer_QuotedAndNetworkEntries(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("quoted PATH entries are Windows'")
	}
	t.Setenv("TQ_HOME", testutil.TempDir(t))
	awsDir, azDir, _ := globalDirs(t)
	l := LayerFor(companyX(t, true))
	in := join(`"`+awsDir+`"`, `\\localhost\c$\Windows`, azDir)
	got, _ := l.FilterPath(in, NativeSyntax())
	if want := join(`\\localhost\c$\Windows`, azDir); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

