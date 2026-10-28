package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/onboard"
	"github.com/tentaqles/tentaqles/internal/termopen"
	"github.com/tentaqles/tentaqles/internal/testutil"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// isolateOnboard points TQ_HOME, HOME and USERPROFILE at a temp dir, clears the agent markers and
// stubs the git global setup and the terminal launcher.
func isolateOnboard(t *testing.T) (base string, opened *[]string) {
	t.Helper()
	home := testutil.TempDir(t)
	t.Setenv("TQ_HOME", filepath.Join(home, ".tentaqles"))
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"TENTAQLES_HEADLESS", "CLAUDECODE", "CLAUDE_PLUGIN_ROOT", "TQ_HUB_TASK"} {
		t.Setenv(k, "")
	}
	prevGit, prevOpen, prevWiz := onboardRunGit, onboardOpenTerminal, onboardWizard
	onboardRunGit = func(args ...string) (string, error) { return "", nil }
	var dirs []string
	onboardOpenTerminal = func(dir string) (termopen.Launch, error) {
		dirs = append(dirs, dir)
		return termopen.Launch{Name: "wt", Args: []string{"-d", dir}, Dir: dir}, nil
	}
	t.Cleanup(func() { onboardRunGit, onboardOpenTerminal, onboardWizard = prevGit, prevOpen, prevWiz })
	base = filepath.Join(home, "work")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	return base, &dirs
}

func runOnboard(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"onboard"}, args...))
	err := root.Execute()
	return out.String(), err
}

func answersFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "answers.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Spec B §14: tq onboard writes a valid manifest with skipped blocks.
func TestOnboard_FromWritesValidManifestWithSkippedBlocks(t *testing.T) {
	base, opened := isolateOnboard(t)
	from := answersFile(t, "name: acme\ngit: { email: tester@acme.test }\ntracker:\n  provider: jira\n  scope: { org: acme, project: PLAT }\n  auth: env\n  env: ACME_JIRA_TOKEN\nchat: { provider: slack }\n")
	out, err := runOnboard(t, "--from", from, "--base", base)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	mp := filepath.Join(base, "acme", manifest.FileName)
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatalf("the written manifest must load: %v", err)
	}
	if m.Tracker == nil || m.Tracker.Provider != "jira" || m.Tracker.Env != "ACME_JIRA_TOKEN" || m.Tracker.Project() != "PLAT" {
		t.Fatalf("tracker = %+v", m.Tracker)
	}
	if m.Chat == nil || m.Chat.Provider != "slack" {
		t.Fatalf("chat = %+v", m.Chat)
	}
	if m.Email != nil || m.DataPlatform != nil || len(m.Agents) != 0 || len(m.Cloud) != 0 {
		t.Fatalf("skipped blocks were written: email=%v data=%v agents=%v cloud=%v", m.Email, m.DataPlatform, m.Agents, m.Cloud)
	}
	raw, _ := os.ReadFile(mp)
	for _, absent := range []string{"email: null", "data_platform", "agents"} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("manifest should not mention %q:\n%s", absent, raw)
		}
	}
	h, _ := trust.HashFile(mp)
	if trust.IsTrusted(h) {
		t.Fatal("onboard must leave the workspace untrusted: tq allow is the human's")
	}
	for _, want := range []string{"(untrusted)", "tq allow acme", "tq login acme claude", "ACME_JIRA_TOKEN", "slack", "/tentaqles:onboard"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if len(*opened) != 0 {
		t.Fatal("no terminal without --open-terminal")
	}
}

