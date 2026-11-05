package policy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/guard"
	"github.com/tentaqles/tentaqles/internal/protect"
)

// The third guard category, self-protection (PRD v2 §7.1, §8.1, §8.2;
// ADR-0005): two rules, evaluated after blocked_commands, identity and
// secrets (§6.3), both procedural because they read paths and command
// words, not a regex.

// Categories of the guard (PRD v2 §7.1). A deny reads
// BLOCKED [<category>/<rule>].
const (
	CategoryIdentity       = "identity"
	CategorySecrets        = "secrets"
	CategorySelfProtection = "self-protection"
)

// The self-protection rule ids.
const (
	// ProtectedPathID denies a read, write, edit, redirect, copy, move,
	// delete, link or permission change that targets a protected path, in
	// a file tool, an MCP input or a shell command.
	ProtectedPathID = "tq/protected-path"
	// HumanOnlyCommandID denies a shell command that runs a human-only tq
	// verb (PRD v2 §8.1).
	HumanOnlyCommandID = "tq/human-only-command"
)

// SelfProtection evaluates the self-protection rules on call: set is the
// protected path set of this hook run and cwd the directory the call runs
// in. It returns the deny, or the zero Decision.
func SelfProtection(set *protect.Set, cwd string, call ToolCall) Decision {
	if set == nil {
		return Decision{}
	}
	if hit, access, ok := protectedTouch(set, cwd, call); ok {
		return selfProtectionDeny(ProtectedPathID, protectedReason(hit, access))
	}
	if call.IsShell() {
		if what, ok := KeychainWrite(call.Command); ok {
			return selfProtectionDeny(ProtectedPathID, what+" writes tq's entry in the OS keychain (service "+KeychainService+": the integrity key and the tq secret tokens), a protected location: tq denies every write there; a human changes it with the tq command in a terminal")
		}
		if what, ok := HumanOnlyInvocation(call.Command); ok {
			return selfProtectionDeny(HumanOnlyCommandID, what+" is human-only: run it yourself in a terminal (tq refuses an agent session and confirms before it writes)")
		}
	}
	return Decision{}
}

// KeychainService is the OS keychain service tq keeps its entries under:
// the integrity key (P1.4) and the tq secret tokens (internal/secret).
const KeychainService = "tentaqles"

// KeychainWrite reports whether a shell command line writes or deletes an
// OS keychain entry of KeychainService: Windows `cmdkey /generic:` /
// `/add:` / `/delete:` with a target naming it (go-keyring's target is
// service:account), macOS `security add-generic-password` /
// `delete-generic-password` with `-s` naming it, Linux `secret-tool store`
// / `clear` with the attribute `service` naming it. what names the tool.
// Reading an entry is the secret rules' business, not this one's.
func KeychainWrite(command string) (what string, ok bool) {
	names := func(v string) bool {
		return strings.Contains(strings.ToLower(strings.Trim(v, `"'`)), KeychainService)
	}
	for _, c := range guard.Commands(command) {
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		switch guard.BaseName(verb) {
		case "cmdkey":
			for _, a := range args {
				l := strings.ToLower(a)
				for _, op := range []string{"/generic:", "/add:", "/delete:"} {
					if strings.HasPrefix(l, op) && names(a[len(op):]) {
						return "cmdkey " + op[:len(op)-1], true
					}
				}
			}
		case "security":
			if len(args) == 0 {
				continue
			}
			sub := strings.ToLower(args[0])
			if sub != "add-generic-password" && sub != "delete-generic-password" {
				continue
			}
			for i := 1; i < len(args); i++ {
				switch {
				case args[i] == "-s" && i+1 < len(args) && names(args[i+1]):
					return "security " + sub, true
				case strings.HasPrefix(args[i], "-s") && len(args[i]) > 2 && names(args[i][2:]):
					return "security " + sub, true
				}
			}
		case "secret-tool":
			if len(args) == 0 {
				continue
			}
			sub := strings.ToLower(args[0])
			if sub != "store" && sub != "clear" {
				continue
			}
			for i := 1; i+1 < len(args); i++ {
				if strings.EqualFold(args[i], "service") && names(args[i+1]) {
					return "secret-tool " + sub, true
				}
			}
		}
	}
	return "", false
}

