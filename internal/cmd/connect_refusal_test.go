package cmd

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/connect"
	"github.com/tentaqles/tentaqles/internal/paths"
)

// A trusted client whose scope names gh but that has no claude identity has no Claude config:
// connect refuses, naming the identity, and writes no config, no connection line and no denial.
func TestConnect_NoClaudeIdentityRefused(t *testing.T) {
	root, _ := setupScoped(t, "acme", "identities: { gh: {} }\n")
	chdir(t, root)
	out, err := runTq(t, "connect", "gh")
	want := "identity acme has no claude identity"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("connect gh: err=%v out=%q, want %q", err, out, want)
	}
	if fileBytes(t, claudeJSONOf("acme")) != "<missing>" {
		t.Error("connect wrote a Claude config for an identity without one")
	}
	if n := len(logLines(t, connect.LogPath())); n != 0 {
		t.Errorf("%d connection lines for a refusal", n)
	}
	if n := len(logLines(t, filepath.Join(paths.Home(), "denials.jsonl"))); n != 0 {
		t.Errorf("%d denial lines: the provider is in scope, this is not a scope refusal", n)
	}
}

// --verify belongs to the doctor report: tq status --json --verify refuses, says where it goes,
// and runs no verify command.
func TestStatusJSON_VerifyRefused(t *testing.T) {
	root, _ := setupScoped(t, "acme", ghBody)
	chdir(t, root)
	statusSeams(t, func(string) (string, error) {
		t.Error("a verify command ran")
		return "", errors.New("must not run")
	})
	out, err := runTq(t, "status", "--json", "--verify", "on")
	if err == nil || !strings.Contains(err.Error(), "tq doctor --json --verify") {
		t.Fatalf("status --json --verify: err=%v out=%q", err, out)
	}
	if strings.Contains(out, `"identity"`) {
		t.Errorf("the refusal printed a status object: %q", out)
	}
}
