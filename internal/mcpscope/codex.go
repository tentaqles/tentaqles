package mcpscope

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Codex keeps its MCP servers in CODEX_HOME/config.toml as tables, [mcp_servers.<name>] with
// sub-tables such as [mcp_servers.<name>.env], or as one-line inline tables under
// [mcp_servers] (name = { command = "...", args = [...] }). FilterCodex reads them line by
// line, as codexcfg does for tq's own block: a removed server takes its header lines and every
// line up to the next table header that is not its own; tq's managed block and every other
// line stay byte for byte. A dotted key outside those tables (mcp_servers.x.command = ...) is
// not read; the hook still denies the server's tools by name.

var (
	tomlHeaderRe = regexp.MustCompile(`^\s*\[\[?\s*([^\[\]]+?)\s*\]\]?\s*(#.*)?$`)
	tomlStrKeyRe = regexp.MustCompile(`^\s*(command|url)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	tomlInlineRe = regexp.MustCompile(`^\s*("[^"]+"|'[^']+'|[A-Za-z0-9_-]+)\s*=\s*\{(.*)\}\s*(#.*)?$`)
	inlineStrRe  = regexp.MustCompile(`\b(command|url)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	// The args and env of a server: the quoted strings of an args array, the keys of an env
	// table or inline table.
	tomlStrRe       = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|'([^']*)'`)
	tomlArgsRe      = regexp.MustCompile(`^\s*args\s*=\s*(\[.*)$`)
	tomlEnvInlineRe = regexp.MustCompile(`^\s*env\s*=\s*\{(.*)\}`)
	tomlKeyRe       = regexp.MustCompile(`(?:^|[{,])\s*("[^"]+"|'[^']+'|[A-Za-z0-9_-]+)\s*=`)
	inlineArgsRe    = regexp.MustCompile(`\bargs\s*=\s*(\[[^\]]*\])`)
	inlineEnvRe     = regexp.MustCompile(`\benv\s*=\s*\{([^}]*)\}`)
	tomlBearerRe    = regexp.MustCompile(`^\s*bearer_token_env_var\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	tomlEnvVarsRe   = regexp.MustCompile(`^\s*env_vars\s*=\s*(\[.*)$`)
	tomlHdrInlineRe = regexp.MustCompile(`^\s*env_http_headers\s*=\s*\{(.*)\}`)
	tomlKVStrRe     = regexp.MustCompile(`(?:^|[{,])\s*(?:"[^"]+"|'[^']+'|[A-Za-z0-9_-]+)\s*=\s*(?:"([^"]*)"|'([^']*)')`)
)

// tomlKeyPath splits a dotted TOML key (mcp_servers."aws-api".env) into its parts.
func tomlKeyPath(k string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		case c == ' ' || c == '\t':
		default:
			cur.WriteByte(c)
		}
	}
	return append(out, strings.TrimSpace(cur.String()))
}

func unquoteKey(k string) string {
	if len(k) >= 2 && (k[0] == '"' || k[0] == '\'') && k[len(k)-1] == k[0] {
		return k[1 : len(k)-1]
	}
	return k
}

// line is one config.toml line with what the scan learned of it.
type line struct {
	text   string
	server string // the server whose table holds the line ("" none)
	inline string // a one-line inline server entry under [mcp_servers]
}

// scanCodex labels every line with the server it belongs to and gathers each server's
// command and url. Multi-line arrays and strings are skipped so a line inside them is never
// read as a header.
func scanCodex(content string) ([]line, map[string]map[string]any) {
	facts := map[string]map[string]any{}
	fact := func(n string) map[string]any {
		if facts[n] == nil {
			facts[n] = map[string]any{}
		}
		return facts[n]
	}
	addArgs := func(f map[string]any, s string) {
		a, _ := f["args"].([]any)
		for _, m := range tomlStrRe.FindAllStringSubmatch(s, -1) {
			a = append(a, m[1]+m[2])
		}
		f["args"] = a
	}
	addEnv := func(f map[string]any, k string) {
		e, _ := f["env"].(map[string]any)
		if e == nil {
			e = map[string]any{}
		}
		e[k] = ""
		f["env"] = e
	}
	var lines []line
	table := []string{}
	depth := 0   // open [ of a multi-line array value
	inML := ""   // the delimiter of an open multi-line string
	argsOf := "" // the server whose multi-line args array is open
	for _, l := range strings.SplitAfter(content, "\n") {
		if l == "" {
			continue
		}
		ln := line{text: l}
		t := strings.TrimRight(l, "\r\n")
		switch {
		case inML != "":
			if strings.Count(t, inML)%2 == 1 {
				inML = ""
			}
		case depth > 0:
			if argsOf != "" {
				addArgs(fact(argsOf), t)
			}
			depth += bracketDelta(t)
			if depth <= 0 {
				depth, argsOf = 0, ""
			}
		case strings.HasPrefix(strings.TrimSpace(t), "#"):
			// a comment: no header, no value
		default:
			argsOf = ""
			if m := tomlHeaderRe.FindStringSubmatch(t); m != nil {
				table = tomlKeyPath(m[1])
			} else if len(table) == 1 && table[0] == "mcp_servers" {
				if m := tomlInlineRe.FindStringSubmatch(t); m != nil {
					ln.inline = unquoteKey(m[1])
					f := fact(ln.inline)
					for _, s := range inlineStrRe.FindAllStringSubmatch(m[2], -1) {
						f[s[1]] = s[2] + s[3]
					}
					if a := inlineArgsRe.FindStringSubmatch(m[2]); a != nil {
						addArgs(f, a[1])
					}
					if e := inlineEnvRe.FindStringSubmatch(m[2]); e != nil {
						for _, k := range tomlKeyRe.FindAllStringSubmatch(e[1], -1) {
							addEnv(f, unquoteKey(k[1]))
						}
					}
				}
			} else if len(table) == 2 && table[0] == "mcp_servers" {
				f := fact(table[1])
				if m := tomlStrKeyRe.FindStringSubmatch(t); m != nil {
					f[m[1]] = m[2] + m[3]
				}
				if m := tomlArgsRe.FindStringSubmatch(t); m != nil {
					addArgs(f, m[1])
					argsOf = table[1]
				}
				// Codex's credential fields name variables: bearer_token_env_var, env_vars,
				// env_http_headers (header -> variable).
				if m := tomlBearerRe.FindStringSubmatch(t); m != nil {
					addEnv(f, m[1]+m[2])
				}
				if m := tomlEnvVarsRe.FindStringSubmatch(t); m != nil {
					for _, q := range tomlStrRe.FindAllStringSubmatch(m[1], -1) {
						addEnv(f, q[1]+q[2])
					}
				}
				if m := tomlHdrInlineRe.FindStringSubmatch(t); m != nil {
					for _, kv := range tomlKVStrRe.FindAllStringSubmatch(m[1], -1) {
						addEnv(f, kv[1]+kv[2])
					}
				}
				if m := tomlEnvInlineRe.FindStringSubmatch(t); m != nil {
					for _, k := range tomlKeyRe.FindAllStringSubmatch(m[1], -1) {
						addEnv(f, unquoteKey(k[1]))
					}
				}
			} else if len(table) == 3 && table[0] == "mcp_servers" && table[2] == "env_http_headers" {
				if m := tomlKVStrRe.FindStringSubmatch(t); m != nil {
					addEnv(fact(table[1]), m[1]+m[2])
				}
			} else if len(table) == 3 && table[0] == "mcp_servers" && table[2] == "env" {
				if m := tomlKeyRe.FindStringSubmatch(t); m != nil {
					addEnv(fact(table[1]), unquoteKey(m[1]))
				}
			}
			for _, d := range []string{`"""`, `'''`} {
				if strings.Count(t, d)%2 == 1 {
					inML = d
				}
			}
			if i := strings.Index(t, "="); i >= 0 && inML == "" {
				depth = bracketDelta(t[i+1:])
				if depth <= 0 {
					depth, argsOf = 0, ""
				}
			}
		}
		if len(table) >= 2 && table[0] == "mcp_servers" {
			ln.server = table[1]
			fact(table[1])
		}
		lines = append(lines, ln)
	}
	return lines, facts
}

func isCommentOrBlank(l string) bool {
	t := strings.TrimSpace(l)
	return t == "" || strings.HasPrefix(t, "#")
}

// bracketDelta counts [ minus ] outside quoted strings.
func bracketDelta(s string) int {
	d := 0
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' && quote == '"' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return d
		case c == '[':
			d++
		case c == ']':
			d--
		}
	}
	return d
}