func selfProtectionDeny(id, reason string) Decision {
	return Decision{Action: Deny, Matched: []*Rule{{ID: id, Category: CategorySelfProtection, Action: Deny, Reason: reason, Source: "builtin"}}}
}

func protectedReason(hit protect.Hit, access string) string {
	if hit.ReadDenied() {
		return fmt.Sprintf("%s is %s, a protected path: tq denies every read, write, copy, move and delete there (this call would %s it); a human changes it with the tq command in a terminal", hit.Path, hit.What, access)
	}
	return fmt.Sprintf("%s is %s, a protected path: tq denies every write, copy, move and delete there (this call would %s it; reading it is allowed); a human changes it with the tq command in a terminal", hit.Path, hit.What, access)
}

// Accesses a call makes on a path.
const (
	accessRead  = "read"
	accessWrite = "write"
)

// fileToolAccess says what a file tool does to its path.
func fileToolAccess(tool string) (string, bool) {
	switch tool {
	case "Read", "Grep", "Glob", "NotebookRead":
		return accessRead, true
	case "Write", "Edit", "MultiEdit", "NotebookEdit":
		return accessWrite, true
	}
	return "", false
}

// protectedTouch finds the first protected path call touches in a denied
// way, and how.
func protectedTouch(set *protect.Set, cwd string, call ToolCall) (protect.Hit, string, bool) {
	switch {
	case call.IsShell():
		return shellTouch(set, cwd, call.Command)
	case call.IsMCP():
		return mcpTouch(set, cwd, call.Content)
	}
	access, ok := fileToolAccess(call.Tool)
	if !ok {
		return protect.Hit{}, "", false
	}
	path := call.Path
	if strings.TrimSpace(path) == "" {
		if call.Tool != "Grep" {
			return protect.Hit{}, "", false
		}
		path = cwd // a Grep with no path searches the cwd
	}
	if hit, ok := set.Match(set.Normalize(path, cwd)); ok && (access == accessWrite || hit.ReadDenied()) {
		return hit, access, true
	}
	return protect.Hit{}, "", false
}

// mcpTouch reads every string of an MCP tool input that looks like a path;
// any protected one is denied (the server may read or write it).
func mcpTouch(set *protect.Set, cwd, raw string) (protect.Hit, string, bool) {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return protect.Hit{}, "", false
	}
	var found protect.Hit
	ok := false
	walkStrings(v, func(s string) bool {
		if len(s) > 1024 || strings.ContainsAny(s, "\n\r") || !strings.ContainsAny(s, `/\~$%`) {
			return true
		}
		if h, hit := set.Match(set.Normalize(s, cwd)); hit {
			found, ok = h, true
			return false
		}
		return true
	})
	return found, accessWrite, ok
}

func walkStrings(v any, visit func(string) bool) bool {
	switch x := v.(type) {
	case string:
		return visit(x)
	case []any:
		for _, e := range x {
			if !walkStrings(e, visit) {
				return false
			}
		}
	case map[string]any:
		for _, e := range x {
			if !walkStrings(e, visit) {
				return false
			}
		}
	}
	return true
}

// Verb classes of the shell rule.
const (
	classNone   = iota
	classWrite  // every argument is a path the command may change
	classRead   // every argument is a path the command prints
	classInline // the argument after the inline-code flag is code
)

