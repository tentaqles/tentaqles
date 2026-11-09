package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// sandboxRunGit runs git for the sandbox; tests may replace it.
var sandboxRunGit = gitcfg.RunGit

// sandboxBuild builds the standard pair of test clients, acme and globex, in a throwaway home
// under folder. It reads the process's own environment and refuses, before writing anything,
// unless TQ_HOME, HOME and USERPROFILE all point inside folder.
func sandboxBuild(folder string) error {
	abs, err := filepath.Abs(folder)
	if err != nil {
		return err
	}
	nf, err := registry.Normalize(abs)
	if err != nil {
		return err
	}
	var tqHome string
	for _, name := range []string{"TQ_HOME", "HOME", "USERPROFILE"} {
		n, err := sandboxInside(nf, name)
		if err != nil {
			return err
		}
		if name == "TQ_HOME" {
			tqHome = n
		}
	}
	for _, name := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM"} {
		if _, ok := os.LookupEnv(name); ok {
			return fmt.Errorf("%s is set: unset it, the sandbox writes git's global config under HOME", name)
		}
	}
	if err := sandboxFresh(tqHome); err != nil {
		return err
	}
	clients := filepath.Join(nf, "clients")
	if err := sandboxFresh(clients); err != nil {
		return err
	}
	if err := os.MkdirAll(clients, 0o755); err != nil {
		return err
	}
	for _, name := range []string{"acme", "globex"} {
		if err := sandboxClient(clients, name); err != nil {
			return fmt.Errorf("build %s: %w", name, err)
		}
	}
	return nil
}

// sandboxInside reads one home variable and requires it to point strictly inside nf. It returns the
// normalized value.
func sandboxInside(nf, name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", fmt.Errorf("%s is not set: point TQ_HOME, HOME and USERPROFILE inside the folder first", name)
	}
	n, err := registry.Normalize(v)
	if err != nil {
		return "", err
	}
	prefix := nf
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	p, pre := n, prefix
	if runtime.GOOS == "windows" {
		p, pre = strings.ToLower(p), strings.ToLower(pre)
	}
	if !strings.HasPrefix(p, pre) {
		return "", fmt.Errorf("%s (%s) is not inside %s: a sandbox never touches a real home", name, v, nf)
	}
	return n, nil
}

// sandboxFresh requires dir to be absent or an empty directory.
func sandboxFresh(dir string) error {
	st, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not empty: the sandbox builds only a fresh home", dir)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(ents) > 0 {
		return fmt.Errorf("%s is not empty: the sandbox builds only a fresh home", dir)
	}
	return nil
}

// sandboxClient builds one test client under clients: acme also has the Codex agent. Its manifest
// ends with the lobe mapping.
func sandboxClient(clients, name string) error {
	a := reach.Answers{Email: "dev@" + name + ".test"}
	if name == "acme" {
		a.Agent = []string{"codex"}
	}
	o, err := a.AddOptions(clients, name, reach.GitNameFor("", a.Email))
	if err != nil {
		return err
	}
	o.RunGit = sandboxRunGit
	ws, err := workspace.Add(o)
	if err != nil {
		return err
	}
	var out bytes.Buffer
	rc := &cobra.Command{}
	rc.SetOut(&out)
	rc.SetErr(&out)
	renderIdentityFiles(rc, ws)
	for _, l := range strings.Split(out.String(), "\n") {
		if strings.Contains(l, "warning:") {
			return fmt.Errorf("render %s: %s", name, strings.TrimSpace(l))
		}
	}
	raw, err := os.ReadFile(ws.ManifestPath)
	if err != nil {
		return err
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		raw = append(raw, '\n')
	}
	raw = append(raw, "lobe:\n  model: off\n"...)
	if err := writeFileAtomic(ws.ManifestPath, raw, 0o600); err != nil {
		return err
	}
	_, err = manifest.Load(ws.ManifestPath)
	return err
}

func newLobeSandboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sandbox <folder>",
		Args:  cobra.ExactArgs(1),
		Short: "Build a throwaway home with two test clients, acme and globex",
		Long: `Builds a throwaway tentaqles home with two test clients, acme and globex, under <folder>/clients.
Test agents, benchmarks and CI use it to get clients without a person answering the reach form.

acme also has the Codex agent. Each client's manifest ends with a lobe: mapping that holds model: off.

The command refuses before it writes anything. It refuses unless TQ_HOME, HOME and USERPROFILE all point
inside <folder>, unless GIT_CONFIG_GLOBAL and GIT_CONFIG_SYSTEM are unset, and unless the home and
<folder>/clients are empty or absent. It never touches a real home. It installs no plugin and writes no
shell profile. It also runs inside a Claude Code session.

It prints ok acme globex. On a refusal or a failure it prints one line that starts with SETUP FAIL and
exits with status 1.`,
		RunE: func(c *cobra.Command, args []string) error {
			if err := sandboxBuild(args[0]); err != nil {
				fmt.Fprintf(c.OutOrStdout(), "SETUP FAIL %v\n", err)
				return err
			}
			fmt.Fprintln(c.OutOrStdout(), "ok acme globex")
			return nil
		},
	}
}
