package scan

import (
	"encoding/base64"
	"errors"
	"path"
	"strings"
	"unicode/utf16"
)

// PowerShell command lines for the pack guards. A guard that looks through
// shell wrappers reads what pwsh or powershell would run: the -Command
// script, an -EncodedCommand decoded from base64 UTF-16LE, or Windows
// PowerShell's bare command. A form whose script the guard cannot see (a
// script file, stdin, an encoded command that does not decode) is opaque,
// and the guard asks (fail closed).

// PwshRun is what one pwsh/powershell invocation runs.
type PwshRun struct {
	// Script is the inline script, when there is one.
	Script string
	// Encoded is set when Script came from -EncodedCommand.
	Encoded bool
	// Opaque names the form when the script cannot be read ("" otherwise).
	Opaque string
}

// IsPowerShell reports whether a program word is pwsh or powershell (any
// directory, any case, with or without .exe).
func IsPowerShell(word string) bool {
	switch pwshName(word) {
	case "pwsh", "powershell", "pwsh-preview":
		return true
	}
	return false
}

func pwshName(word string) string {
	w := strings.ToLower(path.Base(strings.ReplaceAll(word, `\`, "/")))
	return strings.TrimSuffix(w, ".exe")
}

// pwshValueFlags take a value (pwsh 7 and Windows PowerShell 5.1).
var pwshValueFlags = map[string]bool{
	"-executionpolicy": true, "-ex": true, "-ep": true,
	"-windowstyle": true, "-w": true, "-win": true,
	"-configurationname": true, "-config": true, "-configurationfile": true,
	"-workingdirectory": true, "-wd": true, "-wo": true,
	"-version": true, "-v": true,
	"-inputformat": true, "-inp": true, "-if": true,
	"-outputformat": true, "-o": true, "-of": true, "-out": true,
	"-psconsolefile": true, "-settingsfile": true, "-settings": true,
	"-custompipename": true, "-encodedarguments": true, "-ea": true,
}

// pwshSwitches take no value.
var pwshSwitches = []string{"-noprofile", "-nologo", "-noninteractive", "-noexit", "-mta", "-sta", "-help", "-login", "-interactive", "-nop", "-noni"}

// knownPwshParam reports whether l (lower case, - prefix, maybe :value)
// names a PowerShell parameter: no path separator, and a known name or an
// abbreviation PowerShell accepts.
func knownPwshParam(l string) bool {
	if strings.ContainsAny(l[1:], `/\`) {
		return false
	}
	if k := strings.IndexByte(l, ':'); k > 0 {
		l = l[:k]
	}
	switch {
	case l == "-e" || l == "-ec" || prefixOf(l, "-encodedcommand", 3),
		l == "-c" || prefixOf(l, "-command", 3),
		l == "-f" || prefixOf(l, "-file", 3),
		pwshValueFlags[l], l == "-?" || l == "-h":
		return true
	}
	for _, sw := range pwshSwitches {
		if l == sw || prefixOf(l, sw, 4) {
			return true
		}
	}
	return false
}

// prefixOf reports an abbreviation of a PowerShell parameter: l is name or a
// prefix of it at least min bytes long.
func prefixOf(l, name string, min int) bool {
	return len(l) >= min && strings.HasPrefix(name, l)
}

// PowerShell reads the arguments of a pwsh or powershell program word (prog
// is that word; args are the words after it, quotes removed). Parameters
// may start with -, -- or / and may be abbreviated, as PowerShell allows.
func PowerShell(prog string, args []string) PwshRun {
	windows := pwshName(prog) == "powershell"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" {
			return PwshRun{Opaque: "a script read from stdin (-)"}
		}
		l := strings.ToLower(a)
		switch {
		case strings.HasPrefix(l, "--"):
			l = l[1:]
		case strings.HasPrefix(l, "/") && len(l) > 1 && knownPwshParam("-"+l[1:]):
			// a /parameter only when it names one: /home/tester/x.ps1 or
			// /x.ps1 is a script path (pwsh on Linux or macOS)
			l = "-" + l[1:]
		}
		if !strings.HasPrefix(l, "-") {
			if windows {
				// Windows PowerShell runs a bare argument as -Command.
				return PwshRun{Script: strings.Join(args[i:], " ")}
			}
			return PwshRun{Opaque: "a script file (pwsh runs a bare argument as -File)"}
		}
		value, hasValue := "", false
		if k := strings.IndexByte(l, ':'); k > 0 {
			value, hasValue = a[k+1:], true
			l = l[:k]
		}
		next := func() (string, bool) {
			if hasValue {
				return value, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch {
		case l == "-e" || l == "-ec" || prefixOf(l, "-encodedcommand", 3):
			v, ok := next()
			if !ok {
				return PwshRun{Opaque: "an -EncodedCommand with no value"}
			}
			s, err := DecodePowerShell(v)
			if err != nil {
				return PwshRun{Opaque: "an -EncodedCommand that does not decode"}
			}
			return PwshRun{Script: s, Encoded: true}
		case l == "-c" || prefixOf(l, "-command", 3):
			rest := args[i+1:]
			if hasValue {
				rest = append([]string{value}, rest...)
			}
			if len(rest) == 1 && rest[0] == "-" {
				return PwshRun{Opaque: "-Command - (a script read from stdin)"}
			}
			return PwshRun{Script: strings.Join(rest, " ")}
		case l == "-f" || prefixOf(l, "-file", 3):
			return PwshRun{Opaque: "-File (a script file)"}
		case pwshValueFlags[l]:
			if !hasValue {
				i++
			}
		}
	}
	return PwshRun{}
}

// DecodePowerShell decodes an -EncodedCommand value: base64 (padded or not)
// of UTF-16LE text.
func DecodePowerShell(s string) (string, error) {
	s = strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		b, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "="))
	}
	if err != nil {
		return "", err
	}
	if len(b) == 0 || len(b)%2 != 0 {
		return "", errors.New("not UTF-16LE")
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return string(utf16.Decode(u)), nil
}

// textCommands only print or search their arguments.
var textCommands = map[string]bool{"echo": true, "printf": true, "write-host": true, "write-output": true,
	"grep": true, "rg": true, "findstr": true, "select-string": true, "sls": true, "git": true}

// MaxPwshWords bounds PowerShellRuns on a hostile command.
const MaxPwshWords = 50000

// PowerShellRuns finds the pwsh/powershell invocations of a command line,
// for guards that match the raw text and have no tokenizer of their own.
// The split is rough: words on blanks, segments on unquoted ; & | ( ) and
// newlines, quotes removed. truncated reports a command longer than
// MaxPwshWords words, which the caller treats as opaque.
func PowerShellRuns(cmd string) (runs []PwshRun, truncated bool) {
	return powerShellRuns(cmd, 0)
}

// maxPwshNest bounds how deep quoted words are re-read (bash -c "pwsh ...").
const maxPwshNest = 4

func powerShellRuns(cmd string, depth int) (runs []PwshRun, truncated bool) {
	if depth > maxPwshNest {
		return nil, true
	}
	var seg []string
	var b strings.Builder
	in := false
	var quote byte
	words := 0
	flushWord := func() {
		if in {
			seg = append(seg, b.String())
			words++
		}
		b.Reset()
		in = false
	}
	flushSeg := func() {
		flushWord()
		if len(seg) > 0 && textCommands[pwshName(seg[0])] {
			seg = seg[:0]
			return
		}
		for k, w := range seg {
			if IsPowerShell(w) {
				if r := PowerShell(w, seg[k+1:]); r.Script != "" || r.Opaque != "" {
					runs = append(runs, r)
				}
				break
			}
		}
		// A quoted word holding a command line (bash -c "pwsh -File x").
		for _, w := range seg {
			if strings.ContainsAny(w, " \t;&|") {
				if l := strings.ToLower(w); strings.Contains(l, "pwsh") || strings.Contains(l, "powershell") {
					inner, tr := powerShellRuns(w, depth+1)
					runs = append(runs, inner...)
					truncated = truncated || tr
				}
			}
		}
		seg = seg[:0]
	}
	for i := 0; i < len(cmd); i++ {
		if words > MaxPwshWords {
			return runs, true
		}
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				b.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote, in = c, true
		case c == ' ' || c == '\t':
			flushWord()
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '\n' || c == '\r' || c == '{' || c == '}':
			flushSeg()
		default:
			b.WriteByte(c)
			in = true
		}
	}
	flushSeg()
	return runs, truncated
}
