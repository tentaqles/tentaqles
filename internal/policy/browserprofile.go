package policy

import (
	"path/filepath"
	"regexp"
	"strings"
)

// The identity category's browser rule (ADR-0016): a client that lists browsers: in its
// manifest opens pages in its own browser profile, so a browser started by hand with another
// profile, or none (Chrome then picks the last-used one), is denied, and so is every route that
// hands a URL to the OS's default handler. `tentaqles open <url>` is the way through. Without a
// browsers: list nothing here applies.
//
// Like the rest of the hook it is a text filter on literal spellings (AR2).

// OtherBrowserProfileID is the rule's id: BLOCKED [identity/other-browser-profile].
const OtherBrowserProfileID = "other-browser-profile"

// BrowserProfile is one browser of the client: the manifest's browsers: entry (name chrome or
// edge, and the profile folder name).
type BrowserProfile struct {
	Name    string
	Profile string
}

// browserExes maps the program names the rule knows to the manifest's browser name.
var browserExes = map[string]string{
	"chrome": "chrome", "google-chrome": "chrome", "google-chrome-stable": "chrome",
	"msedge": "edge", "microsoft-edge": "edge", "microsoft-edge-stable": "edge",
}

// browserKind is the manifest name of the browser a command word starts, "" when it is none.
func browserKind(word string) string {
	return browserExes[exeName(word)]
}

var webURLRe = regexp.MustCompile(`(?i)^https?://`)

func isWebURLWord(w string) bool {
	return webURLRe.MatchString(strings.Trim(w, `"'`))
}

func (s *Scope) listed(kind string) (BrowserProfile, bool) {
	for _, b := range s.browsers {
		if b.Name == kind {
			return b, true
		}
	}
	return BrowserProfile{}, false
}

func (s *Scope) browserList() string {
	parts := make([]string, 0, len(s.browsers))
	for _, b := range s.browsers {
		parts = append(parts, b.Name+" (profile "+b.Profile+")")
	}
	return strings.Join(parts, ", ")
}

const openHint = "Open a page with `tentaqles open <url>`, which uses this client's browser profile"

func (s *Scope) browserHit(subject, what string) scopeHit {
	return scopeHit{id: OtherBrowserProfileID, subject: subject, reason: what + ". " + openHint + " (" + s.browserList() +
		"). tq denies it in every tool; do not retry another way: tell the developer"}
}

// flagValues reads every value of a browser flag in args: --flag=value, and --flag value; one
// or two leading dashes. Quotes are removed.
func flagValues(args []string, flag string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := strings.Trim(args[i], `"'`)
		l := strings.ToLower(a)
		for _, p := range []string{"--" + flag, "-" + flag} {
			if l == p {
				if i+1 < len(args) {
					out = append(out, strings.Trim(args[i+1], `"'`))
					i++
				} else {
					out = append(out, "")
				}
				break
			}
			if strings.HasPrefix(l, p+"=") {
				out = append(out, strings.Trim(a[len(p)+1:], `"'`))
				break
			}
		}
	}
	return out
}

