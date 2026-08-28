// Package guard decides whether a shell command Claude wants to run is allowed
// in the current workspace. It is pure: callers gather every fact first.
package guard

import (
	"fmt"
	"regexp"
	"strings"
)

// Input is everything Decide needs; callers do all I/O.
type Input struct {
	Command        string   // the Bash command Claude wants to run
	Client         string   // manifest.Client ("" when no workspace)
	Neutral        bool     // cwd resolves to no trusted workspace
	NeutralReason  string   // resolve.Result.Reason when Neutral
	Blocked        []string // effective blocked-command list (manifest union)
	CloudProvider  string   // "azure"|"aws"|"gcp"|"digitalocean"|"" (lowercased)
	ExpectedEmail  string   // manifest.Git.Email
	ActualEmail    string   // effective `git config user.email` in cwd ("" if unknown)
	ExpectedGHUser string   // manifest.Git.User (or ExpectedUser)
	ActualGHUser   string   // from `gh api user --jq .login` ("" if unknown / not run)
	Findings       []string // doctor codes for cwd: "untrusted","env-drift","git-email-drift","claude-config-drift",...
}

// Decision is the outcome of Decide.
type Decision struct {
	Block  bool
	Rule   string // "blocked-command"|"wrong-cloud"|"neutral-remote"|"git-email-drift"|"gh-user"|"env-drift"|"untrusted"|"claude-config-drift"|""
	Reason string // multi-line message for stderr (starts with "BLOCKED: ")
}

// StartsWith reports whether command starts with prefix as a whole word, at the
// start of the command or after && || ; |. Port of the Python
// _command_starts_with: (?:^|&&|\|\||;|\|)\s*<prefix>(?:\s|$)
func StartsWith(command, prefix string) bool {
	p := `(?:^|&&|\|\||;|\|)\s*` + regexp.QuoteMeta(strings.TrimSpace(prefix)) + `(?:\s|$)`
	return regexp.MustCompile(p).MatchString(command)
}

// CloudCLIs maps cloud CLI binary names to the provider they belong to.
func CloudCLIs() map[string]string {
	return map[string]string{"az": "azure", "aws": "aws", "gcloud": "gcp", "gsutil": "gcp", "bq": "gcp", "doctl": "digitalocean"}
}

// IsGit reports whether command invokes git.
func IsGit(c string) bool { return StartsWith(c, "git") }

var readOnlySub = map[string]bool{"status": true, "log": true, "diff": true, "show": true, "rev-parse": true, "ls-files": true, "branch": true, "remote": true}

// gitSubcommand returns the first git sub-word after global flags (-C x, --no-pager, -c k=v, --git-dir=…)
// and the args after it. Handles only the FIRST git invocation in the command.
func gitSubcommand(c string) (sub string, rest []string) {
	// find the segment that starts with git
	for _, seg := range regexp.MustCompile(`&&|\|\||;|\|`).Split(c, -1) {
		f := strings.Fields(seg)
		if len(f) == 0 || f[0] != "git" {
			continue
		}
		i := 1
		for i < len(f) {
			a := f[i]
			switch {
			case a == "-C" || a == "-c":
				i += 2
				continue
			case strings.HasPrefix(a, "-"):
				i++
				continue
			}
			return a, f[i+1:]
		}
		return "", nil
	}
	return "", nil
}

// IsReadOnlyGit reports whether command is a git invocation known to be read-only.
func IsReadOnlyGit(c string) bool {
	sub, rest := gitSubcommand(c)
	if !readOnlySub[sub] {
		return false
	}
	switch sub {
	case "branch":
		for _, a := range rest {
			switch a {
			case "-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force", "--set-upstream-to", "-u", "--unset-upstream":
				return false
			}
		}
		pos := 0
		for _, a := range rest {
			if !strings.HasPrefix(a, "-") {
				pos++
			}
		}
		return pos <= 1
	case "remote":
		if len(rest) == 0 {
			return true
		}
		return rest[0] == "-v" || rest[0] == "show" || rest[0] == "get-url"
	}
	return true
}

