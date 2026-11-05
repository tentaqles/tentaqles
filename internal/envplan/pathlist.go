package envplan

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathSyntax is how a shell spells PATH: its list separator and the mapping between its
// entries and the OS paths tq reads. A native shell (PowerShell, cmd.exe, any POSIX shell)
// spells PATH as the OS does; Git Bash on Windows spells it /c/Users/...:/usr/bin, which tq,
// a Windows program, only sees converted, so the shell hook passes its own spelling
// (ShellPathVar) and tq answers in it.
type PathSyntax struct {
	Sep string
	// ToOS is the OS path of an entry; "" when tq cannot tell (the entry is then kept).
	ToOS func(string) string
	// FromOS is how the shell spells an OS path tq adds (the mask folder).
	FromOS func(string) string
}

// ShellPathVar carries a POSIX shell's own PATH spelling to tq env on Windows (the bash and
// zsh hooks set it for the one call, with MSYS2_ENV_CONV_EXCL so the MSYS runtime leaves it
// as the shell spells it).
const ShellPathVar = "__TQ_SH_PATH"

// NativeSyntax is the OS's own PATH syntax.
func NativeSyntax() PathSyntax {
	id := func(s string) string { return s }
	return PathSyntax{Sep: string(os.PathListSeparator), ToOS: id, FromOS: id}
}

