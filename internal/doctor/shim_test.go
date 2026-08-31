package doctor

import "testing"

// theShimThatBitUs is the real ~/.cli-identities/shims/claude.cmd from the
// machine this check was written for. It looks harmless -- it asks tq for the
// identity -- but it asks for the identity of the *current directory*, so it
// silently replaced the workspace `tq login <ws> claude` had already chosen.
const theShimThatBitUs = `@echo off
setlocal
if "%TQ_ENABLED%"=="0" goto legacy
rem tq: resolve terminal identity for %CD% (CLAUDE_CONFIG_DIR, GH_CONFIG_DIR, AZURE_CONFIG_DIR, ...)
for /f "usebackq delims=" %%L in ` + "`" + `"%LOCALAPPDATA%\tentaqles\bin\tq.exe" env --shell cmd` + "`" + ` do %%L
goto run
:legacy
call "%~dp0_ctx.cmd"
if "%CTX%"=="dirtybird" set "CLAUDE_CONFIG_DIR=%USERPROFILE%\.claude-dbi"
:run
"%USERPROFILE%\.local\bin\claude.exe" --dangerously-skip-permissions %*
endlocal
`

func TestShimShadow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resolved string
		body     string
		vars     []string
		want     bool
		wantWhy  string
	}{
		{
			name:     "the real shim from the machine this was written for",
			resolved: `C:\Users\x\.cli-identities\shims\claude.cmd`,
			body:     theShimThatBitUs,
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     true,
			wantWhy:  "it sets CLAUDE_CONFIG_DIR itself",
		},
		{
			name:     "a shim that only asks tq, never assigning directly",
			resolved: `C:\shims\gh.cmd`,
			body:     "@echo off\nfor /f %%L in (`tq env --shell cmd`) do %%L\ngh.exe %*\n",
			vars:     []string{"GH_CONFIG_DIR"},
			want:     true,
			wantWhy:  "it re-runs `tq env` for the current directory",
		},
		{
			name:     "powershell shim assigning the var",
			resolved: `C:\shims\az.ps1`,
			body:     "$env:AZURE_CONFIG_DIR = \"$HOME\\.azure-work\"\n& az.exe @args\n",
			vars:     []string{"AZURE_CONFIG_DIR"},
			want:     true,
			wantWhy:  "it sets AZURE_CONFIG_DIR itself",
		},
		{
			name:     "posix shim",
			resolved: "/usr/local/bin/claude.sh",
			body:     "#!/bin/sh\nCLAUDE_CONFIG_DIR=$HOME/.claude-work exec claude \"$@\"\n",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     true,
		},
		// Not shims.
		{
			name:     "a real binary is never a shim",
			resolved: `C:\Users\x\.local\bin\claude.exe`,
			body:     "CLAUDE_CONFIG_DIR=whatever",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     false,
		},
		{
			name:     "extensionless binary on posix",
			resolved: "/usr/local/bin/claude",
			body:     "\x7fELF binary bytes",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     false,
		},
		{
			name:     "a wrapper that only READS the variable is fine",
			resolved: `C:\shims\claude.cmd`,
			body:     "@echo off\necho using %CLAUDE_CONFIG_DIR%\n\"%USERPROFILE%\\.local\\bin\\claude.exe\" %*\n",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     false,
		},
		{
			name:     "a plain launcher that adds a flag is fine",
			resolved: `C:\shims\claude.cmd`,
			body:     "@echo off\n\"%USERPROFILE%\\.local\\bin\\claude.exe\" --dangerously-skip-permissions %*\n",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     false,
		},
		{
			name:     "a script mentioning an unrelated variable is fine",
			resolved: `C:\shims\claude.cmd`,
			body:     "@echo off\nset \"SOME_OTHER_DIR=x\"\nclaude.exe %*\n",
			vars:     []string{"CLAUDE_CONFIG_DIR"},
			want:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			read := func(string) ([]byte, error) { return []byte(tc.body), nil }
			why, got := shimShadow(tc.resolved, tc.vars, read)
			if got != tc.want {
				t.Fatalf("shimShadow = %v (%q), want %v", got, why, tc.want)
			}
			if tc.wantWhy != "" && why != tc.wantWhy {
				t.Errorf("reason = %q, want %q", why, tc.wantWhy)
			}
		})
	}
}

func TestShimShadow_UnreadableFileIsNotAFinding(t *testing.T) {
	read := func(string) ([]byte, error) { return nil, errRead }
	if _, got := shimShadow(`C:\shims\claude.cmd`, []string{"CLAUDE_CONFIG_DIR"}, read); got {
		t.Fatal("a file tq cannot read must not produce a finding")
	}
}

func TestShimShadow_EmptyPathOrNoVars(t *testing.T) {
	read := func(string) ([]byte, error) { return []byte(theShimThatBitUs), nil }
	if _, got := shimShadow("", []string{"CLAUDE_CONFIG_DIR"}, read); got {
		t.Error("empty resolved path must not produce a finding")
	}
	// No identity vars: the tq-env fallback still catches it.
	if _, got := shimShadow(`C:\shims\claude.cmd`, nil, read); !got {
		t.Error("a tq-env shim should still be reported when the provider declares no vars")
	}
}

var errRead = errStr("permission denied")

type errStr string

func (e errStr) Error() string { return string(e) }
