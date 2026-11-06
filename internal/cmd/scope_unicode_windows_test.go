package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/shell"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
	"github.com/zalando/go-keyring"
)

// QA round 3: PATH folders named ação, João and 中文 go through Windows PowerShell 5.1's and
// cmd.exe's real hooks (tq prints UTF-8; the hooks read it as UTF-8 and put the console
// encoding back): inside company-x the shared folder is kept exactly and its tools resolve,
// the AWS folder is dropped, and leaving restores PATH byte for byte.
func TestEnv_UnicodePathFoldersRoundTrip(t *testing.T) {
	ps, err := exec.LookPath("powershell")
	if err != nil {
		t.Skip("no Windows PowerShell")
	}
	root, _ := setupScoped(t, "company-x", "guard: v2\n"+companyXBody)
	runEnv(t, "settings", "render", "company-x")
	key, err := keyring.Get(trust.Service, trust.KeyAccount)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trust.KeyFile(), []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	fx := testutil.TempDir(t)
	awsDir := filepath.Join(fx, "ação", "awsdir")
	shared := filepath.Join(fx, "João", "shared")
	tools := filepath.Join(fx, "中文", "tools")
	fakeIn(t, awsDir, "aws.exe")
	fakeIn(t, shared, "aws.exe", "node.exe")
	fakeIn(t, tools, "git.exe")
	sys := filepath.Join(os.Getenv("SystemRoot"), "System32")
	path := strings.Join([]string{awsDir, shared, tools, sys, filepath.Dir(sys), filepath.Join(sys, "WindowsPowerShell", "v1.0")}, ";")
	self, _ := os.Executable()
	dir := testutil.TempDir(t)
	out := func(n string) string { return filepath.Join(dir, n) }
	neutral := testutil.TempDir(t)
	env := append(envWithout("PATH", "__TQ_STATE", "TQ_WS", "TQ_WS_ROOT"), "PATH="+path, "TQ_TEST_BE_TQ=1")
	read := func(n string) string {
		b, err := os.ReadFile(out(n))
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		return strings.TrimPrefix(string(b), "\uFEFF")
	}
	check := func(shellName string) {
		t.Helper()
		in, after, node := read(shellName+"-in.txt"), read(shellName+"-after.txt"), read(shellName+"-node.txt")
		ins := strings.Split(in, ";")
		if !hasString(ins, shared) || hasString(ins, awsDir) || !hasString(ins, tools) {
			t.Errorf("%s: identity PATH = %q", shellName, in)
		}
		if strings.TrimSpace(node) != filepath.Join(shared, "node.exe") {
			t.Errorf("%s: node resolves to %q", shellName, node)
		}
		if after != path {
			t.Errorf("%s: PATH after leaving\n got %q\nwant %q", shellName, after, path)
		}
	}

	// Windows PowerShell 5.1 through its real hook.
	script := out("t.ps1")
	// The console starts on code page 850, a Western default, and must end on it.
	body := "[Console]::OutputEncoding = [Text.Encoding]::GetEncoding(850)\r\n" +
		"& '" + self + "' activate powershell | Out-String | Invoke-Expression\r\n" +
		"Set-Location -LiteralPath '" + root + "'; __tq_hook\r\n" +
		"[IO.File]::WriteAllText('" + out("ps-in.txt") + "', $env:PATH)\r\n" +
		"[IO.File]::WriteAllText('" + out("ps-node.txt") + "', (Get-Command node).Source)\r\n" +
		"Set-Location -LiteralPath '" + neutral + "'; __tq_hook\r\n" +
		"[IO.File]::WriteAllText('" + out("ps-after.txt") + "', $env:PATH)\r\n" +
		"[IO.File]::WriteAllText('" + out("ps-cp.txt") + "', [string][Console]::OutputEncoding.CodePage)\r\n"
	if err := os.WriteFile(script, []byte("\uFEFF"+body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := exec.Command(ps, "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", script)
	c.Env = env
	if o, err := c.CombinedOutput(); err != nil {
		t.Fatalf("powershell: %v %s", err, o)
	}
	check("ps")
	if cp := read("ps-cp.txt"); cp != "850" {
		t.Errorf("powershell: the console encoding was not put back: %s", cp)
	}

	// cmd.exe through its real hook, on its default code page.
	hook, err := shell.HookFor("cmd", self)
	if err != nil {
		t.Fatal(err)
	}
	hookFile := out("hook.cmd")
	if err := os.WriteFile(hookFile, []byte(strings.ReplaceAll(hook, "\n", "\r\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	dump := out("dump.ps1")
	if err := os.WriteFile(dump, []byte("param($f) [IO.File]::WriteAllText($f, [Environment]::GetEnvironmentVariable('PATH'))\r\n"+
		"if ($args.Count -gt 0) { [IO.File]::WriteAllText($args[0], (Get-Command node).Source) }\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	psCall := `"` + ps + `" -NoProfile -ExecutionPolicy Bypass -File "` + dump + `" `
	// The batch file is ASCII: the folders it names reach it through the environment only.
	// It starts on code page 850, a Western default, and must end on it.
	bat := strings.Join([]string{"@echo off", "chcp 850 >nul",
		`cd /d "` + root + `"`, `call "` + hookFile + `"`,
		psCall + `"` + out("cmd-in.txt") + `" "` + out("cmd-node.txt") + `"`,
		`cd /d "` + neutral + `"`, `call "` + hookFile + `"`,
		psCall + `"` + out("cmd-after.txt") + `"`, "chcp"}, "\r\n") + "\r\n"
	if err := os.WriteFile(out("t.cmd"), []byte(bat), 0o600); err != nil {
		t.Fatal(err)
	}
	c = exec.Command(filepath.Join(sys, "cmd.exe"), "/c", out("t.cmd"))
	c.Env = env
	o, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("cmd: %v %s", err, o)
	}
	if !strings.Contains(string(o), "850") || strings.Contains(string(o), "65001") {
		t.Errorf("cmd: the code page was not put back: %s", o)
	}
	check("cmd")
}
