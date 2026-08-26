package shell

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/cli/internal/envplan"
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

func TestEmit_Golden(t *testing.T) {
	for _, sh := range Shells {
		t.Run(sh, func(t *testing.T) {
			got, err := Emit(sh, sample())
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join("testdata", sh+".golden")
			if *update {
				os.WriteFile(p, []byte(got), 0o644)
			}
			want, err := os.ReadFile(p)
			if err != nil {
				t.Fatalf("missing golden %s (run with -update): %v", p, err)
			}
			if got != string(want) {
				t.Fatalf("golden mismatch for %s:\n--- got\n%s\n--- want\n%s", sh, got, want)
			}
		})
	}
}

func TestEmit_NoChange_Empty(t *testing.T) {
	out, _ := Emit("bash", envplan.Ops{})
	if out != "" {
		t.Fatalf("%q", out)
	}
}

func TestEmit_UnknownShell(t *testing.T) {
	if _, err := Emit("tcsh", sample()); err == nil {
		t.Fatal("expected error")
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
