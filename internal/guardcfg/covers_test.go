package guardcfg

import (
	"reflect"
	"testing"

	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/policy"
)

func builtinIDs() map[string]bool {
	ids := map[string]bool{GuardErrorID: true}
	for _, r := range policy.Builtin() {
		ids[r.ID] = true
	}
	for id := range HookRules {
		ids[id] = true
	}
	for _, r := range decide.BuiltinJudgeRules() {
		ids[r.ID] = true
	}
	return ids
}

func TestUnknownOverrides(t *testing.T) {
	ka := []string{"tq/iac-apply", "tq/nope"}
	m := &manifest.Manifest{Client: "acme"}
	m.Guard.Unattended = &manifest.Unattended{Workspace: true, KeepAsking: &ka}
	if got := UnknownOverrides(m, File{RulesOverride: map[string]Value{"me/x": ValueAsk, "tq/iac-destroy": ValueAsk}}); !reflect.DeepEqual(got, []string{"me/x", "tq/nope"}) {
		t.Fatalf("UnknownOverrides: %v", got)
	}
}
