package data

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Nested shells, global flags and JSON escapes (fix round 1 of review r0).

// costNestMax bounds how deep nested shells are read (bash -c "sh -c …").
const costNestMax = 4

// costShellCount bounds the nested strings read from one command.
const costShellCount = 200

// costShells run a command string: bash -c, pwsh -Command, cmd /c, eval, iex.
var costShells = map[string]bool{"bash": true, "sh": true, "zsh": true, "dash": true, "ksh": true, "fish": true, "ash": true,
	"pwsh": true, "powershell": true, "cmd": true, "eval": true, "iex": true, "invoke-expression": true}

// costSegmentsDeep returns the segments of cmd and, recursively, of every
// command string a nested shell in it runs (bash -c, sh -lc, pwsh -Command,
// pwsh -EncodedCommand, cmd /c, eval, Invoke-Expression). wsl and xargs need
// nothing special: their command words stay in the segment. opaque reports a
// nested string the guard could not read (a nest deeper than costNestMax, or
// an encoded command that does not decode); callers ask on an opaque command
// that names their tool, so the guards fail closed.
func costSegmentsDeep(cmd string) (segs []costSegment, opaque bool) {
	count := 0
	var walk func(s string, depth int)
	walk = func(s string, depth int) {
		for _, seg := range costSegments(s) {
			segs = append(segs, seg)
			for k, w := range seg.Words {
				if !costShells[costToolName(w)] {
					continue
				}
				inner, ok, bad := costNested(seg.Words[k:])
				if bad {
					opaque = true
				}
				if !ok {
					continue
				}
				count++
				if depth+1 > costNestMax || count > costShellCount {
					opaque = true
					continue
				}
				walk(inner, depth+1)
			}
		}
	}
	walk(cmd, 0)
	return segs, opaque
}

// costPwshValueFlags take a value in pwsh/powershell before the command.
var costPwshValueFlags = costSet("-executionpolicy", "-ex", "-ep", "-windowstyle", "-w", "-configurationname",
	"-workingdirectory", "-wd", "-version", "-v", "-inputformat", "-outputformat", "-of", "-psconsolefile",
	"-settingsfile", "-custompipename")

func costIsPwshCommandFlag(l string) bool {
	return l == "-c" || (len(l) > 2 && strings.HasPrefix("-command", l))
}

// costNested returns the command string that words (a shell word first)
// would run. bad reports an encoded command that does not decode.
func costNested(words []string) (inner string, ok, bad bool) {
	verb := costToolName(words[0])
	rest := words[1:]
	switch verb {
	case "eval":
		if len(rest) == 0 {
			return "", false, false
		}
		return strings.Join(rest, " "), true, false
	case "iex", "invoke-expression":
		for i := 0; i < len(rest); i++ {
			l := strings.ToLower(rest[i])
			if costIsPwshCommandFlag(l) && i+1 < len(rest) {
				return strings.Join(rest[i+1:], " "), true, false
			}
			if !strings.HasPrefix(l, "-") {
				return strings.Join(rest[i:], " "), true, false
			}
		}
		return "", false, false
	case "cmd":
		for i, w := range rest {
			l := strings.ToLower(w)
			if l == "/c" || l == "/k" || l == "/r" {
				return strings.Join(rest[i+1:], " "), true, false
			}
			if len(l) > 2 && (strings.HasPrefix(l, "/c") || strings.HasPrefix(l, "/k")) {
				// cmd /cbq query … (no space)
				return strings.Join(append([]string{w[2:]}, rest[i+1:]...), " "), true, false
			}
		}
		return "", false, false
	case "pwsh", "powershell":
		for i := 0; i < len(rest); i++ {
			l := strings.ToLower(rest[i])
			switch {
			case l == "-e" || l == "-ec" || (len(l) >= 4 && strings.HasPrefix("-encodedcommand", l)):
				if i+1 >= len(rest) {
					return "", false, true
				}
				s, err := costDecodePwsh(rest[i+1])
				if err != nil {
					return "", false, true
				}
				return s, true, false
			case costIsPwshCommandFlag(l):
				if i+1 >= len(rest) {
					return "", false, false
				}
				return strings.Join(rest[i+1:], " "), true, false
			case l == "-f" || (len(l) > 2 && strings.HasPrefix("-file", l)):
				return "", false, false
			case costPwshValueFlags[l]:
				i++
			case strings.HasPrefix(l, "-"):
			default:
				if verb == "powershell" {
					// Windows PowerShell runs a bare argument as -Command.
					return strings.Join(rest[i:], " "), true, false
				}
				return "", false, false // pwsh runs a bare argument as -File
			}
		}
		return "", false, false
	}
	// POSIX shells: -c, or a short option cluster with c (-lc, -ec, -xc).
	// Every word is scanned (bash -o pipefail -c …); a script run with an
	// argument -c… is read as a command too, which only fails closed.
	for i, w := range rest {
		if strings.HasPrefix(w, "--") || !strings.HasPrefix(w, "-") {
			continue
		}
		if strings.Contains(w, "c") {
			for j := i + 1; j < len(rest); j++ {
				if !strings.HasPrefix(rest[j], "-") {
					return rest[j], true, false
				}
			}
			return "", false, false
		}
	}
	return "", false, false
}

