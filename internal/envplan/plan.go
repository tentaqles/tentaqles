// Package envplan computes the environment a workspace wants and the diff to get there.
package envplan

import (
	"os"
	"sort"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

const StateVar = "__TQ_STATE"

// Desired returns the full env a workspace requires. Neutral (nil) → empty.
func Desired(ws *resolve.Workspace) map[string]string {
	out := map[string]string{}
	if ws == nil {
		return out
	}
	out["TQ_WS"] = ws.Name
	out["TQ_WS_ROOT"] = ws.Root
	if HasBrowsers(ws) {
		// BROWSER names the client's shim, which opens a URL in the client's profile.
		out["BROWSER"] = BrowserVar(ws.Name)
	}
	provs := Providers()
	for _, id := range ws.Manifest.IdentityNames() {
		p, ok := provs[id]
		if !ok {
			continue
		}
		for k, v := range p.Vars(paths.IdentityDir(ws.Name, id)) {
			// The catalog lets two ids share a variable only when they are one CLI (az and
			// azure-devops); the first identity in sorted order keeps it, so both log in to
			// one directory.
			if _, set := out[k]; set {
				continue
			}
			out[k] = v
		}
	}
	return out
}

// State is what tq changed, carried in __TQ_STATE so the binary stays stateless.
type State struct {
	WS   string             `json:"ws"`
	Prev map[string]*string `json:"prev"`
	// PathSet is the PATH tq last wrote for an identity, in the shell's
	// spelling: a PATH still equal to it needs no work at the next prompt, and one the user
	// changed since is merged back (MergePath) on leaving.
	PathSet string `json:"path,omitempty"`
	// Sep is PathSet's list separator (the shell's).
	Sep string `json:"sep,omitempty"`
	// MaskWanted: a PATH folder of PathSet holds an out-of-scope CLI beside other tools, so the
	// mask folder belongs on the PATH; the next prompt puts it back (or takes it off) as soon
	// as the folder is (or stops being) as tq wrote it, and says so while it is missing.
	MaskWanted bool `json:"mw,omitempty"`

	// The compact spelling of PathSet in the encoding (encodeState): the entries of the
	// recorded PATH it drops and the mask entry it puts first.
	PathDrop    []int  `json:"pd,omitempty"`
	PathMask    string `json:"pm,omitempty"`
	PathCompact bool   `json:"pc,omitempty"`
}

// Encode is the __TQ_STATE value (state.go): zlib-compressed JSON, with the PATH tq wrote
// spelled as the entries it dropped from the recorded one, so the state holds one PATH, not
// two. cmd.exe reads a line of at most 8191 characters: a 3.5 KB PATH must fit with room.
func (s State) Encode() string { return encodeState(s) }

// DecodeState reads Encode's value, and the plain base64 JSON of tq before it.
func DecodeState(enc string) State { return decodeState(enc) }

type Ops struct {
	Set     map[string]string
	Unset   []string
	Changed bool
	From    string
	To      string
	// Notice is the one line the shell prints on stderr at this prompt,
	// whether or not anything changed (tentaqles env sets it; shell.Emit renders
	// it from fixed text, never from a path or the manifest).
	Notice *Notice
}

// Notice is what the prompt says about the cwd's identity: its PATH mask, a cmd PATH too long,
// or a lost state. Workspace is a name; shell.Emit prints it
// only when it is a valid workspace name.
type Notice struct {
	Kind      string
	Workspace string
}

// The kinds of Notice.
const (
	// NoticeMask: an identity's PATH needs its mask folder (a folder
	// shared with other tools holds an out-of-scope CLI) and the folder is
	// missing or changed; the fix is tentaqles settings render.
	NoticeMask = "path-mask"
	// NoticeCmdLong: the identity PATH is longer than a cmd.exe line, so cmd.exe keeps the
	// PATH it has (out-of-scope CLIs still resolve there); the rest of the identity applies.
	NoticeCmdLong = "cmd-path-long"
	// NoticeStateLost: TQ_WS was set but __TQ_STATE was gone or unreadable; tq unset its own
	// variables and mask folders and cannot put back the values from before it (lost.go).
	NoticeStateLost = "state-lost"
)

// Want is what a workspace wants of the environment: the variables to set and, for an
// identity, the variables to unset. PathSep is the shell's PATH list
// separator, used when a PATH tq changed is restored.
type Want struct {
	Set     map[string]string
	Unset   []string
	PathSep string
	// PathOrig replaces the recorded PATH before tq (State.Prev): the user changed PATH
	// inside the identity and the merged PATH (MergePath) is what leaving restores.
	PathOrig *string
	// MaskWanted: the PATH holds a shared folder with an out-of-scope CLI (State.MaskWanted).
	MaskWanted bool
}

// DiffWant is Diff for a Want: every variable tq sets or unsets is recorded with its value
// before tq touched it (State.Prev) and put back once no longer wanted, so leaving the folder
// restores the shell exactly. PATH is put back through MergePath.
func DiffWant(want Want, current func(string) (string, bool), prev State, wsName string) (Ops, State) {
	desired := want.Set
	if desired == nil {
		desired = map[string]string{}
	}
	unset := map[string]bool{}
	for _, k := range want.Unset {
		unset[k] = true
	}
	sep := want.PathSep
	if sep == "" {
		sep = string(os.PathListSeparator)
	}
	ops := Ops{Set: map[string]string{}, From: prev.WS, To: wsName}
	next := State{WS: wsName, Prev: map[string]*string{}}
	for k, v := range prev.Prev {
		next.Prev[k] = v
	}
	// 1. restore vars we set (or unset) that are no longer wanted
	for k, orig := range prev.Prev {
		if _, want := desired[k]; want || unset[k] {
			continue
		}
		switch {
		case orig == nil:
			ops.Unset = append(ops.Unset, k)
		case envKey(k) == envKey("PATH") && prev.PathSet != "":
			cur, _ := current(k)
			ops.Set[k] = MergePath(*orig, prev.PathSet, cur, sep)
		default:
			ops.Set[k] = *orig
		}
		delete(next.Prev, k)
	}
	// 2. unset what the identity leaves out, remembering the value
	for _, k := range want.Unset {
		cur, ok := current(k)
		if !ok {
			continue
		}
		if _, tracked := next.Prev[k]; !tracked {
			c := cur
			next.Prev[k] = &c
		}
		ops.Unset = append(ops.Unset, k)
	}
	// 3. set what differs
	for k, v := range desired {
		cur, ok := current(k)
		if _, tracked := next.Prev[k]; !tracked {
			if ok {
				c := cur
				next.Prev[k] = &c
			} else {
				next.Prev[k] = nil
			}
		}
		if !ok || cur != v {
			ops.Set[k] = v
		}
	}
	if p, ok := desired["PATH"]; ok {
		next.PathSet, next.Sep, next.MaskWanted = p, sep, want.MaskWanted
		if want.PathOrig != nil {
			for k := range next.Prev {
				if envKey(k) == envKey("PATH") && next.Prev[k] != nil {
					o := *want.PathOrig
					next.Prev[k] = &o
				}
			}
		}
	}
	// A new PATH record alone is a change of state: the next prompt compares against it.
	ops.Changed = len(ops.Set)+len(ops.Unset) > 0 || next.PathSet != prev.PathSet || next.MaskWanted != prev.MaskWanted
	if !ops.Changed {
		return ops, next
	}
	if len(next.Prev) == 0 {
		ops.Unset = append(ops.Unset, StateVar)
		next.WS = ""
	} else {
		ops.Set[StateVar] = next.Encode()
	}
	// Sorted last so StateVar is ordered with the rest, keeping output stable.
	sort.Strings(ops.Unset)
	return ops, next
}