func (s *Scope) sameProfile(a, b string) bool {
	if s.ci {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// browserStart judges a browser program started with args: allowed only for a browser the
// client lists, with the client's profile (--profile-directory), or with a user data folder
// under the client's own browser folder.
func (s *Scope) browserStart(cwd, word string, args []string) (scopeHit, bool) {
	kind := browserKind(word)
	if kind == "" || len(s.browsers) == 0 {
		return scopeHit{}, false
	}
	name := exeName(word)
	mine, ok := s.listed(kind)
	if !ok {
		return s.browserHit(name, name+" is a "+kind+" browser, and "+kind+" is not one of this client's browsers"), true
	}
	profiles := flagValues(args, "profile-directory")
	dirs := flagValues(args, "user-data-dir")
	for _, p := range profiles {
		if !s.sameProfile(p, mine.Profile) {
			return s.browserHit(name, name+" would open profile "+quoteOrNone(p)+", not this client's profile "+mine.Profile), true
		}
	}
	if len(dirs) > 0 {
		own := s.browserDir
		for _, d := range dirs {
			if own == "" || s.set == nil || !within(own, s.norm(d, cwd)) {
				return s.browserHit(name, name+" would use the user data folder "+quoteOrNone(d)+", not this client's"), true
			}
		}
		return scopeHit{}, false
	}
	if len(profiles) == 0 {
		return s.browserHit(name, name+" started with no --profile-directory opens whichever profile was used last"), true
	}
	return scopeHit{}, false
}

func quoteOrNone(v string) string {
	if v == "" {
		return "(none)"
	}
	return `"` + v + `"`
}

// urlRoute judges the OS routes that hand a URL to the default handler (start, Start-Process,
// explorer, rundll32 url.dll) and a browser named behind start or Start-Process. words is what
// follows the verb: for start and Start-Process the target and its arguments.
func (s *Scope) urlRoute(cwd, verb string, words []string) (scopeHit, bool) {
	if len(s.browsers) == 0 {
		return scopeHit{}, false
	}
	switch verb {
	case "start", "start-process", "saps":
		for i, w := range words {
			if browserKind(w) != "" {
				return s.browserStart(cwd, w, words[i+1:])
			}
		}
		if len(words) > 0 && (isWebURLWord(words[0]) || len(words) > 1 && isWebURLWord(words[1])) {
			return s.browserHit(verb, verb+" would hand the URL to the system's default browser"), true
		}
	case "explorer":
		for _, w := range words {
			if isWebURLWord(w) {
				return s.browserHit(verb, verb+" would hand the URL to the system's default browser"), true
			}
		}
	case "rundll32":
		for _, w := range words {
			if strings.Contains(strings.ToLower(w), "url.dll") {
				return s.browserHit(verb, "rundll32 url.dll would hand the URL to the system's default browser"), true
			}
		}
	}
	return scopeHit{}, false
}

// routeWords are the words of a command that urlRoute reads, by verb.
func (s *Scope) routeWords(verb string, args []string) []string {
	if len(s.browsers) == 0 {
		return nil
	}
	switch verb {
	case "start":
		return cmdStartTargets(args)
	case "start-process", "saps":
		return startWords(args)
	}
	return args
}

// startWords are the words of a Start-Process line that name what runs: -FilePath and the
// -ArgumentList words, and the positional words, in order; switches and the values of the
// other options are left out.
func startWords(args []string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			opt, val, colon := strings.Cut(a, ":")
			kind := startProcessOpt(opt)
			if kind == "" {
				continue
			}
			if !colon {
				if i+1 >= len(args) {
					continue
				}
				i++
				val = args[i]
			}
			switch kind {
			case "exe":
				out = append(out, val)
			case "args":
				out = append(out, splitArgList(val)...)
			}
			continue
		}
		if strings.Trim(a, `"'`) != "" {
			out = append(out, a)
		}
	}
	return out
}

// splitArgList splits a Start-Process -ArgumentList value at commas and white space outside
// quotes, with the quotes removed: `--profile-directory="Profile 2" https://x` is two words.
func splitArgList(v string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	have := false
	flush := func() {
		if have {
			out = append(out, cur.String())
		}
		cur.Reset()
		have = false
	}
	for _, r := range v {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			quote, have = r, true
		case r == ',' || r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			have = true
		}
	}
	flush()
	return out
}

// browserMCP judges an mcp__claude-in-chrome__ call: Claude in Chrome drives a Chrome that is
// already running, in whatever profile it has, so a client with no Chrome of its own is denied.
// A client that lists Chrome is allowed; tq cannot tell which profile that Chrome is in
// (docs/COMMANDS.md).
func (s *Scope) browserMCP(server string) (scopeHit, bool) {
	if len(s.browsers) == 0 || strings.ToLower(strings.ReplaceAll(server, "_", "-")) != "claude-in-chrome" {
		return scopeHit{}, false
	}
	if _, ok := s.listed("chrome"); ok {
		return scopeHit{}, false
	}
	return s.browserHit("claude-in-chrome", "Claude in Chrome drives a Chrome that is already running, and Chrome is not one of this client's browsers"), true
}

// browserDirFor is the canonical form of the client's own browser user data folder.
func browserDirFor(s *Scope, dir string) string {
	if dir == "" || s == nil || s.set == nil {
		return ""
	}
	return s.set.Canon(filepath.Clean(dir))
}
