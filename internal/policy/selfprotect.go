package policy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

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
	if access == "decode" {
		return "the encoded command could not be read: tq denies a PowerShell -EncodedCommand it cannot decode (a protected path may be its target)"
	}
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
		if call.Tool != "Grep" && call.Tool != "Glob" {
			return protect.Hit{}, "", false
		}
		path = cwd // a Grep/Glob with no path searches the cwd
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
// arguments (bash, cmd and PowerShell names, lower-cased, no .exe). Editors
// open a file for editing and downloaders write their output file, so both
// count as writers of a path argument.
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
	"curl", "wget",
	"vim", "vi", "nvim", "nano", "emacs", "ed", "code", "notepad", "notepad++", "subl", "gedit", "micro",
)

// interpreterVerbs run code. With cwd inside a protected directory, or a
// here-doc / stdin body that names a protected path, they are denied.
var interpreterVerbs = wordSet(
	"python", "python3", "python2", "py", "node", "nodejs", "deno", "bun",
	"perl", "ruby", "php", "bash", "sh", "zsh", "dash", "ksh", "fish", "pwsh", "powershell",
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
	case name == "awk":
		for _, a := range args {
			if strings.Contains(a, "inplace") {
				return classWrite // gawk -i inplace rewrites its file operands
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
		return classNone // findTouch judges it
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

// redirects reads the file redirections of a simple command's words: the
// targets written (>, >>, &>, >|, with or without a space, and glued inside a
// word such as `x>path` or `2>path`) and read (<). A descriptor (`2>&1`) and
// a here-doc (`<<`) are not file targets.
func redirects(words []string) (writes, reads []string) {
	for i := 0; i < len(words); i++ {
		op, rest, ok := splitRedir(words[i])
		if !ok {
			continue
		}
		if rest == "" {
			if i+1 >= len(words) {
				break
			}
			i++
			rest = words[i]
		}
		if rest == "" || strings.HasPrefix(rest, "&") {
			continue // a descriptor (2>&1)
		}
		if op == "<" {
			reads = append(reads, rest)
		} else {
			writes = append(writes, rest)
		}
	}
	return writes, reads
}

// splitRedir finds the first redirection operator in a word and returns it
// with whatever follows it in the word (the glued target, "" when the target
// is the next word). `<<` is a here-doc, not a file redirect.
func splitRedir(w string) (op, target string, ok bool) {
	for i := 0; i < len(w); i++ {
		switch w[i] {
		case '>':
			switch {
			case i+1 < len(w) && w[i+1] == '>':
				return ">>", w[i+2:], true
			case i+1 < len(w) && w[i+1] == '|':
				return ">|", w[i+2:], true
			}
			return ">", w[i+1:], true
		case '<':
			if i+1 < len(w) && w[i+1] == '<' {
				return "", "", false // here-doc
			}
			return "<", w[i+1:], true
		}
	}
	return "", "", false
}

// argPaths yields the paths an argument may name: the argument itself, for
// NAME=path and --flag=path the part after the first =, and for a glued
// output flag (`-o<dir>`, 7z) the part after it.
func argPaths(a string) []string {
	out := []string{a}
	if i := strings.Index(a, "="); i > 0 && i < len(a)-1 {
		out = append(out, a[i+1:])
	}
	if v, ok := strings.CutPrefix(a, "-o"); ok && v != "" && v[0] != '=' {
		out = append(out, v)
	}
	return out
}

// shellTouch judges every simple command of a shell command line, then the
// cross-command routes (a here-doc or stdin fed to an interpreter, `… | xargs
// rm`, a PowerShell .NET call) that one command in isolation does not show.
func shellTouch(set *protect.Set, cwd, command string) (protect.Hit, string, bool) {
	cmds := guard.Commands(command)
	for _, c := range cmds {
		base := commandBase(set, cwd, c)
		if h, access, ok := commandTouch(set, cwd, base, c); ok {
			return h, access, true
		}
	}
	// `… | xargs rm|mv|cp <protected>`: xargs' own words rarely name the
	// path; it comes from the piped input. When a command runs a write verb
	// through xargs, any protected path named literally anywhere in the line
	// is the target.
	for _, c := range cmds {
		if len(c.Words) == 0 || guard.BaseName(c.Words[0]) != "xargs" {
			continue
		}
		if verb, _, ok := guard.Verb(c.Words); ok && writeWords[guard.BaseName(verb)] {
			for _, o := range cmds {
				base := commandBase(set, cwd, o)
				for _, w := range o.Words {
					for _, p := range argPaths(w) {
						if h, ok := set.Match(set.Normalize(p, base)); ok {
							return h, accessWrite, true
						}
					}
				}
			}
		}
	}
	// A here-doc or stdin body fed to an interpreter is code; guard.Commands
	// blanks a data here-doc, so the raw command is scanned for a literal
	// protected path when an interpreter reads a body.
	if interpreterReadsBody(command) {
		if h, ok := set.Mentions(command); ok {
			return h, "use", true
		}
	}
	// A .NET static call in PowerShell ([IO.File]::WriteAllText('…')) takes
	// its path inside parentheses, which the tokenizer splits off as its own
	// segment: the whole line (data here-docs blanked) is scanned instead.
	if text := guard.BlankDataHeredocs(command); strings.Contains(text, "]::") {
		if h, ok := set.Mentions(text); ok {
			return h, "use", true
		}
	}
	return protect.Hit{}, "", false
}

// commandBase is the directory one command runs in: cwd, the leading cd
// target, or "" when a cd tq cannot follow ran first (a relative path is
// then unplaceable; an absolute one still resolves).
func commandBase(set *protect.Set, cwd string, c guard.Command) string {
	switch {
	case c.BaseUnknown:
		return ""
	case c.Base != "":
		return set.Normalize(c.Base, cwd)
	}
	return cwd
}

// commandTouch judges one simple command.
func commandTouch(set *protect.Set, cwd, base string, c guard.Command) (protect.Hit, string, bool) {
	match := func(p string) (protect.Hit, bool) { return set.Match(set.Normalize(p, base)) }
	// A cd into a protected folder: a write verb there writes it even with no
	// path argument (cd ~/.tentaqles && git init), and an interpreter run
	// there reads or writes it.
	var baseHit protect.Hit
	baseProtected := false
	if c.Base != "" && base != "" {
		baseHit, baseProtected = set.Match(base)
		// The binary's directory may be a shared bin folder: only a named tq
		// file there is protected, never every write in it.
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
		return protect.Hit{}, "", false
	}
	name := guard.BaseName(verb)
	// A PowerShell -EncodedCommand carries a base64 UTF-16LE script: decode
	// it and run the rule on the text.
	if name == "powershell" || name == "pwsh" {
		if dec, present, decoded := encodedPowerShell(args); present {
			if !decoded {
				return protect.Hit{Path: "the command", What: "an encoded PowerShell command tq could not read"}, "decode", true
			}
			if h, a, ok := shellTouch(set, base, dec); ok {
				return h, a, true
			}
		}
	}
	// eval / iex run a literal string as a command: re-run the rule on it.
	if name == "eval" || name == "iex" || name == "invoke-expression" {
		for _, a := range args {
			if h, acc, ok := shellTouch(set, base, a); ok {
				return h, acc, true
			}
		}
	}
	if name == "find" {
		return findTouch(set, base, args)
	}
	if name == "git" {
		if h, access, ok := gitTouch(c.Words, match); ok {
			return h, access, true
		}
		if baseProtected && gitWrites(c.Words) {
			return baseHit, accessWrite, true
		}
		return protect.Hit{}, "", false
	}
	class := verbClass(name, args)
	if baseProtected && (class == classWrite || class == classInline || (class == classRead && baseHit.ReadDenied())) {
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
	return protect.Hit{}, "", false
}

// findTouch judges a find invocation: a destructive action (-delete, -exec
// and friends) rooted at a directory that is, contains or is contained by a
// protected path, whose -name/-path can match a protected file (or with no
// -name at all), is denied.
func findTouch(set *protect.Set, base string, args []string) (protect.Hit, string, bool) {
	destructive := false
	var dirs, names []string
	sawPred := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-delete", a == "-exec", a == "-execdir", a == "-ok", a == "-okdir":
			destructive, sawPred = true, true
		case a == "-name" || a == "-iname" || a == "-path" || a == "-ipath" || a == "-wholename" || a == "-iwholename":
			sawPred = true
			if i+1 < len(args) {
				names = append(names, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "-"):
			sawPred = true
		default:
			if !sawPred {
				dirs = append(dirs, a) // a start directory precedes the predicates
			}
		}
	}
	if !destructive {
		return protect.Hit{}, "", false
	}
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	for _, d := range dirs {
		if hit, ok := set.FindReaches(set.Normalize(d, base), names); ok {
			return hit, accessWrite, true
		}
	}
	return protect.Hit{}, "", false
}

// encodedPowerShell finds a -EncodedCommand / -enc / -e argument and decodes
// its base64 UTF-16LE value. present reports the flag was seen; decoded
// reports the value parsed.
func encodedPowerShell(args []string) (script string, present, decoded bool) {
	for i := 0; i+1 < len(args); i++ {
		a := strings.ToLower(args[i])
		if a == "-encodedcommand" || a == "-enc" || a == "-e" || a == "-ec" || a == "-encoded" {
			present = true
			raw, err := base64.StdEncoding.DecodeString(args[i+1])
			if err != nil || len(raw)%2 != 0 {
				return "", true, false
			}
			u := make([]uint16, len(raw)/2)
			for j := range u {
				u[j] = uint16(raw[2*j]) | uint16(raw[2*j+1])<<8
			}
			return string(utf16.Decode(u)), true, true
		}
	}
	return "", false, false
}

// interpreterReadsBody reports whether the command feeds a here-doc or stdin
// to an interpreter (`python - <<EOF`, `cat x | sh`, `bash <<EOF`), whose body
// is code the per-command pass does not see (a data here-doc is blanked).
func interpreterReadsBody(command string) bool {
	hasHeredoc := strings.Contains(command, "<<")
	for _, c := range guard.Commands(command) {
		verb, args, ok := guard.Verb(c.Words)
		if !ok {
			continue
		}
		if !interpreterVerbs[guard.BaseName(verb)] {
			continue
		}
		if hasHeredoc {
			return true
		}
		for _, a := range args {
			if a == "-" {
				return true // reads the script from stdin
			}
		}
	}
	return false
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
