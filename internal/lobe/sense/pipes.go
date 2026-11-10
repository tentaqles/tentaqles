// Ported from Trail-code src/trace/shell.ts at e02b754c1d4e1de82b841522e2d8cae811702870 (MIT).

package sense

import (
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/lobe/shellwords"
)

// filterPrograms are the last programs of a pipe that hide the exit code of the command before them:
// Trail's tail, head, less, more and cat, and grep and tee (ours).
var filterPrograms = []string{"tail", "head", "less", "more", "cat", "grep", "tee"}

// readPrograms are the programs that only read; a command whose first program is one of them is not
// judged by its output (rules B and C; owner, 2026-11-10).
var readPrograms = []string{"cat", "grep", "rg", "head", "tail", "less", "ls", "find", "echo", "sed", "awk", "jq"}

// readGitCommands are the git subcommands that only read.
var readGitCommands = []string{"log", "show", "diff", "status"}

// hidesExit reports whether a command hides the exit code of its first part: a here-document (<<)
// never does; a command that ends in `|| true` does; otherwise its last pipeline (the parts after the
// last separator that is not a pipe) has two or more segments and its last segment is one of
// filterPrograms. The shell redirections are removed first, so `2>&1` is not a separator.
func hidesExit(command string) bool {
	cmd := shellwords.StripRedirects(command)
	if strings.Contains(cmd, "<<") {
		return false
	}
	parts := shellwords.Parts(cmd)
	if n := len(parts); n > 0 && parts[n-1].Sep == "||" && parts[n-1].Text == "true" {
		return true
	}
	start := 0
	for i := 1; i < len(parts); i++ {
		if parts[i].Sep != "|" {
			start = i
		}
	}
	pipeline := parts[start:]
	if len(pipeline) < 2 {
		return false
	}
	return slices.Contains(filterPrograms, shellwords.Program(pipeline[len(pipeline)-1].Text))
}

// firstProgram is the program of the first part of a command that is not a cd (its folder and .exe
// taken off, lower case, as shellwords.Program gives it) and the word after it, or "" when either is
// missing. Leading cd parts are skipped, as GrepFoundNothing skips them, so `cd app && cat x.log` gives
// cat. The shell redirections are removed first.
func firstProgram(command string) (program, next string) {
	for _, part := range shellwords.Parts(shellwords.StripRedirects(command)) {
		words := shellwords.CommandWords(part.Text)
		if len(words) == 0 {
			continue
		}
		program = shellwords.Program(part.Text)
		if program == "cd" {
			continue
		}
		if len(words) > 1 {
			next = words[1]
		}
		return program, next
	}
	return "", ""
}

// readsOnly reports whether a first program only reads: one of readPrograms, or git with a read-only
// subcommand. Hidden skips rules B and C for such a command.
func readsOnly(program, next string) bool {
	if program == "git" {
		return slices.Contains(readGitCommands, next)
	}
	return slices.Contains(readPrograms, program)
}