// FilterCodex removes the out-of-scope servers from dir/config.toml. It writes nothing on a
// dry run or when nothing is out of scope.
func FilterCodex(dir string, j *Judge, dryRun bool) ([]Removed, error) {
	if j == nil {
		return nil, nil
	}
	p := filepath.Join(dir, "config.toml")
	raw, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines, facts := scanCodex(string(raw))
	out := map[string]string{}
	for n, f := range facts {
		if why, ok := j.Out(n, f); ok {
			out[n] = why
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	var kept strings.Builder
	text := map[string]*strings.Builder{}
	var order []string
	// A run of a removed server's lines ends with the comments and blank lines before the next
	// table: they are kept (they belong to what follows); the rest of the run is removed.
	var run []line
	runName := ""
	flush := func() {
		end := len(run)
		for end > 0 && isCommentOrBlank(run[end-1].text) {
			end--
		}
		if runName != "" && end > 0 {
			if text[runName] == nil {
				text[runName] = &strings.Builder{}
				order = append(order, runName)
			}
			for _, ln := range run[:end] {
				text[runName].WriteString(ln.text)
			}
		}
		for _, ln := range run[end:] {
			kept.WriteString(ln.text)
		}
		run, runName = nil, ""
	}
	for _, ln := range lines {
		n := ln.server
		if n == "" {
			n = ln.inline
		}
		if _, drop := out[n]; n != "" && drop {
			if runName != n {
				flush()
				runName = n
			}
			run = append(run, ln)
			continue
		}
		flush()
		kept.WriteString(ln.text)
	}
	flush()
	now := time.Now().UTC().Format(time.RFC3339)
	var removed []Removed
	for _, n := range order {
		removed = append(removed, Removed{Where: p, Name: n, Why: out[n], Entry: text[n].String(), At: now})
	}
	if dryRun {
		return removed, nil
	}
	if err := keepRemoved(dir, removed); err != nil {
		return nil, err
	}
	return removed, writeAtomic(p, []byte(kept.String()))
}
