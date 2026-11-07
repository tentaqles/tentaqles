package envplan

import (
	"os"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Env is the environment the prompt hook (tq env) reads: a variable lookup, the names it
// holds (for the prefix entries of the match lists, TF_TOKEN_*), and how the shell spells
// PATH. PathOK false leaves PATH alone: tq cannot spell it for that shell (fish on Windows,
// or a bash hook from before ShellPathVar).
type Env struct {
	Lookup func(string) (string, bool)
	Names  []string
	Syntax PathSyntax
	PathOK bool
}

// OSEnv is the process environment as a native shell spells it.
func OSEnv() Env {
	return Env{Lookup: os.LookupEnv, Names: environNames(os.Environ()), Syntax: NativeSyntax(), PathOK: true}
}

// ShellEnv is the environment tq env reads for shell sh. On Windows bash and zsh spell PATH
// their own way: the hook passes that spelling in ShellPathVar and tq answers in it; without it
// PATH is left alone. fish on Windows: PATH is left alone. Every other case is OSEnv.
func ShellEnv(sh string) Env {
	e := OSEnv()
	if runtime.GOOS != "windows" {
		return e
	}
	switch sh {
	case "bash", "zsh":
		shp, ok := os.LookupEnv(ShellPathVar)
		if !ok || shp == "" {
			e.PathOK = false
			return e
		}
		e.Syntax = MSYSSyntax(shp, os.Getenv("PATH"))
		e.Lookup = func(k string) (string, bool) {
			if envKey(k) == envKey("PATH") {
				return shp, true
			}
			return os.LookupEnv(k)
		}
	case "fish":
		e.PathOK = false
	}
	return e
}

func environNames(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		if k, _, _ := splitKV(kv); k != "" && !strings.HasPrefix(k, "=") {
			out = append(out, k)
		}
	}
	return out
}

// Plan is what workspace ws wants of env at this prompt, given the state the previous prompt
// left (prev): the variables Desired sets and, for an identity (LayerFor), the
// out-of-scope variables unset and the identity PATH (FilterPath). The notice, when not nil,
// says the mask folder is missing.
//
// The PATH is built from the PATH before tq changed it (prev's record, merged with any change
// the user made since: MergePath), never from the PATH of the identity the shell leaves, so
// moving from an AWS-only identity to an AWS one finds aws again. A PATH still equal to the
// one tq wrote last is kept without reading a folder: the cost is paid once per cd into the
// identity and once per PATH change, not at every prompt.
func Plan(ws *resolve.Workspace, prev State, env Env) (Want, *Notice) {
	w := Want{Set: Desired(ws), PathSep: env.Syntax.Sep}
	l := LayerFor(ws)
	if l == nil {
		return w, nil
	}
	// The names the shell holds and the ones an earlier prompt unset (still recorded), so a
	// variable stays unset rather than restored.
	names := append([]string{}, env.Names...)
	for k, v := range prev.Prev {
		if v != nil {
			names = append(names, k)
		}
	}
	w.Unset = l.UnsetNames(dedupeNames(names), w.Set)
	if !env.PathOK {
		return w, nil
	}
	cur, ok := env.Lookup("PATH")
	if !ok {
		return w, nil
	}
	if prev.WS == ws.Name && prev.PathSet != "" && cur == prev.PathSet && l.shimsEntryCurrent(cur, env.Syntax) {
		// The fast path: no folder is read. The mask folder is still checked (one directory
		// read, names and sizes): on the PATH only while it is as tq wrote it, back on it as
		// soon as it is again, and a notice at every prompt while it is wanted and missing.
		has := l.HasMask(cur, env.Syntax)
		if !prev.MaskWanted && !has {
			w.Set["PATH"] = cur
			return w, nil
		}
		if ready := l.MaskReady(); has == ready && prev.MaskWanted {
			w.Set["PATH"], w.MaskWanted = cur, true
			if !ready {
				return w, &Notice{Kind: NoticeMask, Workspace: ws.Name}
			}
			return w, nil
		}
	}
	base := cur
	if orig, tracked := trackedPath(prev); tracked && prev.PathSet != "" {
		base = MergePath(orig, prev.PathSet, cur, env.Syntax.Sep)
		if base != orig {
			// The user changed PATH in the identity: the merged PATH is what leaving restores.
			w.PathOrig = &base
		}
	}
	np, rep := l.FilterPath(base, env.Syntax)
	w.Set["PATH"] = np
	w.MaskWanted = len(rep.Shared) > 0
	if rep.MaskMissing {
		return w, &Notice{Kind: NoticeMask, Workspace: ws.Name}
	}
	return w, nil
}

func dedupeNames(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if k := envKey(x); !seen[k] {
			seen[k] = true
			out = append(out, x)
		}
	}
	return out
}

// trackedPath is the PATH before tq changed it, from prev's record.
func trackedPath(prev State) (string, bool) {
	for k, v := range prev.Prev {
		if envKey(k) == envKey("PATH") && v != nil {
			return *v, true
		}
	}
	return "", false
}
