package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	Cases []struct {
		Name           string   `json:"name"`
		Command        string   `json:"command"`
		Client         string   `json:"client"`
		Neutral        bool     `json:"neutral"`
		NeutralReason  string   `json:"neutral_reason"`
		Blocked        []string `json:"blocked"`
		CloudProvider  string   `json:"cloud_provider"`
		ExpectedEmail  string   `json:"expected_email"`
		ActualEmail    string   `json:"actual_email"`
		ExpectedGHUser string   `json:"expected_gh_user"`
		ActualGHUser   string   `json:"actual_gh_user"`
		Findings       []string `json:"findings"`
		Expect         struct {
			TQ struct {
				Block bool   `json:"block"`
				Rule  string `json:"rule"`
			} `json:"tq"`
		} `json:"expect"`
	} `json:"cases"`
}

func TestCanonicalSuite(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "tests", "fixtures", "guard_cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) != 20 {
		t.Fatalf("canonical suite must have 20 cases, got %d", len(f.Cases))
	}
	for _, c := range f.Cases {
		t.Run(c.Name, func(t *testing.T) {
			d := Decide(Input{
				Command: c.Command, Client: c.Client, Neutral: c.Neutral, NeutralReason: c.NeutralReason,
				Blocked: c.Blocked, CloudProvider: c.CloudProvider,
				ExpectedEmail: c.ExpectedEmail, ActualEmail: c.ActualEmail,
				ExpectedGHUser: c.ExpectedGHUser, ActualGHUser: c.ActualGHUser, Findings: c.Findings,
			})
			if d.Block != c.Expect.TQ.Block {
				t.Fatalf("block=%v want %v (reason %q)", d.Block, c.Expect.TQ.Block, d.Reason)
			}
			if c.Expect.TQ.Block && d.Rule != c.Expect.TQ.Rule {
				t.Fatalf("rule=%q want %q", d.Rule, c.Expect.TQ.Rule)
			}
			if d.Block && len(d.Reason) < 8 || d.Block && d.Reason[:8] != "BLOCKED:" {
				t.Fatalf("reason must start with BLOCKED: got %q", d.Reason)
			}
		})
	}
}

func TestStartsWith(t *testing.T) {
	cases := []struct {
		cmd, prefix string
		want        bool
	}{
		{"git push", "git", true},
		{"github", "git", false},
		{"echo x; gh pr list", "gh", true},
		{"echo x || az login", "az", true},
		{"  aws s3 ls", "aws", true},
		{"gh", "gh", true},
		{"git push --force", "git push --force", true},
		{"git push --force-with-lease", "git push --force", false},
		// Fix round 1 addendum: separators must also catch newlines and
		// common command-substitution/grouping wrappers.
		{"echo x\ngit push", "git", true},
		{"$(gh api user)", "gh", true},
	}
	for _, c := range cases {
		if got := StartsWith(c.cmd, c.prefix); got != c.want {
			t.Errorf("StartsWith(%q,%q)=%v want %v", c.cmd, c.prefix, got, c.want)
		}
	}
}

func TestReadOnlyGit(t *testing.T) {
	for _, c := range []string{"git status", "git --no-pager log -3", "git -C x diff", "git remote -v", "git branch -a", "git status; git log"} {
		if !IsReadOnlyGit(c) {
			t.Errorf("%q should be read-only", c)
		}
	}
	for _, c := range []string{"git commit -m x", "git push", "git branch -D x", "git remote add o u", "ls", "git status && git commit -m x"} {
		if IsReadOnlyGit(c) {
			t.Errorf("%q should NOT be read-only", c)
		}
	}
}

func TestRemoteMutationChainedAndWrapped(t *testing.T) {
	for _, c := range []string{"git status && git push origin main", "(git push)"} {
		if !IsRemoteMutation(c) {
			t.Errorf("%q should be a remote mutation", c)
		}
	}
}

