package policy

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/protect"
)

// selfProtectionSet is a protected set over a temp home for the rule tests.
func selfProtectionSet(t *testing.T) (*protect.Set, string, string) {
	t.Helper()
	home := t.TempDir()
	if r, err := filepath.EvalSymlinks(home); err == nil {
		home = r
	}
	state := filepath.Join(home, ".tentaqles")
	cwd := filepath.Join(home, "repos", "acme")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	s := protect.New(protect.Inputs{
		Home: state, UserHome: home, Bases: []string{filepath.Join(home, "repos")}, Roots: []string{cwd},
		Executable: filepath.Join(home, "bin", "tq"),
		Env:        func(k string) string { return map[string]string{"TQ_HOME": state}[k] },
	})
	return s, state, cwd
}

func TestSelfProtectionShellAndFileTools(t *testing.T) {
	s, state, cwd := selfProtectionSet(t)
	manifest := filepath.Join(state, "identities", "acme", "manifest.yaml")
	marker := filepath.Join(cwd, ".tentaqles.yaml")
	cases := []struct {
		name string
		call ToolCall
		want string // the rule id, "" = allow
	}{
		{"write manifest", ToolCall{Tool: "Write", Path: manifest}, ProtectedPathID},
		{"read manifest", ToolCall{Tool: "Read", Path: manifest}, ProtectedPathID},
		{"read marker", ToolCall{Tool: "Read", Path: marker}, ""},
		{"write marker", ToolCall{Tool: "Write", Path: marker}, ProtectedPathID},
		{"write elsewhere", ToolCall{Tool: "Write", Path: filepath.Join(cwd, "x.md")}, ""},
		{"relative write elsewhere", ToolCall{Tool: "Write", Path: "x.md"}, ""},
		{"relative write to the marker", ToolCall{Tool: "Edit", Path: ".tentaqles.yaml"}, ProtectedPathID},
		{"unknown tool", ToolCall{Tool: "WebFetch", Path: manifest}, ""},
		{"redirect", ToolCall{Tool: "Bash", Command: "echo x > ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"dd of=", ToolCall{Tool: "Bash", Command: "dd if=/dev/zero of=$TQ_HOME/config.yaml"}, ProtectedPathID},
		{"find -delete", ToolCall{Tool: "Bash", Command: "find ~/.tentaqles -name x -delete"}, ProtectedPathID},
		{"find listing", ToolCall{Tool: "Bash", Command: "find ~/.tentaqles -name x"}, ""},
		{"sed without -i", ToolCall{Tool: "Bash", Command: "sed 's/a/b/' " + manifest}, ProtectedPathID},
		{"sed -i elsewhere", ToolCall{Tool: "Bash", Command: "sed -i 's/a/b/' notes.md"}, ""},
		{"perl -pi", ToolCall{Tool: "Bash", Command: "perl -pi -e 's/a/b/' " + manifest}, ProtectedPathID},
		{"tar", ToolCall{Tool: "Bash", Command: "tar czf backup.tgz ~/.tentaqles"}, ProtectedPathID},
		{"stderr to a descriptor", ToolCall{Tool: "Bash", Command: "cmd 2>&1 | tee log.txt"}, ""},
		{"cd unknown then rm relative", ToolCall{Tool: "Bash", Command: "cd - && rm config.yaml"}, ""},
		{"cd unknown then rm absolute", ToolCall{Tool: "Bash", Command: "cd - && rm " + manifest}, ProtectedPathID},
		{"ls", ToolCall{Tool: "Bash", Command: "ls -la ~/.tentaqles"}, ""},
		{"python script", ToolCall{Tool: "Bash", Command: "python build.py"}, ""},
		{"python -c mention", ToolCall{Tool: "Bash", Command: `python -c "import shutil; shutil.rmtree('` + filepath.ToSlash(state) + `')"`}, ProtectedPathID},
		{"node -e plain", ToolCall{Tool: "Bash", Command: `node -e "console.log(1)"`}, ""},
		{"mcp path", ToolCall{Tool: "mcp__fs__write", Content: `{"path":"` + strings.ReplaceAll(manifest, `\`, `\\`) + `"}`}, ProtectedPathID},
		{"mcp nested path", ToolCall{Tool: "mcp__fs__write", Content: `{"files":[{"path":"~/.tentaqles/config.yaml"}]}`}, ProtectedPathID},
		{"mcp plain", ToolCall{Tool: "mcp__db__query", Content: `{"sql":"select 1"}`}, ""},
		{"mcp url", ToolCall{Tool: "mcp__web__fetch", Content: `{"url":"https://example.com/.tentaqles/x"}`}, ""},
		// git: global options name the repository; path arguments are
		// writes for a sub-command that writes, reads otherwise
		{"git -C state, read sub", ToolCall{Tool: "Bash", Command: "git -C ~/.tentaqles status"}, ProtectedPathID},
		{"git --git-dir= state", ToolCall{Tool: "Bash", Command: "git --git-dir=$TQ_HOME/.git log"}, ProtectedPathID},
		{"git diff a state file", ToolCall{Tool: "Bash", Command: "git diff --no-index " + manifest + " x"}, ProtectedPathID},
		{"git log the marker", ToolCall{Tool: "Bash", Command: "git log -- .tentaqles.yaml"}, ""},
		{"git checkout the marker", ToolCall{Tool: "Bash", Command: "git checkout -- .tentaqles.yaml"}, ProtectedPathID},
		{"git status", ToolCall{Tool: "Bash", Command: "git status"}, ""},
		// cd into a protected folder, then a write with no path argument
		{"cd state then git init", ToolCall{Tool: "Bash", Command: "cd ~/.tentaqles && git init"}, ProtectedPathID},
		{"cd state then touch", ToolCall{Tool: "Bash", Command: "cd $TQ_HOME; touch x"}, ProtectedPathID},
		{"cd state then ls", ToolCall{Tool: "Bash", Command: "cd ~/.tentaqles && ls -la"}, ""},
		{"cd state then git status", ToolCall{Tool: "Bash", Command: "cd ~/.tentaqles && git status"}, ""},
		{"cd the bin dir then rm another tool", ToolCall{Tool: "Bash", Command: "cd " + filepath.ToSlash(filepath.Join(filepath.Dir(state), "bin")) + " && rm oldtool"}, ""},
		{"cd the bin dir then rm tq", ToolCall{Tool: "Bash", Command: "cd " + filepath.ToSlash(filepath.Join(filepath.Dir(state), "bin")) + " && rm tq"}, ProtectedPathID},
		// Grep with no path searches the cwd
		{"Grep with no path in the workspace", ToolCall{Tool: "Grep"}, ""},
		// plain spellings the round-2 critic found (ADR-0005)
		{"glued redirect", ToolCall{Tool: "Bash", Command: "echo x>~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"glued append", ToolCall{Tool: "Bash", Command: "echo x>>~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"descriptor redirect is not a file", ToolCall{Tool: "Bash", Command: "make 2>&1 | tee build.log"}, ""},
		{"brace var expansion", ToolCall{Tool: "Bash", Command: "rm ${HOME}/.tentaqles/config.yaml"}, ProtectedPathID},
		{"find -delete under home", ToolCall{Tool: "Bash", Command: "find ~ -name config.yaml -delete"}, ProtectedPathID},
		{"find -delete in state", ToolCall{Tool: "Bash", Command: "find ~/.tentaqles -delete"}, ProtectedPathID},
		{"find -delete any name under home", ToolCall{Tool: "Bash", Command: "find ~ -name '*.log' -delete"}, ProtectedPathID},
		{"find -exec rm under home", ToolCall{Tool: "Bash", Command: "find ~ -name acme -type d -exec rm -rf {} +"}, ProtectedPathID},
		{"find destructive in workspace is free", ToolCall{Tool: "Bash", Command: "find . -name '*.pyc' -delete"}, ""},
		{"find no action", ToolCall{Tool: "Bash", Command: "find ~ -name config.yaml"}, ""},
		// interpreters by prefix, path arguments, in-place, stdin/pipe bodies
		{"python3.12 -c literal", ToolCall{Tool: "Bash", Command: "python3.12 -c \"open(r'" + filepath.ToSlash(state) + "/config.yaml','w')\""}, ProtectedPathID},
		{"python -c with path arg", ToolCall{Tool: "Bash", Command: "python -c \"import sys;open(sys.argv[1],'w')\" ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"node -e with path arg", ToolCall{Tool: "Bash", Command: "node -e \"x\" ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"ruby -i file", ToolCall{Tool: "Bash", Command: "ruby -i -pe 'x' ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"echo code piped to python", ToolCall{Tool: "Bash", Command: "echo \"open(r'" + filepath.ToSlash(state) + "/config.yaml','w')\" | python"}, ProtectedPathID},
		{"python a project script is free", ToolCall{Tool: "Bash", Command: "python build.py --out dist"}, ""},
		// write programs added in round 3
		{"Invoke-WebRequest -OutFile", ToolCall{Tool: "PowerShell", Command: "Invoke-WebRequest https://x -OutFile ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"Expand-Archive -DestinationPath", ToolCall{Tool: "PowerShell", Command: "Expand-Archive e.zip -DestinationPath ~/.tentaqles -Force"}, ProtectedPathID},
		{"patch file", ToolCall{Tool: "Bash", Command: "patch ~/.tentaqles/config.yaml < d.diff"}, ProtectedPathID},
		{"certutil -decode", ToolCall{Tool: "PowerShell", Command: "certutil -decode x.b64 ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"fsutil file createnew", ToolCall{Tool: "PowerShell", Command: "fsutil file createnew ~/.tentaqles/x 0"}, ProtectedPathID},
		{"Export-Clixml -Path", ToolCall{Tool: "PowerShell", Command: "Get-Date | Export-Clixml -Path ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"sort -o marker", ToolCall{Tool: "Bash", Command: "sort -o .tentaqles.yaml x"}, ProtectedPathID},
		{"Start-BitsTransfer -Destination", ToolCall{Tool: "PowerShell", Command: "Start-BitsTransfer -Source https://x -Destination ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"curl out of state is free", ToolCall{Tool: "Bash", Command: "curl -o out.json https://example.com"}, ""},
		// recursive searches rooted at home
		{"grep -r at home", ToolCall{Tool: "Bash", Command: "grep -r email ~"}, ProtectedPathID},
		{"grep -r at home, Unix spelling", ToolCall{Tool: "Bash", Command: "grep -r email " + unixSpelling(filepath.Dir(state))}, ProtectedPathID},
		{"rg at home, Unix spelling", ToolCall{Tool: "Bash", Command: "rg email " + unixSpelling(filepath.Dir(state))}, ProtectedPathID},
		{"grep -r in a project is free", ToolCall{Tool: "Bash", Command: "grep -r foo src"}, ""},
		{"findstr /s /i with a relative root in a project is free", ToolCall{Tool: "PowerShell", Command: "findstr /s /i x ."}, ""},
		{"xargs rm via pipe", ToolCall{Tool: "Bash", Command: "echo ~/.tentaqles/config.yaml | xargs rm"}, ProtectedPathID},
		{"xargs rm plain", ToolCall{Tool: "Bash", Command: "ls ~/.tentaqles/*.yaml | xargs rm -f"}, ProtectedPathID},
		{"eval literal", ToolCall{Tool: "Bash", Command: "eval 'rm ~/.tentaqles/config.yaml'"}, ProtectedPathID},
		{"iex literal", ToolCall{Tool: "PowerShell", Command: "iex 'Remove-Item ~/.tentaqles/config.yaml'"}, ProtectedPathID},
		{"curl -o", ToolCall{Tool: "Bash", Command: "curl -o ~/.tentaqles/config.yaml https://example.com/x"}, ProtectedPathID},
		{"7z -o glued", ToolCall{Tool: "Bash", Command: "7z x a.7z -o~/.tentaqles"}, ProtectedPathID},
		{"awk inplace marker", ToolCall{Tool: "Bash", Command: "awk -i inplace '{print}' .tentaqles.yaml"}, ProtectedPathID},
		{"vim the config", ToolCall{Tool: "Bash", Command: "vim -c 'wq' ~/.tentaqles/config.yaml"}, ProtectedPathID},
		{"cd state then python -c", ToolCall{Tool: "Bash", Command: "cd ~/.tentaqles && python -c \"open('config.yaml','w')\""}, ProtectedPathID},
		{"python heredoc body names state", ToolCall{Tool: "Bash", Command: "python - <<'EOF'\nopen('" + filepath.ToSlash(state) + "/config.yaml','w')\nEOF"}, ProtectedPathID},
		{"cat heredoc note is free", ToolCall{Tool: "Bash", Command: "cat > notes.md <<'EOF'\nrm -rf ~/.tentaqles\nEOF"}, ""},
		{"rm glob in root is free", ToolCall{Tool: "Bash", Command: "rm *.log"}, ""},
		{"human-only verb", ToolCall{Tool: "Bash", Command: "tq allow acme"}, HumanOnlyCommandID},
		{"free verb", ToolCall{Tool: "Bash", Command: "tq doctor"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := SelfProtection(s, cwd, c.call)
			got := ""
			if d.Action == Deny {
				got = d.Matched[0].ID
				if d.Matched[0].Category != CategorySelfProtection || !strings.HasPrefix(d.Reason(), "BLOCKED [self-protection/"+got+"]: ") {
					t.Fatalf("category: %q", d.Reason())
				}
			}
			if got != c.want {
				t.Fatalf("got %q want %q (%s)", got, c.want, d.Reason())
			}
		})
	}
	if d := SelfProtection(nil, cwd, ToolCall{Tool: "Write", Path: manifest}); d.Action != Allow {
		t.Fatal("no set, no rule")
	}
	// A Grep with no path searches the cwd: from inside the state folder
	// that is a read of the state.
	if d := SelfProtection(s, state, ToolCall{Tool: "Grep"}); d.Action != Deny {
		t.Fatal("a Grep with no path from the state folder reads it")
	}
	// findstr's /s and /i are switches, not roots; its relative root "."
	// run from the home folder is a search of home, which reads the state.
	if d := SelfProtection(s, filepath.Dir(state), ToolCall{Tool: "PowerShell", Command: "findstr /s /i x ."}); d.Action != Deny {
		t.Fatalf("findstr /s /i x . from home must deny: %s", d.Reason())
	}
	// A switch-shaped single-segment root ("/home" on Unix, the MSYS drive
	// "/c" on Windows) that resolves to an ancestor of the fixture home is a
	// root, not a switch: it is denied.
	u := unixSpelling(filepath.Dir(state))
	seg := u
	if i := strings.Index(u[1:], "/"); i >= 0 {
		seg = u[:i+1]
	}
	if d := SelfProtection(s, cwd, ToolCall{Tool: "PowerShell", Command: "findstr /s x " + seg}); d.Action != Deny {
		t.Fatalf("findstr /s x %s (an ancestor of home) must deny: %s", seg, d.Reason())
	}
	// A drive root is an ancestor of home. The Grep/Glob tools carry a path
	// verbatim (`/`, `C:\`, `C:/`) and must not read `/` as relative.
	toolRoots := []string{"/"}
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(state)
		toolRoots = append(toolRoots, vol+`\`, vol+"/", `\`)
	}
	for _, r := range toolRoots {
		for _, tool := range []string{"Grep", "Glob"} {
			if d := SelfProtection(s, cwd, ToolCall{Tool: tool, Path: r}); d.Action != Deny {
				t.Errorf("%s path %q: a search of the drive root must deny: %s", tool, r, d.Reason())
			}
		}
	}
	// Shell recursive searches rooted at a drive root (forward-slash spelling,
	// which the tokenizer reads cleanly) deny too.
	shellRoots := []string{"/"}
	if runtime.GOOS == "windows" {
		shellRoots = append(shellRoots, filepath.VolumeName(state)+"/")
	}
	for _, r := range shellRoots {
		for _, cmd := range []string{"grep -r x " + r, "rg x " + r, "findstr /s x " + r, "Get-ChildItem -Recurse " + r} {
			if d := SelfProtection(s, cwd, ToolCall{Tool: "PowerShell", Command: cmd}); d.Action != Deny {
				t.Errorf("%q: a search of the drive root must deny: %s", cmd, d.Reason())
			}
		}
	}
}

// unixSpelling writes an absolute path the way a Unix shell (or Git Bash's
// MSYS on Windows) spells it: forward slashes, a leading "/" and, on Windows,
// the drive as /c/.
func unixSpelling(p string) string {
	p = filepath.ToSlash(p)
	if runtime.GOOS == "windows" && len(p) >= 2 && p[1] == ':' {
		return "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

// Writing or deleting tq's keychain entry (the integrity key, the tq
// secret tokens) is a write to a protected location, on every OS's tool.
func TestKeychainWrite(t *testing.T) {
	deny := map[string]string{
		"cmdkey /generic:tentaqles:integrity /user:tq /pass:x":                 "cmdkey /generic",
		"cmdkey /GENERIC:Tentaqles:integrity /pass:x":                          "cmdkey /generic",
		"cmdkey.exe /delete:tentaqles:integrity":                               "cmdkey /delete",
		`cmdkey /add:"tentaqles:acme/jira" /pass:x`:                            "cmdkey /add",
		`cmd /c cmdkey /delete:tentaqles:integrity`:                            "cmdkey /delete",
		"security add-generic-password -U -s tentaqles -a integrity -w x":      "security add-generic-password",
		"security delete-generic-password -s tentaqles -a integrity":           "security delete-generic-password",
		"security add-generic-password -stentaqles -a integrity -w x":          "security add-generic-password",
		"/usr/bin/security delete-generic-password -a x -s 'tentaqles'":        "security delete-generic-password",
		"secret-tool store --label=tq service tentaqles username integrity":    "secret-tool store",
		"secret-tool clear service tentaqles username integrity":               "secret-tool clear",
		"echo x | secret-tool store --label=tq Service tentaqles username x":   "secret-tool store",
		`bash -c "security delete-generic-password -s tentaqles -a integrity"`: "security delete-generic-password",
	}
	for cmd, want := range deny {
		if got, ok := KeychainWrite(cmd); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", cmd, got, ok, want)
		}
		d := SelfProtection(protect.New(protect.Inputs{}), "", ToolCall{Tool: "Bash", Command: cmd})
		if d.Action != Deny || d.Matched[0].ID != ProtectedPathID || !strings.Contains(d.Reason(), "OS keychain") {
			t.Errorf("%q: %s", cmd, d.Reason())
		}
	}
	for _, cmd := range []string{
		"cmdkey /list", "cmdkey /list:tentaqles*", "cmdkey /generic:github.com /user:x /pass:y",
		"security find-generic-password -s tentaqles -a integrity", "security add-generic-password -s other -a x -w y",
		"security add-generic-password -a tentaqles -s other -w y",
		"secret-tool lookup service tentaqles username integrity", "secret-tool store --label=x service other username tentaqles",
		"echo cmdkey /delete:tentaqles:x",
	} {
		if got, ok := KeychainWrite(cmd); ok {
			t.Errorf("%q: denied as %q", cmd, got)
		}
	}
}

// A PowerShell -EncodedCommand is decoded (base64 UTF-16LE) and the rule is
// re-run on the script; an undecodable value is denied with its own reason.
func TestEncodedPowerShell(t *testing.T) {
	s, _, cwd := selfProtectionSet(t)
	enc := func(ps string) string {
		u := utf16.Encode([]rune(ps))
		b := make([]byte, len(u)*2)
		for i, r := range u {
			b[2*i] = byte(r)
			b[2*i+1] = byte(r >> 8)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	for _, flag := range []string{"-EncodedCommand", "-enco", "-enc", "-en", "-e", "-ec", "-ENC"} {
		cmd := "powershell " + flag + " " + enc("Remove-Item ~/.tentaqles/config.yaml")
		if d := SelfProtection(s, cwd, ToolCall{Tool: "PowerShell", Command: cmd}); d.Action != Deny || d.Matched[0].ID != ProtectedPathID {
			t.Errorf("%s: encoded protected write must deny: %s", flag, d.Reason())
		}
	}
	// An encoded command that targets nothing protected is allowed.
	ok := "powershell -enc " + enc("Get-Date")
	if d := SelfProtection(s, cwd, ToolCall{Tool: "PowerShell", Command: ok}); d.Action != Allow {
		t.Errorf("a harmless encoded command is allowed: %s", d.Reason())
	}
	// A value that is not valid base64 UTF-16LE is denied, not skipped.
	bad := "powershell -EncodedCommand @@@notbase64@@@"
	d := SelfProtection(s, cwd, ToolCall{Tool: "PowerShell", Command: bad})
	if d.Action != Deny || !strings.Contains(d.Reason(), "could not be read") {
		t.Fatalf("an undecodable encoded command must deny: %s", d.Reason())
	}
}

func TestHumanOnlyInvocation(t *testing.T) {
	deny := map[string]string{
		"tq init C:/work":                      "tq init",
		"tq add beta --base x":                 "tq add",
		"tq edit acme":                         "tq edit",
		"tq off":                               "tq off",
		"tq allow acme":                        "tq allow",
		"tq deny acme":                         "tq deny",
		"tq trust acme":                        "tq trust",
		"tq hooks install bash":                "tq hooks install",
		"tq hooks remove":                      "tq hooks remove",
		"tq hooks verify --accept":             "tq hooks verify --accept",
		"tq settings render acme":              "tq settings render",
		"tq settings anchors add foo":          "tq settings anchors add",
		"tq settings anchors remove foo":       "tq settings anchors remove",
		"tq bundle sync acme":                  "tq bundle sync",
		"tq bundle capture --dir x":            "tq bundle capture",
		"tq migrate --apply":                   "tq migrate --apply",
		"tq migrate --steps identity --apply":  "tq migrate --apply",
		"tq migrate manifests --apply":         "tq migrate manifests --apply",
		"tq uninstall --restore latest":        "tq uninstall",
		"tq setup --from plan.yaml":            "tq setup",
		"tq onboard acme":                      "tq onboard",
		"tq secret set acme jira":              "tq secret set",
		"tq secret rm acme jira":               "tq secret rm",
		"tq providers add widget":              "tq providers add",
		"tq login acme gh":                     "tq login",
		"tq run --off -- claude":               "tq run --off",
		"tq worktrees prune":                   "tq worktrees prune",
		"tq.exe allow acme":                    "tq allow",
		`C:\tools\tq.exe hooks install`:        "tq hooks install",
		"./tq init":                            "tq init",
		"/usr/local/bin/tq allow x":            "tq allow",
		`"C:\Program Files\tq\tq.exe" allow x`: "tq allow",
		"go run ./cmd/tq allow acme":           "tq allow",
		"go run github.com/tentaqles/tentaqles/cmd/tq deny acme": "tq deny",
		"sudo tq allow acme":             "tq allow",
		"env X=1 tq allow acme":          "tq allow",
		"TQ_HOME=/x tq allow acme":       "tq allow",
		`bash -c "tq allow acme"`:        "tq allow",
		"cmd /c tq allow acme":           "tq allow",
		"go test ./... && tq allow acme": "tq allow",
		"ls; tq deny acme":               "tq deny",
		"xargs tq allow":                 "tq allow",
	}
	for cmd, want := range deny {
		if got, ok := HumanOnlyInvocation(cmd); !ok || got != want {
			t.Errorf("%q: got %q %v, want %q", cmd, got, ok, want)
		}
	}
	allow := []string{
		"tq doctor", "tq list", "tq env", "tq activate bash", "tq version", "tq status",
		"tq migrate", "tq migrate manifests", "tq migrate --steps identity",
		"tq settings render --dry-run acme", "tq settings render acme --dry-run", "tq settings anchors",
		"tq hooks status", "tq hooks verify",
		"tq secret status acme jira", "tq secrets audit",
		"tq bundle diff acme", "tq bundle catalog",
		"tq providers list", "tq providers show gh",
		"tq run acme -- claude", "tq worktrees list", "tq dotenv run -- npm test",
		"tq claude-hook pre-tool-use", "tq allow --help", "tq -h",
		"echo tq allow acme", "git commit -m 'tq allow'", "go run ./cmd/other allow", "tqx allow", "cat tq",
	}
	for _, cmd := range allow {
		if got, ok := HumanOnlyInvocation(cmd); ok {
			t.Errorf("%q: denied as %q", cmd, got)
		}
	}
}

// TestHumanOnlyInvocation_TentaqlesName: the hook treats tentaqles as it treats tq, in every
// spelling, and reach is human-only under both names (ADR-0012).
func TestHumanOnlyInvocation_TentaqlesName(t *testing.T) {
	for _, bin := range []string{"tq", "tq.exe", "tentaqles", "tentaqles.exe", `C:\tools\tentaqles.exe`, "/usr/local/bin/tentaqles", "go run ./cmd/tentaqles"} {
		for _, verb := range []string{"reach", "reach C:/clients/acme", "allow acme", "deny acme", "login acme gh", "uninstall"} {
			if _, ok := HumanOnlyInvocation(bin + " " + verb); !ok {
				t.Errorf("%s %s passed the hook", bin, verb)
			}
		}
	}
	for _, free := range []string{"tentaqles status", "tentaqles version", "tentaqles reach --help", "echo tentaqles reach", "grep 'tentaqles reach' README.md"} {
		if what, ok := HumanOnlyInvocation(free); ok {
			t.Errorf("%q denied as %s", free, what)
		}
	}
}
