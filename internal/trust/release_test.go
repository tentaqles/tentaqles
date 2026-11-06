package trust

import (
	"errors"
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

// setReleaseCommit makes sha the commit this tq was built from, for the test.
func setReleaseCommit(t *testing.T, sha string) {
	t.Helper()
	prev := ReleaseCommit
	ReleaseCommit = sha
	t.Cleanup(func() { ReleaseCommit = prev })
}

// commitFiles writes files under plugin/ of clone and commits them; it
// returns the new commit.
func commitFiles(t *testing.T, clone string, files map[string]string, msg string) string {
	t.Helper()
	for rel, body := range files {
		write(t, filepath.Join(clone, "plugin", filepath.FromSlash(rel)), body)
	}
	gitFixture(t, clone, "add", "-A")
	gitFixture(t, clone, "commit", "-q", "-m", msg)
	return strings.TrimSpace(gitFixture(t, clone, "rev-parse", "HEAD"))
}

// publish writes the tentaqles marketplace clone of claudeDir: one commit
// holding files under plugin/ (with the pin of .claude-plugin/
// marketplace.json, which tq never reads), and makes that commit the one
// this tq was built from. It returns the sha.
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
	sha := commitFiles(t, clone, files, "release")
	pin(t, clone, sha)
	setReleaseCommit(t, sha)
	return sha
}

// pin writes the clone's marketplace.json pinning sha (tq ignores it).
func pin(t *testing.T, clone, sha string) {
	t.Helper()
	write(t, filepath.Join(clone, ".claude-plugin", "marketplace.json"),
		`{"name":"tentaqles","plugins":[{"name":"tentaqles","source":{"source":"git-subdir","url":"https://github.com/tentaqles/tentaqles.git","path":"plugin","ref":"v0.9.1","sha":"`+sha+`"}}]}`)
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

// The critic's case: the clone re-pins itself. It holds tq's release commit,
// then a commit it forged (an emptied hooks.json, a planted bin/tq), and its
// marketplace.json pins the forged one, which the cache holds. tq compares
// with the commit it was built from, never with the pin: refused.
func TestARepinnedCloneIsRefused(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	sha := publish(t, f.claude, releaseFiles())
	clone := MarketplaceClone(f.claude)
	forged := releaseFiles()
	forged["hooks/hooks.json"] = `{"hooks":{}}`
	forged["bin/tq"] = "#!/bin/sh\nexit 0\n"
	forgedSHA := commitFiles(t, clone, forged, "forged")
	pin(t, clone, forgedSHA)
	install(t, f, forged)
	rep := f.verify()
	rel, err := CheckRelease("acme", rep)
	if err != nil || rel.SHA != sha {
		t.Fatalf("compared with tq's commit %s: %+v %v", sha[:12], rel, err)
	}
	want := []string{"bin/tq: not in the release", "hooks/hooks.json: differs from the release"}
	if strings.Join(rel.Mismatches, "|") != strings.Join(want, "|") {
		t.Fatalf("the forged folder is not tq's release: %v", rel.Mismatches)
	}
	// Even with tq's commit gone from the clone's history, the forged pin
	// is no substitute.
	setReleaseCommit(t, strings.Repeat("1", 40))
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "the clone does not have tq's release") {
		t.Fatalf("%v", err)
	}
}

// A clone whose repository could make tq's commit id stand for other content
// is refused: a replace ref pointing tq's release at a tampered commit (git
// would otherwise read the tampered tree for tq's id), a graft, borrowed
// objects, a shallow clone, a packed replace ref.
func TestAnAlteredCloneIsRefused(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	sha := publish(t, f.claude, releaseFiles())
	clone := MarketplaceClone(f.claude)
	tampered := releaseFiles()
	tampered["hooks/hooks.json"] = `{"hooks":{}}`
	tamperedSHA := commitFiles(t, clone, tampered, "tampered")
	install(t, f, tampered)
	rep := f.verify()

	// The replace ref: plain git now shows the tampered tree for tq's id.
	gitFixture(t, clone, "replace", sha, tamperedSHA)
	if got := gitFixture(t, clone, "show", sha+":plugin/hooks/hooks.json"); got != `{"hooks":{}}` {
		t.Fatalf("precondition: the replace ref redirects tq's release, got %q", got)
	}
	_, err := CheckRelease("acme", rep)
	if err == nil || !strings.Contains(err.Error(), "the clone is altered: refs/replace/"+sha) {
		t.Fatalf("a replace ref: %v", err)
	}
	gitFixture(t, clone, "replace", "-d", sha)
	gitDir := filepath.Join(clone, ".git")
	if rel, err := CheckRelease("acme", rep); err != nil || len(rel.Mismatches) != 1 || rel.Mismatches[0] != "hooks/hooks.json: differs from the release" {
		t.Fatalf("unaltered again, compared with tq's own tree: %+v %v", rel, err)
	}

	for _, c := range []struct{ file, body, what string }{
		{"info/grafts", sha + " " + tamperedSHA + "\n", "info/grafts"},
		{"objects/info/alternates", filepath.ToSlash(t.TempDir()) + "\n", "objects/info/alternates"},
		{"shallow", sha + "\n", "shallow"},
	} {
		p := filepath.Join(gitDir, filepath.FromSlash(c.file))
		write(t, p, c.body)
		if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "the clone is altered: "+c.what) {
			t.Fatalf("%s: %v", c.file, err)
		}
		os.Remove(p)
	}

	// A replace ref that only lives in packed-refs.
	gitFixture(t, clone, "replace", sha, tamperedSHA)
	gitFixture(t, clone, "pack-refs", "--all")
	if _, err := os.Stat(filepath.Join(gitDir, "refs", "replace", sha)); err == nil {
		t.Fatal("precondition: the replace ref is packed")
	}
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "the clone is altered: refs/replace/"+sha) {
		t.Fatalf("a packed replace ref: %v", err)
	}
}

