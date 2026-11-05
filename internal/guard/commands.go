package guard

import "strings"

// This file exports the hook's tokenizer to the self-protection rules
// (internal/policy): the same quote-aware segment split, word split, cd
// tracking and nested-shell reading the git-invocation reader uses, so the
// hook has one tokenizer and not two.

// Command is one simple command of a shell command line, as the hook's
// rules read it: its words with quotes removed, and the directory a
// preceding cd put it in.
type Command struct {
	Words []string
	// Base is the directory a preceding cd (in any earlier segment) put the
	// command in, as written: "" for the session cwd; relative to the cwd,
	// or to the previous cd, when the cd was relative.
	Base string
	// BaseUnknown: a cd tq cannot follow (cd -, cd $X, popd, cd /d x) ran
	// before, so a relative path cannot be placed.
	BaseUnknown bool
	// Nested: found inside a nested shell (bash -c, cmd /c, pwsh -c, iex).
	Nested bool
}

// Commands splits a command line into its simple commands: quote-aware on
// the separators of sepPattern, following nested shells (bash -c, sh -c,
// cmd /c, pwsh -Command, Invoke-Expression) to three levels, tracking cd.
// A heredoc body that is data (BlankDataHeredocs) is not read; one fed to a
// shell is. The wrapper of a nested shell is returned too, before the
// commands inside it.
func Commands(c string) []Command {
	return commandsDepth(BlankDataHeredocs(c), "", false, 0)
}

func commandsDepth(c, base string, baseUnknown bool, depth int) []Command {
	var out []Command
	masked := maskQuoted(c)
	for _, sp := range segmentSpans(masked) {
		words := splitWords(c[sp.start:sp.end])
		if len(words) == 0 {
			continue
		}
		out = append(out, Command{Words: words, Base: base, BaseUnknown: baseUnknown, Nested: depth > 0})
		if dir, ok := cdTarget(words); ok {
			if dir == "" {
				baseUnknown = true
			} else {
				base = joinBase(base, dir)
			}
			continue
		}
		if inner, ok := nestedCommand(words); ok && depth < 3 {
			out = append(out, commandsDepth(inner, base, baseUnknown, depth+1)...)
		}
	}
	return out
}

// joinBase composes a later cd with the base before it, textually: an
// absolute, drive-absolute, ~ or variable target replaces the base.
func joinBase(base, dir string) string {
	if base == "" || isAbsLike(dir) {
		return dir
	}
	return base + "/" + dir
}

func isAbsLike(p string) bool {
	if len(p) >= 2 && p[1] == ':' {
		return true
	}
	switch {
	case strings.HasPrefix(p, "/"), strings.HasPrefix(p, `\`), strings.HasPrefix(p, "~"),
		strings.HasPrefix(p, "$"), strings.HasPrefix(p, "%"):
		return true
	}
	return false
}

// Verb reads a simple command's words the way the git reader does: leading
// NAME=value assignments and wrappers (env, command, builtin, exec, sudo,
// doas, nice, nohup, time, timeout, xargs, stdbuf, the PowerShell call
// operator &) are skipped with their own options, and the command word is
// returned with quotes removed, followed by the words after it.
func Verb(words []string) (verb string, args []string, ok bool) {
	i := 0
	for i < len(words) {
		w := words[i]
		if envAssignRe.MatchString(w) {
			i++
			continue
		}
		spec, isWrapper := gitWrappers[strings.ToLower(strings.Trim(w, `"'`))]
		if !isWrapper {
			break
		}
		i++
		for i < len(words) && strings.HasPrefix(words[i], "-") && len(words[i]) > 1 {
			opt := words[i]
			if opt == "--" {
				i++
				break
			}
			if spec.value[opt] {
				i++
			}
			i++
		}
		for n := 0; n < spec.positional && i < len(words); n++ {
			i++
		}
	}
	if i >= len(words) {
		return "", nil, false
	}
	return strings.Trim(words[i], `"'`), words[i+1:], true
}

// BaseName returns the executable name a command word names: its last path
// element, quotes removed, lower-cased, without a .exe suffix.
func BaseName(w string) string {
	return strings.TrimSuffix(strings.ToLower(baseWord(w)), ".exe")
}