func TestOnboard_FullAnswersAndTerminal(t *testing.T) {
	base, opened := isolateOnboard(t)
	from := answersFile(t, `
name: acme
display_name: Acme
git: { name: Tester, email: tester@acme.test, provider: azure-devops }
cloud: { provider: azure, also: [aws] }
data_platform: { provider: snowflake }
agents: [claude, codex]
tracker: { provider: azure-devops, scope: { org: acme, project: Platform } }
email: { provider: none }
`)
	out, err := runOnboard(t, "--from", from, "--base", base, "--open-terminal")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m, err := manifest.Load(filepath.Join(base, "acme", manifest.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Cloud["provider"] != "azure" {
		t.Fatalf("cloud = %v", m.Cloud)
	}
	ids := strings.Join(m.IdentityNames(), ",")
	if ids != "aws,az,azure-devops,claude,codex,snowflake" {
		t.Fatalf("identities = %s", ids)
	}
	if m.Email == nil || m.Email.Provider != "none" {
		t.Fatalf("an explicit none is kept: %+v", m.Email)
	}
	if len(*opened) != 1 || (*opened)[0] != filepath.Join(base, "acme") {
		t.Fatalf("opened %v", *opened)
	}
	if !strings.Contains(out, "tq never closes a terminal") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestOnboard_NameArgOverridesAndValidationWritesNothing(t *testing.T) {
	base, _ := isolateOnboard(t)
	from := answersFile(t, "name: ignored\ngit: { email: tester@acme.test }\ntracker: { provider: trello }\n")
	if _, err := runOnboard(t, "acme", "--from", from, "--base", base); err == nil {
		t.Fatal("an unknown tracker must fail")
	}
	if _, err := os.Stat(filepath.Join(base, "acme")); !os.IsNotExist(err) {
		t.Fatal("a refused answer set must write nothing")
	}
	ok := answersFile(t, "name: ignored\ngit: { email: tester@acme.test }\n")
	if out, err := runOnboard(t, "acme", "--from", ok, "--base", base); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(base, "acme", manifest.FileName)); err != nil {
		t.Fatal("the name argument wins over the file's name")
	}
}

func TestOnboard_InteractiveRefusedInAgentSession(t *testing.T) {
	base, _ := isolateOnboard(t)
	t.Setenv("CLAUDECODE", "1")
	called := false
	onboardWizard = func(string, string) (onboardWizardResult, error) {
		called = true
		return onboardWizardResult{}, errors.New("no")
	}
	_, err := runOnboard(t, "acme", "--base", base)
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Fatal("the wizard must not start in an agent session")
	}
}

func TestOnboard_AgentSessionNeverOpensATerminal(t *testing.T) {
	base, opened := isolateOnboard(t)
	t.Setenv("TENTAQLES_HEADLESS", "1")
	from := answersFile(t, "name: acme\ngit: { email: tester@acme.test }\n")
	out, err := runOnboard(t, "--from", from, "--base", base, "--open-terminal")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(*opened) != 0 || !strings.Contains(out, "Not opening a terminal") {
		t.Fatalf("opened %v\n%s", *opened, out)
	}
}

func TestOnboard_WizardResultIsWritten(t *testing.T) {
	base, opened := isolateOnboard(t)
	onboardWizard = func(name, b string) (onboardWizardResult, error) {
		if name != "acme" || b != base {
			t.Errorf("wizard got name %q base %q", name, b)
		}
		return onboardWizardResult{Answers: &onboard.Answers{Name: name, Base: b, Git: onboard.Git{Email: "tester@acme.test"},
			Chat: &manifest.Service{Provider: "teams"}}, OpenTerminal: true}, nil
	}
	out, err := runOnboard(t, "acme", "--base", base)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	m, err := manifest.Load(filepath.Join(base, "acme", manifest.FileName))
	if err != nil || m.Chat.Provider != "teams" {
		t.Fatalf("manifest %+v err %v", m, err)
	}
	if len(*opened) != 1 {
		t.Fatal("the wizard's terminal answer is honoured")
	}
}

func TestReparse_RefusesASecretTypedIntoTheWizard(t *testing.T) {
	a := &onboard.Answers{Name: "acme", Git: onboard.Git{Email: "tester@acme.test"},
		Tracker: &manifest.Tracker{Provider: "jira", Env: "ghp_abcdefghijklmnopqrstuvwxyz0123"}} // tq:allow-secret (fake fixture)
	if _, err := reparse(a); err == nil {
		t.Fatal("a token typed into a name field must be refused")
	}
}
