// Package secrets detects credential-shaped values in text. It is the Go twin
// of plugin/tentaqles/privacy.py's REDACTION_PATTERNS (kept in the same order,
// specific vendors first) plus a few vendor formats the plugin does not need.
//
// Detection is heuristic. Callers use it to refuse or mask, never to prove a
// value is safe. Findings never carry the matched value itself, only its
// pattern name and position, so a report can be printed or logged safely.
package secrets

import (
	"bufio"
	"regexp"
	"strings"
)

// Pattern is one named detector.
type Pattern struct {
	Name string
	Re   *regexp.Regexp
}

// Patterns is ordered: the first pattern that matches a line names the finding.
var Patterns = []Pattern{
	{"aws_access_key", regexp.MustCompile(`\b(?:AKIA|ASIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASCA)[0-9A-Z]{16}\b`)},
	{"gcp_service_account", regexp.MustCompile(`"type"\s*:\s*"service_account"`)},
	{"github_token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr|github_pat)_[A-Za-z0-9_]{20,}\b`)},
	{"anthropic_key", regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"openai_key", regexp.MustCompile(`\bsk-(?:proj-|svcacct-)?[A-Za-z0-9_\-]{20,}`)},
	{"stripe_key", regexp.MustCompile(`\b(?:sk|rk)_live_[A-Za-z0-9]{16,}`)},
	{"slack_token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9\-]{10,}`)},
	{"google_api_key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"vercel_key", regexp.MustCompile(`\bvck_[A-Za-z0-9]{20,}`)},
	{"azure_storage_key", regexp.MustCompile(`(?i)AccountKey=[A-Za-z0-9+/=]{40,}`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}\.[A-Za-z0-9_\-]{5,}`)},
	{"bearer_token", regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._\-+/=]{16,}`)},
	{"connection_string", regexp.MustCompile(`\b(?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|rediss|mssql|sqlserver|amqp|amqps)://[^\s:/@]+:[^\s/@]+@[^\s/]+`)},
	{"private_key", regexp.MustCompile(`-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY-----`)},
	{"api_key_assignment", regexp.MustCompile(`(?i)\b[a-z0-9_.-]*(?:api[_-]?key|secret|token|password|passwd|pwd|credential)s?\b["']?\s*[:=]\s*["']?[A-Za-z0-9_\-./+=!@#$%^&*]{12,}`)},
}

// placeholderRe matches values that are references, not secrets:
// ${VAR}, $VAR, %VAR%, <redacted>, xxx..., your-key-here and friends.
var placeholderRe = regexp.MustCompile(`(?i)(\$\{[A-Z0-9_]+(:-[^}]*)?\}|\$[A-Z_][A-Z0-9_]{2,}\b|(?-i:%[A-Z_][A-Z0-9_]{2,}%)|<[^>]*>|\b(x{6,}|changeme|your[_-]?[a-z_-]*here|dummy|placeholder|redacted)\b|process\.env|import\.meta\.env|os\.environ|getenv|\benv\(|\$\{\{|secrets\.|config\.)`)

// AllowMarkers silence a finding on the line that carries them, matching the
// gitleaks convention so existing fixtures keep working.
var AllowMarkers = []string{"gitleaks:allow", "tq:allow-secret"}

// Finding locates a secret without carrying its value.
type Finding struct {
	Pattern string
	Line    int // 1-based
}

// ScanLine returns the name of the first pattern that matches line, or "".
// Lines carrying an allow marker, and api_key_assignment hits whose value is
// a placeholder, are ignored.
func ScanLine(line string) string {
	for _, m := range AllowMarkers {
		if strings.Contains(line, m) {
			return ""
		}
	}
	for _, p := range Patterns {
		loc := p.Re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		if p.Name == "api_key_assignment" || p.Name == "connection_string" || p.Name == "bearer_token" {
			// Generic patterns: a reference anywhere on the line (an env
			// lookup, ${VAR}, a template) means the value is wired in, not
			// written down. Vendor-format tokens are always reported.
			if placeholderRe.MatchString(line) {
				continue
			}
		}
		if p.Name == "api_key_assignment" && codeReference(line[loc[0]:loc[1]]) {
			continue
		}
		return p.Name
	}
	return ""
}

// assignValueRe pulls the value out of an api_key_assignment match.
var assignValueRe = regexp.MustCompile(`[:=]\s*["']?([^"'\s]+)`)

// identRefRe is a code reference rather than a literal: options.apiKey,
// this.config.token, cfg.Secret().
var identRefRe = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)+(\(\))?[,;)]?$`)

// plainIdentRe is a bare identifier (token_budget, maxTokens): real
// credentials essentially always carry digits or symbols.
var plainIdentRe = regexp.MustCompile(`^[A-Za-z_]+[,;)]?$`)

func codeReference(match string) bool {
	m := assignValueRe.FindStringSubmatch(match)
	return m != nil && (identRefRe.MatchString(m[1]) || plainIdentRe.MatchString(m[1]))
}

// Scan reports every line of text that holds a secret-shaped value.
func Scan(text string) []Finding {
	var out []Finding
	sc := bufio.NewScanner(strings.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if name := ScanLine(sc.Text()); name != "" {
			out = append(out, Finding{Pattern: name, Line: n})
		}
	}
	return out
}

// Contains reports whether text holds at least one secret-shaped value.
func Contains(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if ScanLine(line) != "" {
			return true
		}
	}
	return false
}

// Redact replaces every secret-shaped span in text with [REDACTED:<pattern>].
func Redact(text string) string {
	for _, p := range Patterns {
		text = p.Re.ReplaceAllStringFunc(text, func(m string) string {
			if (p.Name == "api_key_assignment" || p.Name == "connection_string" || p.Name == "bearer_token") && placeholderRe.MatchString(m) {
				return m
			}
			return "[REDACTED:" + p.Name + "]"
		})
	}
	return text
}
