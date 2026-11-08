package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// companyXBody is an Azure-only client with Azure DevOps as its git host.
const companyXBody = "git: { email: dev@company-x.example, provider: azure-devops }\n" +
	"identities: { az: {}, claude: {} }\ncloud: { provider: azure }\n"

// setupScoped writes client name (the manifest body given) under an isolated home, plus a
// second client, beta, in the same clients folder. It returns both folders.
func setupScoped(t *testing.T, name, body string) (root, beta string) {
	t.Helper()
	base := newBase(t)
	root = writeClient(t, base, name, body)
	beta = writeClient(t, base, "beta", "git: { email: dev@beta.example }\nidentities: { aws: {} }\ncloud: { provider: aws }\n")
	writeTB(t, filepath.Join(beta, "src", "main.go"), "package main\n")
	t.Setenv("TQ_WS", name)
	return root, beta
}

func jsonPath(t *testing.T, p string) string {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// hookPayloadFor is a PreToolUse payload of a Bash command run in cwd.
func hookPayloadFor(t *testing.T, cwd, command string) string {
	t.Helper()
	return `{"tool_name":"Bash","cwd":` + jsonPath(t, cwd) + `,"tool_input":{"command":` + jsonPath(t, command) + `}}`
}

// clearAgentMarkers unsets CLAUDECODE: the suite may run inside a Claude Code session.
func clearAgentMarkers(t *testing.T) {
	t.Helper()
	unsetenv(t, "CLAUDECODE")
}
