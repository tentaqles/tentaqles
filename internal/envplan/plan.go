// Package envplan computes the environment a workspace wants and the diff to get there.
package envplan

import (
	"encoding/base64"
	"encoding/json"
	"sort"

	"github.com/tentaqles/tentaqles/cli/internal/paths"
	"github.com/tentaqles/tentaqles/cli/internal/resolve"
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
	provs := Providers()
	for _, id := range ws.Manifest.IdentityNames() {
		p, ok := provs[id]
		if !ok {
			continue
		}
		for k, v := range p.Vars(paths.IdentityDir(ws.Name, id)) {
			out[k] = v
		}
	}
	return out
}

// State is what tq changed, carried in __TQ_STATE so the binary stays stateless.
type State struct {
	WS   string             `json:"ws"`
	Prev map[string]*string `json:"prev"`
}

func (s State) Encode() string {
	b, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(b)
}

func DecodeState(enc string) State {
	var s State
	if b, err := base64.RawURLEncoding.DecodeString(enc); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Prev == nil {
		s.Prev = map[string]*string{}
	}
	return s
}

type Ops struct {
	Set     map[string]string
	Unset   []string
	Changed bool
	From    string
	To      string
}

// Diff computes the shell operations to move from the current env to desired,
// restoring anything tq previously changed that is no longer desired.
func Diff(desired map[string]string, current func(string) (string, bool), prev State, wsName string) (Ops, State) {
	ops := Ops{Set: map[string]string{}, From: prev.WS, To: wsName}
	next := State{WS: wsName, Prev: map[string]*string{}}
	for k, v := range prev.Prev {
		next.Prev[k] = v
	}
	// 1. restore vars we set that are no longer wanted
	for k, orig := range prev.Prev {
		if _, want := desired[k]; want {
			continue
		}
		if orig == nil {
			ops.Unset = append(ops.Unset, k)
		} else {
			ops.Set[k] = *orig
		}
		delete(next.Prev, k)
	}
	// 2. set what differs
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
	sort.Strings(ops.Unset)
	ops.Changed = len(ops.Set)+len(ops.Unset) > 0
	if !ops.Changed {
		return ops, next
	}
	if len(next.Prev) == 0 {
		ops.Unset = append(ops.Unset, StateVar)
		next.WS = ""
	} else {
		ops.Set[StateVar] = next.Encode()
	}
	return ops, next
}
