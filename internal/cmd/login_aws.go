package cmd

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

var ssoSessionRe = regexp.MustCompile(`^\s*\[\s*sso-session\s+([^\]\s]+)\s*\]`)

// ssoSessionNameRe is the allowlist for session names, from the file or the
// flag: they become a command-line argument, and a name with shell
// metacharacters or a leading dash must never reach one.
var ssoSessionNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// awsSSONote is printed once whenever an SSO path runs: the AWS CLI keeps SSO
// tokens in ~/.aws/sso/cache, which AWS_CONFIG_FILE does not move.
const awsSSONote = "tq: aws sso login tokens are cached in the shared ~/.aws/sso/cache; workspaces using the same SSO start URL share that login"

// awsSSOSessions lists the [sso-session NAME] sections of an AWS config file,
// sorted and de-duplicated. A missing or unreadable file has none.
func awsSSOSessions(configPath string) []string {
	if configPath == "" {
		return nil
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := ssoSessionRe.FindStringSubmatch(strings.TrimRight(line, "\r")); m != nil && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// awsLoginArgs picks the `aws` arguments for `tq login <ws> aws`.
//
//	--sso-session NAME   aws sso login --sso-session NAME (always wins)
//	--sso                aws configure sso (first-time SSO setup)
//	one sso-session      aws sso login --sso-session NAME
//	several sessions     error: --sso-session NAME is required
//	none                 aws configure (static access keys)
func awsLoginArgs(configPath string, sso bool, session string) ([]string, error) {
	if session = strings.TrimSpace(session); session != "" {
		if !ssoSessionNameRe.MatchString(session) {
			return nil, fmt.Errorf("invalid sso-session name %q (letters, digits, '.', '_' and '-' only)", session)
		}
		return []string{"sso", "login", "--sso-session", session}, nil
	}
	if sso {
		return []string{"configure", "sso"}, nil
	}
	switch names := awsSSOSessions(configPath); len(names) {
	case 0:
		return []string{"configure"}, nil
	case 1:
		if !ssoSessionNameRe.MatchString(names[0]) {
			return nil, fmt.Errorf("invalid sso-session name %q in %s (letters, digits, '.', '_' and '-' only)", names[0], configPath)
		}
		return []string{"sso", "login", "--sso-session", names[0]}, nil
	default:
		return nil, fmt.Errorf("%s has several sso-sessions (%s); pass --sso-session NAME", configPath, strings.Join(names, ", "))
	}
}

// isAWSSSOArgs reports whether args are one of the SSO login paths.
func isAWSSSOArgs(args []string) bool {
	return (len(args) > 0 && args[0] == "sso") || (len(args) > 1 && args[0] == "configure" && args[1] == "sso")
}

// awsConfigFile reads AWS_CONFIG_FILE out of an identity environment.
func awsConfigFile(env []string) string {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "AWS_CONFIG_FILE") {
			return v
		}
	}
	return ""
}
