package envplan

import "path/filepath"

// Provider describes how one CLI is pointed at a private config home.
type Provider struct {
	Name      string
	Vars      func(dir string) map[string]string
	LoginCmd  string // executable to run for `tq login`
	LoginArgs []string
}

func dirVar(v string) func(string) map[string]string {
	return func(d string) map[string]string { return map[string]string{v: d} }
}

// Providers is the single place the env-var mapping lives. Keep manifest.KnownIdentities in sync.
func Providers() map[string]Provider {
	return map[string]Provider{
		"claude": {Name: "claude", Vars: dirVar("CLAUDE_CONFIG_DIR"), LoginCmd: "claude", LoginArgs: []string{"/login"}},
		"codex":  {Name: "codex", Vars: dirVar("CODEX_HOME"), LoginCmd: "codex", LoginArgs: []string{"login"}},
		"gemini": {Name: "gemini", Vars: dirVar("GEMINI_CLI_HOME"), LoginCmd: "gemini"},
		"cursor": {Name: "cursor", Vars: dirVar("CURSOR_CONFIG_DIR"), LoginCmd: "agent", LoginArgs: []string{"login"}},
		"gh":     {Name: "gh", Vars: dirVar("GH_CONFIG_DIR"), LoginCmd: "gh", LoginArgs: []string{"auth", "login"}},
		"az":     {Name: "az", Vars: dirVar("AZURE_CONFIG_DIR"), LoginCmd: "az", LoginArgs: []string{"login"}},
		"gcloud": {Name: "gcloud", Vars: dirVar("CLOUDSDK_CONFIG"), LoginCmd: "gcloud", LoginArgs: []string{"auth", "login"}},
		"aws": {Name: "aws", Vars: func(d string) map[string]string {
			return map[string]string{
				"AWS_CONFIG_FILE":             filepath.Join(d, "config"),
				"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(d, "credentials"),
			}
		}, LoginCmd: "aws", LoginArgs: []string{"configure"}},
		"kube": {Name: "kube", Vars: func(d string) map[string]string {
			return map[string]string{"KUBECONFIG": filepath.Join(d, "config")}
		}},
		"npm": {Name: "npm", Vars: func(d string) map[string]string {
			return map[string]string{"NPM_CONFIG_USERCONFIG": filepath.Join(d, "npmrc")}
		}, LoginCmd: "npm", LoginArgs: []string{"login"}},
	}
}
