package trust

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitFixture runs git in dir for a test fixture, with neither the system nor
// the user's git config (so no line-ending conversion) and a fixed author.
func gitFixture(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.com")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// releaseFiles is the plugin folder of the published release 0.17.0.
func releaseFiles() map[string]string {
	return map[string]string{
		".claude-plugin/plugin.json": `{"name":"tentaqles","version":"0.17.0"}`,
		"hooks/hooks.json":           `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"bash tq_hook.sh pre-tool-use"}]}]}}`,
		"scripts/tq_hook.sh":         "#!/usr/bin/env bash\nexec tq claude-hook \"$1\"\n",
		"skills/x/SKILL.md":          "a skill\n",
	}
}

// publish writes the tentaqles marketplace clone of claudeDir: one commit
// holding files under plugin/, pinned by .claude-plugin/marketplace.json.
// It returns the sha.
func publish(t *testing.T, claudeDir string, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("needs git")
	}
	clone := MarketplaceClone(claudeDir)
	if err := os.MkdirAll(clone, 0o700); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, clone, "init", "-q")
	gitFixture(t, clone, "remote", "add", "origin", "https://github.com/tentaqles/tentaqles.git")
	for rel, body := range files {
		write(t, filepath.Join(clone, "plugin", filepath.FromSlash(rel)), body)
	}
	gitFixture(t, clone, "add", "-A")
	gitFixture(t, clone, "commit", "-q", "-m", "release")
	sha := strings.TrimSpace(gitFixture(t, clone, "rev-parse", "HEAD"))
	write(t, filepath.Join(clone, ".claude-plugin", "marketplace.json"),
		`{"name":"tentaqles","plugins":[{"name":"tentaqles","source":{"source":"git-subdir","url":"https://github.com/tentaqles/tentaqles.git","path":"plugin","ref":"v0.9.1","sha":"`+sha+`"}}]}`)
	return sha
}

// install puts files as the cached plugin folder 0.17.0 of f's identity.
func install(t *testing.T, f *fixture, files map[string]string) string {
	t.Helper()
	next := filepath.Join(f.claude, "plugins", "cache", "tentaqles", "tentaqles", "0.17.0")
	for rel, body := range files {
		write(t, filepath.Join(next, filepath.FromSlash(rel)), body)
	}
	return next
}

// A new plugin folder is the release only when every covered file has the
// pinned commit's blob, byte for byte, and no covered file of the release is
// missing. Location says nothing.
func TestCheckReleaseComparesBytesNotLocation(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	sha := publish(t, f.claude, releaseFiles())
	next := install(t, f, releaseFiles())
	check := func() (Release, error) {
		t.Helper()
		invalidate()
		rep := f.verify()
		if !rep.PluginBump || rep.PluginTo != "0.17.0" {
			t.Fatalf("a bump: %+v", rep)
		}
		return CheckRelease("acme", rep)
	}
	rel, err := check()
	if err != nil || len(rel.Mismatches) != 0 || rel.SHA != sha || rel.Version != "0.17.0" {
		t.Fatalf("the release, byte for byte: %+v %v", rel, err)
	}

	cases := []struct {
		name, want string
		edit       func()
		undo       func()
	}{
		{"an emptied hooks.json", "hooks/hooks.json: differs from the release",
			func() { write(t, filepath.Join(next, "hooks", "hooks.json"), `{"hooks":{}}`) },
			func() { write(t, filepath.Join(next, "hooks", "hooks.json"), releaseFiles()["hooks/hooks.json"]) }},
		{"a planted bin/tq", "bin/tq: not in the release",
			func() { write(t, filepath.Join(next, "bin", "tq"), "#!/bin/sh\n") },
			func() { os.RemoveAll(filepath.Join(next, "bin")) }},
		{"a release file deleted", "hooks/hooks.json: in the release but missing",
			func() { os.Remove(filepath.Join(next, "hooks", "hooks.json")) },
			func() { write(t, filepath.Join(next, "hooks", "hooks.json"), releaseFiles()["hooks/hooks.json"]) }},
		{"line endings changed", "scripts/tq_hook.sh: differs from the release",
			func() {
				write(t, filepath.Join(next, "scripts", "tq_hook.sh"), strings.ReplaceAll(releaseFiles()["scripts/tq_hook.sh"], "\n", "\r\n"))
			},
			func() { write(t, filepath.Join(next, "scripts", "tq_hook.sh"), releaseFiles()["scripts/tq_hook.sh"]) }},
	}
	for _, c := range cases {
		c.edit()
		rel, err := check()
		if err != nil || len(rel.Mismatches) != 1 || rel.Mismatches[0] != c.want {
			t.Fatalf("%s: want %q, got %+v %v", c.name, c.want, rel, err)
		}
		c.undo()
	}
	// A skill is not covered: editing one does not stop the match.
	write(t, filepath.Join(next, "skills", "x", "SKILL.md"), "edited\n")
	if rel, err := check(); err != nil || len(rel.Mismatches) != 0 {
		t.Fatalf("skills are not compared: %+v %v", rel, err)
	}
}