// writeWords change, copy, move, delete, link or re-permission their
// arguments (bash, cmd and PowerShell names, lower-cased, no .exe).
var writeWords = wordSet(
	"cp", "copy", "copy-item", "cpi", "xcopy", "robocopy",
	"mv", "move", "move-item", "mi", "ren", "rename", "rename-item", "rni",
	"rm", "del", "erase", "remove-item", "ri", "rmdir", "rd", "unlink", "shred",
	"mkdir", "md", "new-item", "ni", "touch", "install", "dd", "rsync", "scp",
	"chmod", "chown", "chgrp", "icacls", "cacls", "attrib", "chattr", "chflags", "takeown", "setfacl", "set-acl",
	"ln", "mklink", "truncate",
	"tee", "tee-object", "out-file", "set-content", "sc", "add-content", "ac", "clear-content", "clc",
	"set-item", "clear-item", "set-itemproperty", "remove-itemproperty", "rename-itemproperty", "new-itemproperty",
	"tar", "zip", "unzip", "7z",
)

// readWords print their arguments.
var readWords = wordSet(
	"cat", "type", "more", "less", "head", "tail", "bat", "nl", "strings", "xxd", "od",
	"grep", "rg", "egrep", "fgrep", "findstr", "gc", "get-content", "select-string", "sls",
	"awk", "cut", "sort", "uniq", "wc", "diff", "cmp", "comm", "jq", "yq",
	"source", ".", "bash", "sh", "zsh", "dash", "ksh", "fish", "pwsh", "powershell",
)

// inlineFlags name, per interpreter, the flag whose next argument is code.
var inlineFlags = map[string][]string{
	"python": {"-c"}, "python3": {"-c"}, "python2": {"-c"}, "py": {"-c"},
	"node": {"-e", "--eval", "-p", "--print"}, "nodejs": {"-e", "--eval", "-p", "--print"},
	"deno": {"eval"}, "bun": {"-e", "--eval"},
	"perl": {"-e", "-E"}, "ruby": {"-e"}, "php": {"-r"},
}

func wordSet(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

// verbClass classifies a command word with its arguments.
func verbClass(name string, args []string) int {
	switch {
	case name == "sed":
		for _, a := range args {
			if strings.HasPrefix(a, "-i") || strings.HasPrefix(a, "--in-place") {
				return classWrite
			}
		}
		return classRead
	case name == "perl":
		for _, a := range args {
			if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i") && !strings.Contains(a, "e") && !strings.Contains(a, "E") {
				return classWrite // -i, -pi, -ni: in place
			}
		}
		return classInline
	case name == "find":
		for _, a := range args {
			switch a {
			case "-delete", "-exec", "-execdir", "-ok", "-okdir":
				return classWrite
			}
		}
		return classNone
	case writeWords[name]:
		return classWrite
	case readWords[name]:
		return classRead
	}
	if _, ok := inlineFlags[name]; ok {
		return classInline
	}
	return classNone
}

// redirects reads the file redirections of a simple command: the targets
// written (>, >>, 1>, 2>, &>, >|, with or without a space) and read (<).
func redirects(words []string) (writes, reads []string) {
	take := func(w string) (op, rest string) {
		for _, o := range []string{">>", "&>", ">|", "1>", "2>", ">", "<"} {
			if strings.HasPrefix(w, o) {
				return o, w[len(o):]
			}
		}
		return "", ""
	}
	for i := 0; i < len(words); i++ {
		op, rest := take(words[i])
		if op == "" {
			continue
		}
		if rest == "" {
			if i+1 >= len(words) {
				break
			}
			i++
			rest = words[i]
		}
		if rest == "" || strings.HasPrefix(rest, "&") || strings.HasPrefix(rest, "<") {
			continue // a descriptor (2>&1) or a heredoc
		}
		if op == "<" {
			reads = append(reads, rest)
		} else {
			writes = append(writes, rest)
		}
	}
	return writes, reads
}

// argPaths yields the paths an argument may name: the argument itself and,
// for NAME=path and --flag=path, the part after the first =.
func argPaths(a string) []string {
	out := []string{a}
	if i := strings.Index(a, "="); i > 0 && i < len(a)-1 {
		out = append(out, a[i+1:])
	}
	return out
}

