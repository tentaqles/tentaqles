package humanonly

import "strings"

// cygwinPtyName reports whether a pipe name is a Cygwin or MSYS2 pty, the
// pipes mintty hands to Git Bash and MSYS2 shells in place of a console:
// \msys-<id>-pty<n>-from-master (stdin) and \msys-<id>-pty<n>-to-master
// (stdout), or \cygwin-... (the mattn/go-isatty IsCygwinTerminal rule), with
// or without the \Device\NamedPipe prefix.
func cygwinPtyName(name string) bool {
	t := strings.Split(name, "-")
	if len(t) < 5 {
		return false
	}
	switch strings.ToLower(t[0]) {
	case `\msys`, `\cygwin`, `\device\namedpipe\msys`, `\device\namedpipe\cygwin`:
	default:
		return false
	}
	if t[1] == "" || !strings.HasPrefix(t[2], "pty") {
		return false
	}
	if t[3] != "from" && t[3] != "to" {
		return false
	}
	return t[4] == "master"
}