// A clone whose origin is not the tentaqles repository is no release, and
// git runs against the clone whatever the environment says: GIT_DIR,
// GIT_CONFIG_* and the like are dropped.
func TestCheckReleaseChecksTheOriginAndIgnoresGitEnv(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	publish(t, f.claude, releaseFiles())
	install(t, f, releaseFiles())
	rep := f.verify()
	clone := MarketplaceClone(f.claude)

	// A hostile environment: another repository, and config through the env.
	other := t.TempDir()
	gitFixture(t, other, "init", "-q")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "remote.origin.url")
	t.Setenv("GIT_CONFIG_VALUE_0", "https://github.com/tentaqles/tentaqles.git")
	if rel, err := CheckRelease("acme", rep); err != nil || len(rel.Mismatches) != 0 {
		t.Fatalf("git runs against the clone alone: %+v %v", rel, err)
	}

	for url, ok := range map[string]bool{
		"git@github.com:tentaqles/tentaqles.git":      true,
		"https://github.com/tentaqles/tentaqles":      true,
		"https://github.com/evil/tentaqles.git":       false,
		"https://github.com/tentaqles/tentaqles.gitx": false,
		"file:///tmp/tentaqles":                       false,
	} {
		os.Unsetenv("GIT_DIR")
		gitFixture(t, clone, "remote", "set-url", "origin", url)
		t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
		_, err := CheckRelease("acme", rep)
		if ok && err != nil {
			t.Fatalf("%s is the tentaqles repository: %v", url, err)
		}
		if !ok && (err == nil || !strings.Contains(err.Error(), "not github.com/tentaqles/tentaqles")) {
			t.Fatalf("%s must be refused: %v", url, err)
		}
	}
	os.Unsetenv("GIT_DIR")
	gitFixture(t, clone, "remote", "remove", "origin")
	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "no origin") {
		t.Fatalf("no origin: %v", err)
	}
}

// Every git call has the cleaned environment and the two overrides.
func TestGitCommandIsClean(t *testing.T) {
	for _, kv := range []string{"GIT_DIR=/x", "GIT_WORK_TREE=/x", "GIT_INDEX_FILE=/x", "GIT_OBJECT_DIRECTORY=/x", "GIT_ALTERNATE_OBJECT_DIRECTORIES=/x",
		"GIT_CONFIG=/x", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.fsmonitor", "GIT_CONFIG_VALUE_0=evil", "GIT_CONFIG_PARAMETERS='core.fsmonitor=evil'",
		"GIT_CONFIG_GLOBAL=/x", "GIT_CONFIG_SYSTEM=/x", "GIT_EXEC_PATH=/x"} {
		k, v, _ := strings.Cut(kv, "=")
		t.Setenv(k, v)
	}
	t.Setenv("Git_Dir", "/x") // Windows names are case-insensitive
	clone := filepath.Join(t.TempDir(), "clone")
	c := gitCommand(t.Context(), clone, "ls-tree", "HEAD")
	args := strings.Join(c.Args[1:], " ")
	if want := "-c core.fsmonitor= -c safe.directory=" + filepath.ToSlash(clone) + " -C " + clone + " ls-tree HEAD"; args != want {
		t.Fatalf("args %q, want %q", args, want)
	}
	set := map[string]string{}
	for _, kv := range c.Env {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(strings.ToUpper(k), "GIT_") {
			set[strings.ToUpper(k)] = v
		}
	}
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_CONFIG", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_SYSTEM", "GIT_EXEC_PATH"} {
		if _, ok := set[k]; ok {
			t.Errorf("%s must be dropped", k)
		}
	}
	if set["GIT_CONFIG_NOSYSTEM"] != "1" || set["GIT_CONFIG_GLOBAL"] != os.DevNull || set["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("%v", set)
	}
}

// No release to compare with is an error, never a match: no clone, a pin
// without a sha, a sha the clone does not have, a change that is not a
// plugin bump alone.
func TestCheckReleaseFailsClosed(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	install(t, f, releaseFiles())
	rep := f.verify()
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "no tentaqles marketplace clone") {
		t.Fatalf("no clone: %v", err)
	}
	publish(t, f.claude, releaseFiles())
	mk := filepath.Join(MarketplaceClone(f.claude), ".claude-plugin", "marketplace.json")
	raw, _ := os.ReadFile(mk)
	write(t, mk, `{"plugins":[{"name":"tentaqles","source":{"source":"git-subdir","path":"plugin","ref":"v0.9.1"}}]}`)
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "no commit sha") {
		t.Fatalf("no sha: %v", err)
	}
	write(t, mk, `{"plugins":[{"name":"tentaqles","source":"./plugin"}]}`)
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "does not pin") {
		t.Fatalf("a directory source: %v", err)
	}
	write(t, mk, strings.Replace(string(raw), `"sha":"`, `"sha":"`+strings.Repeat("0", 40)+`","x":"`, 1))
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "is not in the marketplace clone") {
		t.Fatalf("an unknown sha: %v", err)
	}
	write(t, mk, string(raw))
	write(t, f.settings, `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[]}]}}`)
	invalidate()
	if rep := f.verify(); rep.PluginBump {
		t.Fatalf("a settings change beside the bump: %+v", rep)
	} else if _, err := CheckRelease("acme", rep); err == nil {
		t.Fatal("not a bump alone: no release check")
	}
}