// shellTouch judges every simple command of a shell command line.
func shellTouch(set *protect.Set, cwd, command string) (protect.Hit, string, bool) {
	for _, c := range guard.Commands(command) {
		base := cwd
		switch {
		case c.BaseUnknown:
			base = "" // a relative path cannot be placed; absolute ones still are
		case c.Base != "":
			base = set.Normalize(c.Base, cwd)
		}
		match := func(p string) (protect.Hit, bool) { return set.Match(set.Normalize(p, base)) }
		// A cd into a protected folder: a write verb there writes it even
		// with no path argument (cd ~/.tentaqles && git init).
		var baseHit protect.Hit
		baseProtected := false
		if c.Base != "" && base != "" {
			baseHit, baseProtected = set.Match(base)
			// The binary's directory may be a shared bin folder: only a
			// named tq file there is protected, never every write in it.
			baseProtected = baseProtected && baseHit.Kind != protect.KindBinary
		}

		writes, reads := redirects(c.Words)
		for _, t := range writes {
			if h, ok := match(t); ok {
				return h, accessWrite, true
			}
		}
		for _, t := range reads {
			if h, ok := match(t); ok && h.ReadDenied() {
				return h, accessRead, true
			}
		}
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		name := guard.BaseName(verb)
		if name == "git" {
			if h, access, ok := gitTouch(c.Words, match); ok {
				return h, access, true
			}
			if baseProtected && gitWrites(c.Words) {
				return baseHit, accessWrite, true
			}
			continue
		}
		class := verbClass(name, args)
		if baseProtected && (class == classWrite || (class == classRead && baseHit.ReadDenied())) {
			access := accessWrite
			if class == classRead {
				access = accessRead
			}
			return baseHit, access, true
		}
		switch class {
		case classWrite:
			for _, a := range args {
				for _, p := range argPaths(a) {
					if h, ok := match(p); ok {
						return h, accessWrite, true
					}
				}
			}
		case classRead:
			for _, a := range args {
				for _, p := range argPaths(a) {
					if h, ok := match(p); ok && h.ReadDenied() {
						return h, accessRead, true
					}
				}
			}
		case classInline:
			flags := inlineFlags[name]
			for i := 0; i+1 < len(args); i++ {
				for _, f := range flags {
					if args[i] == f {
						if h, ok := set.Mentions(args[i+1]); ok {
							return h, "use", true
						}
					}
				}
			}
		}
	}
	// A .NET static call in PowerShell ([IO.File]::WriteAllText('…'),
	// [System.IO.Directory]::Delete(…)) takes its path inside parentheses,
	// which the tokenizer splits off as a separate segment: the whole line
	// (data heredocs blanked) is scanned for a protected path instead. Read
	// or write cannot be told apart there, so any protected path is denied.
	if text := guard.BlankDataHeredocs(command); strings.Contains(text, "]::") {
		if h, ok := set.Mentions(text); ok {
			return h, "use", true
		}
	}
	return protect.Hit{}, "", false
}

// gitWriteSubs are the git sub-commands that write the work tree, the
// repository or a file they name; every other sub-command only reads.
var gitWriteSubs = wordSet(
	"init", "clone", "checkout", "switch", "restore", "reset", "rm", "mv", "clean",
	"apply", "am", "stash", "merge", "pull", "rebase", "cherry-pick", "revert",
	"worktree", "read-tree", "checkout-index", "update-index", "config", "archive",
	"bundle", "format-patch", "add", "commit", "submodule", "sparse-checkout", "gc",
	"fetch", "tag", "branch", "notes", "replace", "update-ref", "symbolic-ref",
)