// costDecodePwsh decodes a PowerShell -EncodedCommand (base64 of UTF-16LE).
func costDecodePwsh(s string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return "", err
	}
	if len(b)%2 != 0 {
		return "", errors.New("odd length")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u)), nil
}

// costMayNest reports whether a lower-cased command could hide a tool name
// from a substring prefilter (an encoded PowerShell command).
func costMayNest(low string) bool {
	return strings.Contains(low, "pwsh") || strings.Contains(low, "powershell")
}

// costSubPath returns the positional words of args from the first one in
// groups on, skipping flags and the values of valueFlags (global flags such
// as databricks -p prod, given before the subcommand). nil when no
// positional word of args is a group.
func costSubPath(args []string, valueFlags, groups map[string]bool) []string {
	var pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && valueFlags[a] {
				i++
			}
			continue
		}
		pos = append(pos, a)
	}
	for i, p := range pos {
		if groups[p] {
			return pos[i:]
		}
	}
	return nil
}

func costSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// Value flags per CLI (global ones, plus the subcommand flags whose values
// could look like a group word), and the groups the guards read.
var (
	dbxValueFlags = costSet("-p", "--profile", "-t", "--target", "-o", "--output", "--log-level", "--log-file",
		"--log-format", "--json", "--json-file")
	dbxGroups      = costSet("clusters", "warehouses", "api")
	azValueFlags   = costSet("--subscription", "-o", "--output", "--query")
	azGroups       = costSet("synapse", "fabric")
	snowValueFlags = costSet("--config-file", "--connection", "-c", "--environment", "--format", "--account",
		"--accountname", "--user", "--username", "--password", "--authenticator", "--private-key-file",
		"--private-key-path", "--token-file-path", "--database", "--dbname", "--schema", "--schemaname", "--role",
		"--rolename", "--warehouse", "--host", "--port", "--region", "--mfa-passcode", "-q", "--query", "-f",
		"--filename", "-D", "--variable")
	snowGroups = costSet("sql")
)

// costNamesTool reports whether a lower-cased command names tool as a word
// (bounded by anything but a letter, digit or underscore).
func costNamesTool(low, tool string) bool {
	word := func(c byte) bool {
		return c == '_' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
	}
	for off := 0; off < len(low); {
		i := strings.Index(low[off:], tool)
		if i < 0 {
			return false
		}
		i += off
		j := i + len(tool)
		if (i == 0 || !word(low[i-1])) && (j >= len(low) || !word(low[j])) {
			return true
		}
		off = i + 1
	}
	return false
}