// Fix round 2, finding 2: the single `&` (background) is a separator too.
func TestSingleAmpersandIsSeparator(t *testing.T) {
	for _, c := range []struct {
		cmd, prefix string
		want        bool
	}{
		{"sleep 0 & git push", "git", true},
		{"sleep 0 & gh pr list", "gh", true},
		{"sleep 0 && git push", "git", true},
		{"echo a&b", "b", true},
		{"echo grand", "and", false},
	} {
		if got := StartsWith(c.cmd, c.prefix); got != c.want {
			t.Errorf("StartsWith(%q,%q)=%v want %v", c.cmd, c.prefix, got, c.want)
		}
	}
	if !IsRemoteMutation("sleep 0 & git push") {
		t.Error("`sleep 0 & git push` must be a remote mutation")
	}
	if !IsRemoteMutation("sleep 0 & gh api user") {
		t.Error("`sleep 0 & gh api user` must be a remote mutation")
	}
	if IsReadOnlyGit("git status & git push") {
		t.Error("`git status & git push` must not be read-only")
	}
}

// Fix round 2, finding 3: inline identity overrides are blocked outright.
func TestInlineIdentityOverride(t *testing.T) {
	for _, c := range []string{
		"git -c user.email=x@y.z commit -m x",
		"git -c user.name=Someone commit -m x",
		"git --config-env=user.email=EMAIL commit -m x",
		"git --config-env=user.name=NAME commit -m x",
		"git -c User.Email=x@y.z commit -m x",
		"echo hi && git -c user.email=x@y.z push",
		"git -c user.email=x@y.z status",
	} {
		if !HasInlineIdentityOverride(c) {
			t.Errorf("%q should be an inline identity override", c)
		}
		d := Decide(Input{Command: c, Client: "acme", ExpectedEmail: "dev@acme.com"})
		if !d.Block || d.Rule != "git-email-drift" {
			t.Errorf("Decide(%q) = %+v, want block git-email-drift", c, d)
		}
	}
	for _, c := range []string{
		"git -c core.pager=cat log",
		"git -C repo commit -m x",
		"git commit -m 'user.email=x'",
		"gh -c user.email=x api user",
	} {
		if HasInlineIdentityOverride(c) {
			t.Errorf("%q should NOT be an inline identity override", c)
		}
	}
	// Blocks regardless of ActualEmail, and only when the workspace pins one.
	if d := Decide(Input{Command: "git -c user.email=x@y.z commit", Client: "acme", ExpectedEmail: "dev@acme.com", ActualEmail: "dev@acme.com"}); !d.Block {
		t.Error("inline override must block even when the configured email matches")
	}
	if d := Decide(Input{Command: "git -c user.email=x@y.z commit", Client: "acme"}); d.Block {
		t.Error("no expected email: nothing to protect, must not block")
	}
	// It is not a remote mutation, so the fallback guard leaves it alone.
	if IsRemoteMutation("git -c user.email=x@y.z commit -m x") {
		t.Error("`git -c user.email=... commit` is not a remote mutation")
	}
}

// Fix round 2, finding 8: read-only git is exempt from env-drift/untrusted too.
func TestReadOnlyGitExemptFromEnvDriftAndUntrusted(t *testing.T) {
	for _, finding := range []string{"env-drift", "untrusted"} {
		if d := Decide(Input{Command: "git status", Client: "acme", Findings: []string{finding}}); d.Block {
			t.Errorf("%s: `git status` must be allowed, got %+v", finding, d)
		}
		if d := Decide(Input{Command: "git commit -m x", Client: "acme", Findings: []string{finding}}); !d.Block || d.Rule != finding {
			t.Errorf("%s: `git commit` must block with that rule, got %+v", finding, d)
		}
		if d := Decide(Input{Command: "git status && git commit -m x", Client: "acme", Findings: []string{finding}}); !d.Block || d.Rule != finding {
			t.Errorf("%s: chained mutating git must block, got %+v", finding, d)
		}
	}
}