// MSYSSyntax is Git Bash's (MSYS2's) PATH syntax on Windows. shellPath is the shell's own
// spelling, osPath the same list as the MSYS runtime converted it for tq (os PATH): when both
// hold as many entries, entry i of one is entry i of the other, which covers every mount
// (/usr/bin is C:\Program Files\Git\usr\bin). Otherwise /x/rest is X:\rest and any other
// entry is unknown, so kept.
func MSYSSyntax(shellPath, osPath string) PathSyntax {
	sh := strings.Split(shellPath, ":")
	win := strings.Split(osPath, ";")
	pair := map[string]string{}
	if len(sh) == len(win) {
		for i := range sh {
			pair[sh[i]] = win[i]
		}
	}
	return PathSyntax{
		Sep: ":",
		ToOS: func(e string) string {
			if w, ok := pair[e]; ok {
				return w
			}
			if len(e) >= 2 && e[0] == '/' && isLetter(e[1]) && (len(e) == 2 || e[2] == '/') {
				return strings.ToUpper(e[1:2]) + `:\` + filepath.FromSlash(strings.TrimPrefix(e[2:], "/"))
			}
			return ""
		},
		FromOS: func(p string) string {
			if len(p) >= 2 && p[1] == ':' && isLetter(p[0]) {
				return "/" + strings.ToLower(p[:1]) + filepath.ToSlash(p[2:])
			}
			return filepath.ToSlash(p)
		},
	}
}

func isLetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// PathReport says what FilterPath did.
type PathReport struct {
	Dropped []string // entries removed: every executable in them is out of scope
	Shared  []string // entries kept that hold an out-of-scope executable beside others
	// MaskMissing: a shared entry needs the mask folder but it is not ready (tq settings
	// render writes it); the out-of-scope CLI there still resolves.
	MaskMissing bool
}

// FilterPath is the identity PATH built from path (layer 2, ADR-0007), in syn's spelling:
//
//   - an entry whose executables are all out-of-scope CLIs (an AWS CLI install folder for an
//     Azure-only identity) is dropped: the CLI resolves to nothing;
//   - an entry that also holds anything else (npm's global folder, scoop's shims, WinGet's
//     Links, /usr/local/bin, ~/.local/bin) is kept, and the mask folder goes first on the
//     PATH, where a stub of each out-of-scope name answers "command not found", exit 127;
//   - every other entry, an empty one, one tq cannot map to an OS path, and on Windows any
//     folder under %SystemRoot% are kept as they are, in order.
//
// A mask entry already there is removed before the result is built, so filtering is
// idempotent.
func (l *Layer) FilterPath(path string, syn PathSyntax) (string, PathReport) {
	var rep PathReport
	var kept []string
	shared := false
	for _, e := range strings.Split(path, syn.Sep) {
		osp := ""
		if e != "" {
			osp = syn.ToOS(e)
		}
		if osp != "" && l.isMaskEntry(osp) {
			continue
		}
		switch l.classify(osp) {
		case dirDrop:
			rep.Dropped = append(rep.Dropped, e)
			continue
		case dirShared:
			rep.Shared = append(rep.Shared, e)
			shared = true
		}
		kept = append(kept, e)
	}
	if shared {
		if l.MaskReady() {
			kept = append([]string{syn.FromOS(l.maskDir)}, kept...)
		} else {
			rep.MaskMissing = true
		}
	}
	return strings.Join(kept, syn.Sep), rep
}

// HasMask reports whether a PATH in syn's spelling holds the layer's mask folder.
func (l *Layer) HasMask(path string, syn PathSyntax) bool {
	for _, e := range strings.Split(path, syn.Sep) {
		if e != "" && l.isMaskEntry(syn.ToOS(e)) {
			return true
		}
	}
	return false
}

const (
	dirKeep = iota
	dirDrop
	dirShared
)

// classify reads one PATH folder: one directory read, and only for a folder that holds an
// out-of-scope name a look at its other files (their extension on Windows, a stat on POSIX).
func (l *Layer) classify(dir string) int {
	if dir == "" || !filepath.IsAbs(dir) || underSystemRoot(dir) {
		return dirKeep
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		return dirKeep
	}
	out := false
	var others []os.DirEntry
	for _, e := range es {
		if e.IsDir() {
			continue
		}
		if l.outExecutable(e.Name()) {
			out = true
			continue
		}
		others = append(others, e)
	}
	if !out {
		return dirKeep
	}
	for _, e := range others {
		if isExecutable(dir, e) {
			return dirShared
		}
	}
	return dirDrop
}

// outExecutable reports whether a file name is an out-of-scope CLI: on Windows only a
// runnable spelling (an extension of PATHEXT or .ps1, or none, which Git Bash runs) counts,
// so aws.txt is not aws.
func (l *Layer) outExecutable(name string) bool {
	if runtime.GOOS == "windows" {
		if ext := strings.ToLower(filepath.Ext(name)); ext != "" && !winExecExt()[ext] {
			return false
		}
	}
	_, ok := l.scope.OutOfScopeExecutable(name)
	return ok
}

// isExecutable reports whether a file another tool may need is runnable from PATH: on
// Windows an extension of PATHEXT or .ps1, or an extensionless file starting with #! (a
// script Git Bash runs); on POSIX a regular file (links followed) with an execute bit.
func isExecutable(dir string, e os.DirEntry) bool {
	p := filepath.Join(dir, e.Name())
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if ext != "" {
			return winExecExt()[ext]
		}
		f, err := os.Open(p)
		if err != nil {
			return false
		}
		defer f.Close()
		b := make([]byte, 2)
		n, _ := f.Read(b)
		return n == 2 && string(b) == "#!"
	}
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0
}

var winExts map[string]bool

func winExecExt() map[string]bool {
	if winExts != nil {
		return winExts
	}
	m := map[string]bool{".ps1": true}
	pe := os.Getenv("PATHEXT")
	if pe == "" {
		pe = ".COM;.EXE;.BAT;.CMD;.VBS;.VBE;.JS;.JSE;.WSF;.WSH;.MSC"
	}
	for _, x := range strings.Split(pe, ";") {
		if x = strings.ToLower(strings.TrimSpace(x)); strings.HasPrefix(x, ".") {
			m[x] = true
		}
	}
	winExts = m
	return m
}

// underSystemRoot: no provider CLI installs into %SystemRoot% (C:\Windows), and System32 is
// the largest folder on a Windows PATH; it is never read.
func underSystemRoot(dir string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	sr := os.Getenv("SystemRoot")
	if sr == "" {
		return false
	}
	d, r := strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(sr))
	return d == r || strings.HasPrefix(d, r+`\`)
}

// MergePath is the PATH to restore on leaving an identity (and the base the next identity
// PATH is built from): orig, the PATH before tq changed it, when the shell's PATH is still set,
// the one tq last wrote. A change the user made since is kept, the way mise keeps it on
// deactivation: an entry added in front of tq's entries goes in front of orig, one added
// elsewhere after it, and an entry the user removed from set is removed from orig too.
func MergePath(orig, set, cur, sep string) string {
	if cur == set {
		return orig
	}
	inSet := map[string]bool{}
	for _, e := range strings.Split(set, sep) {
		inSet[e] = true
	}
	inCur := map[string]bool{}
	var front, back []string
	seenSet := false
	for _, e := range strings.Split(cur, sep) {
		inCur[e] = true
		if inSet[e] {
			seenSet = true
			continue
		}
		if seenSet {
			back = append(back, e)
		} else {
			front = append(front, e)
		}
	}
	out := append([]string{}, front...)
	for _, e := range strings.Split(orig, sep) {
		if inSet[e] && !inCur[e] {
			continue // the user removed it
		}
		out = append(out, e)
	}
	out = append(out, back...)
	return strings.Join(out, sep)
}

// ErrNotInPath is LookPath's answer for a name the identity PATH does not hold.
var ErrNotInPath = errors.New("executable file not found in the identity PATH")

// LookPath resolves a command name against the PATH (and on Windows PATHEXT) of env, the
// environment a launch runs with, not tq's own: tq run <ws> -- aws from a shell outside the
// identity must not find the aws the identity PATH dropped. A name with a path separator is
// returned as it is.
func LookPath(name string, env []string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		return name, nil
	}
	pathv, pathext := "", ""
	for _, kv := range env {
		k, v, _ := splitKV(kv)
		switch envKey(k) {
		case envKey("PATH"):
			pathv = v
		case envKey("PATHEXT"):
			pathext = v
		}
	}
	var exts []string
	if runtime.GOOS == "windows" {
		if filepath.Ext(name) != "" {
			exts = append(exts, "")
		}
		if pathext == "" {
			pathext = ".COM;.EXE;.BAT;.CMD"
		}
		for _, x := range strings.Split(pathext, ";") {
			if x = strings.TrimSpace(x); x != "" {
				exts = append(exts, strings.ToLower(x))
			}
		}
	} else {
		exts = []string{""}
	}
	for _, dir := range filepath.SplitList(pathv) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		for _, x := range exts {
			p := filepath.Join(dir, name+x)
			fi, err := os.Stat(p)
			if err != nil || fi.IsDir() {
				continue
			}
			if runtime.GOOS != "windows" && fi.Mode()&0o111 == 0 {
				continue
			}
			return p, nil
		}
	}
	return "", ErrNotInPath
}