// gitTouch judges one git call: its global options (-C, --git-dir,
// --work-tree, -c, --config-env) name the repository it acts on, so a
// protected path there is denied whatever the sub-command; a path argument
// of a sub-command that writes is a write, of one that reads a read.
func gitTouch(words []string, match func(string) (protect.Hit, bool)) (protect.Hit, string, bool) {
	inv, ok := guard.ParseGitWords(words)
	start := 0 // the git word, after any assignment or wrapper
	for start < len(words) && !guard.IsGitWord(words[start]) {
		start++
	}
	end := len(words)
	if ok {
		end = len(words) - len(inv.Args) - 1 // the sub-command
	}
	var globals []string
	if start+1 <= end {
		globals = words[start+1 : end]
	}
	for _, w := range globals {
		for _, p := range argPaths(w) {
			if h, ok := match(p); ok {
				return h, accessWrite, true
			}
		}
	}
	if !ok {
		return protect.Hit{}, "", false
	}
	write := gitWriteSubs[strings.ToLower(inv.Sub)]
	for _, a := range inv.Args {
		for _, p := range argPaths(a) {
			h, ok := match(p)
			switch {
			case !ok:
			case write:
				return h, accessWrite, true
			case h.ReadDenied():
				return h, accessRead, true
			}
		}
	}
	return protect.Hit{}, "", false
}

// gitWrites reports whether a git call's sub-command writes.
func gitWrites(words []string) bool {
	inv, ok := guard.ParseGitWords(words)
	return ok && gitWriteSubs[strings.ToLower(inv.Sub)]
}

// HumanOnlyInvocation reports whether a shell command line runs a
// human-only tq verb (PRD v2 §8.1): tq, tq.exe, a path to either, or
// `go run ./cmd/tq`, through any wrapper or nested shell the tokenizer
// follows. what names the invocation (tq allow, tq hooks install, ...).
func HumanOnlyInvocation(command string) (what string, ok bool) {
	for _, c := range guard.Commands(command) {
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		name := guard.BaseName(verb)
		if name == "go" && len(args) >= 2 && strings.EqualFold(args[0], "run") && isTqPackage(args[1]) {
			name, args = "tq", args[2:]
		}
		if name != "tq" {
			continue
		}
		if what, ok := humanOnlyVerb(args); ok {
			return what, true
		}
	}
	return "", false
}

func isTqPackage(a string) bool {
	p := strings.TrimSuffix(strings.ToLower(strings.ReplaceAll(a, `\`, "/")), "/")
	return p == "cmd/tq" || strings.HasSuffix(p, "/cmd/tq")
}

// humanOnlyVerb reads tq's arguments: positional words and flags.
func humanOnlyVerb(args []string) (string, bool) {
	var pos []string
	flags := map[string]bool{}
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags[strings.ToLower(a)] = true
			continue
		}
		pos = append(pos, strings.ToLower(a))
	}
	if len(pos) == 0 || flags["--help"] || flags["-h"] {
		return "", false
	}
	verb := pos[0]
	sub := ""
	if len(pos) > 1 {
		sub = pos[1]
	}
	switch verb {
	case "init", "add", "edit", "off", "allow", "deny", "trust", "setup", "onboard", "uninstall", "login":
		return "tq " + verb, true
	case "hooks":
		switch {
		case sub == "install" || sub == "remove":
			return "tq hooks " + sub, true
		case sub == "verify" && flags["--accept"]:
			return "tq hooks verify --accept", true
		}
	case "settings":
		switch {
		case sub == "render" && !flags["--dry-run"]:
			return "tq settings render", true
		case sub == "anchors" && len(pos) > 2 && (pos[2] == "add" || pos[2] == "remove"):
			return "tq settings anchors " + pos[2], true
		}
	case "bundle":
		if sub == "sync" || sub == "capture" {
			return "tq bundle " + sub, true
		}
	case "migrate":
		if flags["--apply"] {
			if sub == "manifests" {
				return "tq migrate manifests --apply", true
			}
			return "tq migrate --apply", true
		}
	case "secret":
		if sub == "set" || sub == "rm" {
			return "tq secret " + sub, true
		}
	case "providers":
		if sub == "add" {
			return "tq providers add", true
		}
	case "run":
		if flags["--off"] {
			return "tq run --off", true
		}
	case "worktrees":
		if sub == "prune" {
			return "tq worktrees prune", true
		}
	}
	return "", false
}