// A plugin release newer than tq is not the commit tq was built from: the
// batch refuses it until tq is upgraded.
func TestAPluginReleaseNewerThanTqIsRefused(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	publish(t, f.claude, releaseFiles())
	clone := MarketplaceClone(f.claude)
	newer := releaseFiles()
	newer[".claude-plugin/plugin.json"] = `{"name":"tentaqles","version":"0.18.0"}`
	newerSHA := commitFiles(t, clone, newer, "a later plugin release")
	pin(t, clone, newerSHA)
	install(t, f, newer)
	rel, err := CheckRelease("acme", f.verify())
	if err != nil || len(rel.Mismatches) != 1 || rel.Mismatches[0] != ".claude-plugin/plugin.json: differs from the release" {
		t.Fatalf("%+v %v", rel, err)
	}
	setReleaseCommit(t, newerSHA) // the tq built from the later release
	if rel, err := CheckRelease("acme", f.verify()); err != nil || len(rel.Mismatches) != 0 {
		t.Fatalf("an upgraded tq accepts it: %+v %v", rel, err)
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
	t.Setenv("GIT_REPLACE_REF_BASE", "refs/evil/")
	t.Setenv("GIT_GRAFT_FILE", "/x/grafts")
	clone := filepath.Join(t.TempDir(), "clone")
	c := gitCommand(t.Context(), clone, "ls-tree", "HEAD")
	args := strings.Join(c.Args[1:], " ")
	if want := "--no-replace-objects -c core.fsmonitor= -c safe.directory=" + filepath.ToSlash(clone) + " -C " + clone + " ls-tree HEAD"; args != want {
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
	for _, k := range []string{"GIT_REPLACE_REF_BASE", "GIT_GRAFT_FILE"} {
		if _, ok := set[k]; ok {
			t.Errorf("%s must be dropped", k)
		}
	}
	if set["GIT_CONFIG_NOSYSTEM"] != "1" || set["GIT_CONFIG_GLOBAL"] != os.DevNull || set["GIT_TERMINAL_PROMPT"] != "0" || set["GIT_NO_REPLACE_OBJECTS"] != "1" {
		t.Fatalf("%v", set)
	}
}

// No release to compare with is an error, never a match: a tq built without
// a commit, no clone, a clone without tq's commit, a release commit that is
// not a commit id, a change that is not a plugin bump alone.
func TestCheckReleaseFailsClosed(t *testing.T) {
	f := newFixture(t, "acme")
	f.allow()
	install(t, f, releaseFiles())
	rep := f.verify()
	setReleaseCommit(t, strings.Repeat("ab", 20))
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "no tentaqles marketplace clone") {
		t.Fatalf("no clone: %v", err)
	}
	sha := publish(t, f.claude, releaseFiles())
	setReleaseCommit(t, "")
	if _, err := CheckRelease("acme", rep); !errors.Is(err, ErrNoReleaseCommit) || !strings.Contains(err.Error(), "this tq build has no release commit") {
		t.Fatalf("a dev build: %v", err)
	}
	other := strings.Repeat("0", 40)
	setReleaseCommit(t, other)
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "the clone does not have tq's release "+other[:12]) {
		t.Fatalf("a clone without tq's commit: %v", err)
	}
	setReleaseCommit(t, "v0.9.1")
	if _, err := CheckRelease("acme", rep); err == nil || !strings.Contains(err.Error(), "is not a commit id") {
		t.Fatalf("a tag is not a commit id: %v", err)
	}
	setReleaseCommit(t, strings.ToUpper(sha))
	if rel, err := CheckRelease("acme", rep); err != nil || len(rel.Mismatches) != 0 || rel.SHA != sha {
		t.Fatalf("tq's own commit, in any case: %+v %v", rel, err)
	}
	write(t, f.settings, `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[]}]}}`)
	invalidate()
	if rep := f.verify(); rep.PluginBump {
		t.Fatalf("a settings change beside the bump: %+v", rep)
	} else if _, err := CheckRelease("acme", rep); err == nil {
		t.Fatal("not a bump alone: no release check")
	}
}
