package shell

import (
	"flag"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

var update = flag.Bool("update", false, "rewrite golden files")

func sample() envplan.Ops {
	return envplan.Ops{
		Changed: true,
		Set: map[string]string{
			"CLAUDE_CONFIG_DIR": `C:\Users\re nato\it's\claude`,
			"TQ_WS":             "acme",
			"WEIRD":             `a"b$c` + "`d",
		},
		Unset: []string{"AZURE_CONFIG_DIR", "GH_CONFIG_DIR"},
	}
}

func TestHook_EachShell_ContainsBinAndEnvCall(t *testing.T) {
	for _, sh := range Shells {
		h, err := Hook(sh)
		if err != nil {
			t.Fatalf("%s: %v", sh, err)
		}
		if !strings.Contains(h, "env --shell "+sh) {
			t.Fatalf("%s hook must call env --shell %s:\n%s", sh, sh, h)
		}
	}
}

func TestHook_Pwsh_WrapsPromptOnce(t *testing.T) {
	h, _ := Hook("pwsh")
	if !strings.Contains(h, "__tq_prev_prompt") || !strings.Contains(h, "function global:prompt") {
		t.Fatalf("pwsh hook must wrap existing prompt:\n%s", h)
	}
}
