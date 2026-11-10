package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/lobe/store"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// agentVerbs are the three read verbs of F-07 that the agent tests run, each with its own arguments.
// Later verbs join this list as their features add them.
var agentVerbs = [][]string{{"lobe", "status"}, {"lobe", "events"}, {"lobe", "settings"}}

// agentTwoClients makes acme and globex under a fresh home, each with its store, and one event line in
// each store. It returns the base folder; the neutral folder is base/neutral.
func agentTwoClients(t *testing.T) string {
	t.Helper()
	base := newBase(t)
	for _, name := range []string{"acme", "globex"} {
		writeClient(t, base, name, healthBody(name))
		if _, err := store.Open(name, store.ReadWrite); err != nil {
			t.Fatal(err)
		}
		writeTB(t, filepath.Join(paths.LobeDir(name), "events", "events-20261109.jsonl"),
			`{"v":1,"id":"evt_`+name[:1]+`1","kind":"fail"}`+"\n")
	}
	writeTB(t, filepath.Join(base, "neutral", "keep.txt"), "not a client\n")
	return base
}

// assertNoStoreOutput fails when out holds anything the store of client would print: its event lines, its
// status or settings text, or its JSON status.
func assertNoStoreOutput(t *testing.T, out, client string) {
	t.Helper()
	for _, s := range []string{"evt_" + client[:1] + "1", "client: " + client, `"client":"` + client + `"`, "capture="} {
		if strings.Contains(out, s) {
			t.Fatalf("refused command printed the store of %s (%q):\n%s", client, s, out)
		}
	}
}

// TestLobeAgentCannotReadAnotherClient: inside a Claude Code session the three read verbs refuse another
// client, from the client's own folder and from a neutral folder, and print nothing from the store.
func TestLobeAgentCannotReadAnotherClient(t *testing.T) {
	base := agentTwoClients(t)
	t.Setenv("CLAUDECODE", "1")

	t.Run("from acme's folder with --client globex", func(t *testing.T) {
		chdir(t, filepath.Join(base, "acme"))
		refused := 0
		for _, verb := range agentVerbs {
			out, err := runLobe(t, "", append(verb, "--client", "globex")...)
			if err == nil {
				t.Errorf("%v --client globex ran inside acme's session:\n%s", verb, out)
				continue
			}
			assertNoStoreOutput(t, out, "globex")
			refused++
		}
		if refused != 3 {
			t.Fatalf("refused %d of 3", refused)
		}
	})

	t.Run("from a neutral folder with --client acme", func(t *testing.T) {
		chdir(t, filepath.Join(base, "neutral"))
		refused := 0
		for _, verb := range agentVerbs {
			out, err := runLobe(t, "", append(verb, "--client", "acme")...)
			if err == nil {
				t.Errorf("%v --client acme ran from a neutral folder:\n%s", verb, out)
				continue
			}
			assertNoStoreOutput(t, out, "acme")
			refused++
		}
		if refused != 3 {
			t.Fatalf("refused %d of 3", refused)
		}
	})

	t.Run("from acme's folder with --client as globex's folder path", func(t *testing.T) {
		chdir(t, filepath.Join(base, "acme"))
		for _, verb := range agentVerbs {
			out, err := runLobe(t, "", append(verb, "--client", filepath.Join(base, "globex"))...)
			if err == nil {
				t.Errorf("%v --client <globex's folder> ran inside acme's session:\n%s", verb, out)
				continue
			}
			assertNoStoreOutput(t, out, "globex")
		}
	})

	t.Run("from acme's folder with --client as a subfolder of globex", func(t *testing.T) {
		sub := filepath.Join(base, "globex", "app")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		chdir(t, filepath.Join(base, "acme"))
		for _, value := range []string{sub, filepath.Join("..", "globex", "app"), "globex" + string(filepath.Separator) + "app"} {
			for _, verb := range agentVerbs {
				out, err := runLobe(t, "", append(verb, "--client", value)...)
				if err == nil {
					t.Errorf("%v --client %s ran inside acme's session:\n%s", verb, value, out)
					continue
				}
				assertNoStoreOutput(t, out, "globex")
			}
		}
	})

	t.Run("control: --client names the folder's own client", func(t *testing.T) {
		chdir(t, filepath.Join(base, "acme"))
		out, err := runLobe(t, "", "lobe", "events", "--client", "acme")
		if err != nil {
			t.Fatalf("own client refused: %v\n%s", err, out)
		}
		if !strings.Contains(out, "evt_a1") {
			t.Fatalf("own client's events not printed:\n%s", out)
		}
	})
}

