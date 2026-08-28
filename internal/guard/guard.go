// Package guard decides whether a shell command Claude wants to run is allowed
// in the current workspace. It is pure: callers gather every fact first.
//
// Command splitting/prefix-matching here is defense-in-depth string
// heuristics, not a shell parser: it recognizes common separators
// (&& || ; | newline/CR) and common command-substitution/grouping openers
// ($( ` ( { ) } ) so an obvious wrapper like "(git push)" or "$(gh api user)"
// doesn't slip past the guard, but it makes no attempt to fully parse shell
// quoting, escaping, or nesting.
package guard

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
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

// sepPattern is the single shared definition of what separates one shell
// "segment" from another for our purposes: the classic command separators
// (&& || ; |), line breaks, and the common command-substitution/grouping
// tokens ($( ` ( { ) }) that can otherwise hide a command from a naive
// prefix check. StartsWith and the git segment splitter both use this so
// they never drift apart.
const sepPattern = `&&|\|\||;|\||\n|\r|\$\(|` + "`" + `|\(|\)|\{|\}`

var sepRegexp = regexp.MustCompile(sepPattern)

// startsWithCache memoizes the compiled per-prefix regexp built by
// StartsWith: the set of distinct prefixes in a process (manifest blocked
// commands, cloud CLI names, "git", "gh") is small and fixed, so a cache
// avoids recompiling the same pattern on every call.
var startsWithCache sync.Map // map[string]*regexp.Regexp

// StartsWith reports whether command starts with prefix as a whole word, at the
// start of the command or after a separator (see sepPattern). Port of the
// Python _command_starts_with, extended to also treat command substitution
// and grouping openers as separators.
func StartsWith(command, prefix string) bool {
	// TrimSpace(prefix): callers pass manifest entries like "gh " (trailing
	// space) meant to match "gh pr list" etc. Since matching is now
	// whole-word (not substring), the trailing space in the source data
	// would otherwise require a literal double space in the command; trim
	// it here so "gh " still matches "gh pr list".
	key := strings.TrimSpace(prefix)
	var re *regexp.Regexp
	if v, ok := startsWithCache.Load(key); ok {
		re = v.(*regexp.Regexp)
	} else {
		re = regexp.MustCompile(`(?:^|` + sepPattern + `)\s*` + regexp.QuoteMeta(key) + `(?:\s|$)`)
		startsWithCache.Store(key, re)
	}
	return re.MatchString(command)
}

// CloudCLIs maps cloud CLI binary names to the provider they belong to.
func CloudCLIs() map[string]string {
	return map[string]string{"az": "azure", "aws": "aws", "gcloud": "gcp", "gsutil": "gcp", "bq": "gcp", "doctl": "digitalocean"}
}

// IsGit reports whether command invokes git.
func IsGit(c string) bool { return StartsWith(c, "git") }

var readOnlySub = map[string]bool{"status": true, "log": true, "diff": true, "show": true, "rev-parse": true, "ls-files": true, "branch": true, "remote": true}

type gitInvocation struct {
	sub  string
	rest []string
}

// gitSegments returns every git invocation found in command (split on
// sepPattern), each with its sub-word after global flags (-C x, -c k=v, …)
// and the args following it.
func gitSegments(c string) []gitInvocation {
	var out []gitInvocation
	for _, seg := range sepRegexp.Split(c, -1) {
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
			out = append(out, gitInvocation{sub: a, rest: f[i+1:]})
			break
		}
		// If the loop exhausts f without hitting the default case (e.g. a
		// bare "git" or "git --no-pager"), there is no sub-word to record.
	}
	return out
}

func isReadOnlySegment(sub string, rest []string) bool {
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

// IsReadOnlyGit reports whether command is a git invocation known to be
// read-only. A command with multiple chained git invocations is read-only
// only if EVERY git segment in it is read-only.
func IsReadOnlyGit(c string) bool {
	segs := gitSegments(c)
	if len(segs) == 0 {
		return false
	}
	for _, s := range segs {
		if !isReadOnlySegment(s.sub, s.rest) {
			return false
		}
	}
	return true
}

// IsRemoteMutation reports whether command touches a remote: git push/fetch/pull/clone
// (in ANY chained git segment), any gh invocation, or any cloud CLI invocation.
func IsRemoteMutation(c string) bool {
	if StartsWith(c, "gh") {
		return true
	}
	for cli := range CloudCLIs() {
		if StartsWith(c, cli) {
			return true
		}
	}
	for _, s := range gitSegments(c) {
		switch s.sub {
		case "push", "fetch", "pull", "clone":
			return true
		}
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
