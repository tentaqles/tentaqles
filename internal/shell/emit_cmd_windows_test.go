package shell

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// cmd.exe through the real hook (ADR-0008, round 2): every character a Windows PATH can hold
// (% ! ^ & | < > and quotes) arrives exactly, with delayed expansion off and on, and the
// notice line is printed; a line longer than cmd.exe reads is refused, never cut.
func TestEmit_Cmd_RoundTripsEveryCharacter(t *testing.T) {
	dir := t.TempDir()
	values := map[string]string{
		"PROBE_PCT":   `C:\a;%JAVA_HOME%\bin;%A%`,
		"PROBE_BANG":  `C:\x!y!z;C:\q^w`,
		"PROBE_META":  `C:\amp&b;C:\p|q;C:\lt<gt>`,
		"PROBE_QUOTE": `"C:\quoted dir";C:\end`,
		"PROBE_CARET": `C:\only^caret`,
	}
	ops := envplan.Ops{Changed: true, Set: values, Unset: []string{"PROBE_GONE"},
		Notice: &envplan.Notice{Kind: envplan.NoticeMask, Workspace: "company-x"}}
	for _, delayed := range []bool{false, true} {
		out, err := EmitOpts("cmd", ops, Opts{CmdDelayed: delayed})
		if err != nil {
			t.Fatal(err)
		}
		name := "plain.txt"
		if delayed {
			name = "delayed.txt"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(strings.ReplaceAll(out, "\n", "\r\n")), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A fake tq that prints the emitted lines: the delayed form when asked for it.
	fake := filepath.Join(dir, "faketq.cmd")
	if err := os.WriteFile(fake, []byte("@if \"%4\"==\"--cmd-delayed\" (type \"%~dp0delayed.txt\") else (type \"%~dp0plain.txt\")\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	hook, err := HookFor("cmd", fake)
	if err != nil {
		t.Fatal(err)
	}
	hookFile := filepath.Join(dir, "hook.cmd")
	if err := os.WriteFile(hookFile, []byte(strings.ReplaceAll(hook, "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no powershell to read the result")
	}
	show := filepath.Join(dir, "show.ps1")
	if err := os.WriteFile(show, []byte("foreach ($k in $args) { $k + '=[' + [Environment]::GetEnvironmentVariable($k) + ']' }\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	keys := []string{"PROBE_PCT", "PROBE_BANG", "PROBE_META", "PROBE_QUOTE", "PROBE_CARET", "PROBE_GONE"}
	wrapper := filepath.Join(dir, "run.cmd")
	if err := os.WriteFile(wrapper, []byte("@echo off\r\nset \"A=EXPANDED\"\r\nset \"JAVA_HOME=J\"\r\nset \"PROBE_GONE=still\"\r\ncall \""+hookFile+"\"\r\n\""+ps+"\" -NoProfile -ExecutionPolicy Bypass -File \""+show+"\" "+strings.Join(keys, " ")+"\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	for _, flags := range [][]string{{"/v:off"}, {"/v:on"}} {
		out, err := exec.Command(cmdExe, append(flags, "/c", wrapper)...).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", flags, err, out)
		}
		s := string(out)
		for k, v := range values {
			if !strings.Contains(s, k+"=["+v+"]") {
				t.Errorf("%v: %s not exact:\n%s", flags, k, s)
			}
		}
		if !strings.Contains(s, "PROBE_GONE=[]") {
			t.Errorf("%v: unset failed:\n%s", flags, s)
		}
		if !strings.Contains(s, "tq: PATH mask missing") {
			t.Errorf("%v: no notice:\n%s", flags, s)
		}
	}
	// Too long a line is an error, not a cut.
	long := envplan.Ops{Changed: true, Set: map[string]string{"PATH": strings.Repeat("x", CmdLineMax)}}
	if _, err := Emit("cmd", long); err == nil || !strings.Contains(err.Error(), "PATH") {
		t.Fatalf("long line: %v", err)
	}
}

// TestEmit_Cmd_LineBudget measures CmdLineMax through the real hook: a printed line of
// exactly CmdLineMax bytes runs, one byte more is dropped (with every line after it), with
// delayed expansion off and on. Emit accepts the first and refuses the second.
func TestEmit_Cmd_LineBudget(t *testing.T) {
	dir := t.TempDir()
	line := func(n int) string {
		head := `set "PROBE_BIG=`
		return head + strings.Repeat("x", n-len(head)-1) + `"`
	}
	for _, n := range []int{CmdLineMax, CmdLineMax + 1} {
		body := line(n) + "\r\n" + `set "PROBE_AFTER=yes"` + "\r\n"
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("out%d.txt", n)), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no powershell to read the result")
	}
	show := filepath.Join(dir, "show.ps1")
	if err := os.WriteFile(show, []byte("'BIG=' + ([string]$env:PROBE_BIG).Length + ' AFTER=' + $env:PROBE_AFTER\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmdExe := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	for _, n := range []int{CmdLineMax, CmdLineMax + 1} {
		fake := filepath.Join(dir, fmt.Sprintf("faketq%d.cmd", n))
		if err := os.WriteFile(fake, []byte(fmt.Sprintf("@type \"%%~dp0out%d.txt\"\r\n", n)), 0o600); err != nil {
			t.Fatal(err)
		}
		hook, err := HookFor("cmd", fake)
		if err != nil {
			t.Fatal(err)
		}
		hookFile := filepath.Join(dir, fmt.Sprintf("hook%d.cmd", n))
		if err := os.WriteFile(hookFile, []byte(strings.ReplaceAll(hook, "\n", "\r\n")), 0o600); err != nil {
			t.Fatal(err)
		}
		wrapper := filepath.Join(dir, fmt.Sprintf("run%d.cmd", n))
		if err := os.WriteFile(wrapper, []byte("@echo off\r\ncall \""+hookFile+"\"\r\n\""+ps+"\" -NoProfile -ExecutionPolicy Bypass -File \""+show+"\"\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, v := range []string{"/v:off", "/v:on"} {
			out, _ := exec.Command(cmdExe, v, "/c", wrapper).CombinedOutput()
			s := string(out)
			want := fmt.Sprintf("BIG=%d AFTER=yes", n-len(`set "PROBE_BIG=`)-1)
			if n > CmdLineMax {
				want = "BIG=0 AFTER="
			}
			if !strings.Contains(s, want) {
				t.Errorf("line of %d bytes, %s: got %q, want %q", n, v, strings.TrimSpace(s), want)
			}
		}
	}
	if _, err := Emit("cmd", envplan.Ops{Changed: true, Set: map[string]string{"PROBE_BIG": strings.Repeat("x", CmdLineMax-len(`set "PROBE_BIG="`))}}); err != nil {
		t.Fatalf("Emit refuses a line of exactly CmdLineMax: %v", err)
	}
	if _, err := Emit("cmd", envplan.Ops{Changed: true, Set: map[string]string{"PROBE_BIG": strings.Repeat("x", CmdLineMax-len(`set "PROBE_BIG="`)+1)}}); err == nil {
		t.Fatal("Emit accepts a line over CmdLineMax")
	}
}