// TestLobeCommandRefusesAnotherLogin: inside acme's folder under a Claude Code session, a login variable
// that names globex's folder makes the three read verbs exit 1 and print nothing from the store. The same
// holds for CODEX_HOME. With both variables unset the verbs work.
func TestLobeCommandRefusesAnotherLogin(t *testing.T) {
	base := agentTwoClients(t)
	t.Setenv("CLAUDECODE", "1")
	acme := filepath.Join(base, "acme")

	for _, login := range []struct{ env, dir string }{
		{"CLAUDE_CONFIG_DIR", paths.IdentityDir("globex", "claude")},
		{"CODEX_HOME", paths.IdentityDir("globex", "codex")},
	} {
		t.Run(login.env+" names globex's folder", func(t *testing.T) {
			unsetenv(t, "CLAUDE_CONFIG_DIR")
			unsetenv(t, "CODEX_HOME")
			t.Setenv(login.env, login.dir)
			chdir(t, acme)
			refused := 0
			for _, verb := range agentVerbs {
				out, err := runLobe(t, "", verb...)
				if err == nil {
					t.Errorf("%v with %s naming globex ran:\n%s", verb, login.env, out)
					continue
				}
				assertNoStoreOutput(t, out, "globex")
				assertNoStoreOutput(t, out, "acme")
				refused++
			}
			if refused != 3 {
				t.Fatalf("refused %d of 3", refused)
			}
		})
	}

	// The login folder written with a trailing separator, and (on Windows) in another case, names the same
	// folder: globex's is still refused, and acme's own is still allowed.
	trailing := string(filepath.Separator)
	for _, c := range []struct {
		name, value string
		refused     bool
		windowsOnly bool // the case fold of a Windows path: the same folder only on Windows
	}{
		{"globex's login with a trailing separator", paths.IdentityDir("globex", "claude") + trailing, true, false},
		{"globex's login with a trailing slash", filepath.ToSlash(paths.IdentityDir("globex", "claude")) + "/", true, false},
		{"globex's login in upper case with a trailing separator", strings.ToUpper(paths.IdentityDir("globex", "claude")) + trailing, true, false},
		{"acme's own login with a trailing separator", paths.IdentityDir("acme", "claude") + trailing, false, false},
		{"acme's own login in upper case with a trailing separator", strings.ToUpper(paths.IdentityDir("acme", "claude")) + trailing, false, true},
	} {
		t.Run("CLAUDE_CONFIG_DIR "+c.name, func(t *testing.T) {
			if c.windowsOnly && runtime.GOOS != "windows" {
				t.Skip("the case of a path is the same folder on Windows only")
			}
			unsetenv(t, "CODEX_HOME")
			t.Setenv("CLAUDE_CONFIG_DIR", c.value)
			chdir(t, acme)
			for _, verb := range agentVerbs {
				out, err := runLobe(t, "", verb...)
				if c.refused {
					if err == nil {
						t.Errorf("%v with CLAUDE_CONFIG_DIR=%s ran:\n%s", verb, c.value, out)
						continue
					}
					assertNoStoreOutput(t, out, "globex")
					assertNoStoreOutput(t, out, "acme")
					continue
				}
				if err != nil {
					t.Errorf("%v with CLAUDE_CONFIG_DIR=%s refused: %v\n%s", verb, c.value, err, out)
				}
			}
		})
	}

	t.Run("both unset", func(t *testing.T) {
		unsetenv(t, "CLAUDE_CONFIG_DIR")
		unsetenv(t, "CODEX_HOME")
		chdir(t, acme)
		for _, verb := range agentVerbs {
			if out, err := runLobe(t, "", verb...); err != nil {
				t.Errorf("%v with both logins unset: %v\n%s", verb, err, out)
			}
		}
	})
}