// costStatements splits SQL text into its statements on ; outside quotes.
func costStatements(q string) []string {
	var out []string
	var quote byte
	start := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == ';':
			if s := strings.TrimSpace(q[start:i]); s != "" {
				out = append(out, s)
			}
			start = i + 1
		}
	}
	if s := strings.TrimSpace(q[start:]); s != "" {
		out = append(out, s)
	}
	return out
}

// costUnbounded reports a statement that reads rows with no LIMIT/FETCH.
func costUnbounded(st string) bool {
	u := costUnquoted(st)
	return costSelectRe.MatchString(u) && !costLimitRe.MatchString(u)
}

var (
	sfTimeoutAnyRe = regexp.MustCompile(`(?is)\b(?:UN)?SET\b.*\bSTATEMENT_TIMEOUT_IN_SECONDS\b`)
	sfTimeoutRe    = regexp.MustCompile(`(?is)\bSET\b.*\bSTATEMENT_TIMEOUT_IN_SECONDS\s*=\s*'?\s*([0-9]+)`)
	pgTimeoutAnyRe = regexp.MustCompile(`(?is)\b(?:SET\s+(?:SESSION\s+|LOCAL\s+)?|RESET\s+)statement_timeout\b`)
	pgTimeoutRe    = regexp.MustCompile(`(?is)\bSET\s+(?:SESSION\s+|LOCAL\s+)?statement_timeout\s*(?:=|\s+TO\s+)\s*'?\s*([0-9]+)`)
	pgOptAnyRe     = regexp.MustCompile(`(?i)PGOPTIONS\s*=`)
	pgOptRe        = regexp.MustCompile(`(?is)PGOPTIONS\s*=.*statement_timeout\s*=\s*'?\s*([0-9]+)`)
)

func costNonZero(digits string) bool { return strings.TrimLeft(digits, "0") != "" }

// costTimeoutSet reports whether st sets (or resets) a statement timeout,
// and whether the timeout it leaves is bounded (a literal above 0).
func costTimeoutSet(re, anyRe *regexp.Regexp, st string) (set, bounded bool) {
	if !anyRe.MatchString(st) {
		return false, false
	}
	m := re.FindStringSubmatch(st)
	return true, m != nil && costNonZero(m[1])
}

// costPGOptions reports a PGOPTIONS assignment in text and whether it sets a
// non-zero statement_timeout.
func costPGOptions(text string) (set, bounded bool) {
	if !pgOptAnyRe.MatchString(text) {
		return false, false
	}
	m := pgOptRe.FindStringSubmatch(text)
	return true, m != nil && costNonZero(m[1])
}

// costMCPMaxStrings and costMCPMaxDepth bound the walk over an MCP input.
const (
	costMCPMaxStrings = 2000
	costMCPMaxDepth   = 32
)

// costMCPStrings returns the string values of an MCP tool input, decoded from
// JSON (so \n, \t and \uXXXX are real characters). Input that is not JSON is
// returned once with those escapes undone.
func costMCPStrings(content string) []string {
	var v any
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return []string{costUnescape(content)}
	}
	var out []string
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		if depth > costMCPMaxDepth || len(out) >= costMCPMaxStrings {
			return
		}
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case []any:
			for _, e := range t {
				walk(e, depth+1)
			}
		case map[string]any:
			for _, e := range t {
				walk(e, depth+1)
			}
		}
	}
	walk(v, 0)
	return out
}

// costUnescape undoes the JSON string escapes in s, leaving anything else.
func costUnescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		switch n := s[i+1]; n {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b', 'f':
			b.WriteByte(' ')
		case '"', '\\', '/':
			b.WriteByte(n)
		case 'u':
			if i+6 <= len(s) {
				if r, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
					b.WriteRune(rune(r))
					i += 5
					continue
				}
			}
			b.WriteByte(c)
			continue
		default:
			b.WriteByte(c)
			continue
		}
		i++
	}
	return b.String()
}