// IsRemoteMutation reports whether command touches a remote: git push/fetch/pull/clone,
// any gh invocation, or any cloud CLI invocation.
func IsRemoteMutation(c string) bool {
	if StartsWith(c, "gh") {
		return true
	}
	for cli := range CloudCLIs() {
		if StartsWith(c, cli) {
			return true
		}
	}
	sub, _ := gitSubcommand(c)
	switch sub {
	case "push", "fetch", "pull", "clone":
		return true
	}
	return false
}

func has(fs []string, code string) bool {
	for _, f := range fs {
		if f == code {
			return true
		}
	}
	return false
}

// Decide applies the rule precedence documented in the task brief:
// blocked-command -> wrong-cloud -> neutral-remote -> untrusted (git only) ->
// env-drift (git only) -> claude-config-drift (git push / gh only) ->
// git-email-drift (non-read-only git only, both emails known) ->
// gh-user (gh only, both users known).
func Decide(in Input) Decision {
	block := func(rule, msg string) Decision { return Decision{Block: true, Rule: rule, Reason: "BLOCKED: " + msg} }
	client := in.Client
	if client == "" {
		client = "unknown"
	}
	for _, b := range in.Blocked {
		if strings.TrimSpace(b) == "" {
			continue
		}
		if StartsWith(in.Command, b) {
			return block("blocked-command", fmt.Sprintf("Command '%s' is blocked by client manifest.\n  Client: %s", b, client))
		}
	}
	if p := strings.ToLower(in.CloudProvider); p != "" && p != "none" {
		for cli, prov := range CloudCLIs() {
			if StartsWith(in.Command, cli) && prov != p {
				return block("wrong-cloud", fmt.Sprintf("This workspace uses %s, but you ran a %s command ('%s').\n  Client: %s", p, prov, cli, client))
			}
		}
	}
	if in.Neutral {
		if IsRemoteMutation(in.Command) {
			return block("neutral-remote", fmt.Sprintf("cwd is not inside a trusted tq workspace (%s); refusing a remote/cloud command with no identity.\n  Fix: cd into a workspace, or run: tq allow <name>", in.NeutralReason))
		}
		return Decision{}
	}
	git := IsGit(in.Command)
	if git && has(in.Findings, "untrusted") {
		return block("untrusted", fmt.Sprintf("workspace %s is not trusted; git is refused until you run: tq allow %s", client, client))
	}
	if git && has(in.Findings, "env-drift") {
		return block("env-drift", "shell identity (TQ_WS) does not match cwd; open a new shell or run: eval \"$(tq env --shell <shell>)\"")
	}
	if (IsRemoteMutation(in.Command)) && has(in.Findings, "claude-config-drift") {
		return block("claude-config-drift", fmt.Sprintf("this Claude session is not running under the %s identity dir (CLAUDE_CONFIG_DIR drift).\n  Fix: start Claude from a tq-activated shell in the workspace, or: tq run %s -- claude", client, client))
	}
	if git && !IsReadOnlyGit(in.Command) && in.ExpectedEmail != "" && in.ActualEmail != "" && !strings.EqualFold(in.ExpectedEmail, in.ActualEmail) {
		return block("git-email-drift", fmt.Sprintf("Git email mismatch.\n  Expected: %s\n  Actual:   %s\n  Fix: tq doctor (identity is managed by tq; do not set user.email by hand)", in.ExpectedEmail, in.ActualEmail))
	}
	if StartsWith(in.Command, "gh") && in.ExpectedGHUser != "" && in.ActualGHUser != "" && !strings.EqualFold(in.ExpectedGHUser, in.ActualGHUser) {
		return block("gh-user", fmt.Sprintf("GitHub user mismatch.\n  Expected: %s\n  Actual:   %s\n  Fix: tq login %s gh", in.ExpectedGHUser, in.ActualGHUser, client))
	}
	return Decision{}
}
