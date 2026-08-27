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
		if sh == "cmd" {
			continue
		}
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

func TestEmit_Cmd_RejectsUnquotableValue(t *testing.T) {
	_, err := Emit("cmd", sample())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "WEIRD") {
		t.Fatalf("error should mention WEIRD: %v", err)
	}
}

func TestEmit_Cmd_Golden(t *testing.T) {
	ops := envplan.Ops{
		Changed: true,
		Set: map[string]string{
			"CLAUDE_CONFIG_DIR": `C:\Users\re nato\it's\claude`,
			"TQ_WS":             "acme",
		},
		Unset: []string{"AZURE_CONFIG_DIR", "GH_CONFIG_DIR"},
	}
	got, err := Emit("cmd", ops)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "cmd.golden"))
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for cmd:\n--- got\n%s\n--- want\n%s", got, want)
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

func TestEmit_RejectsHostileKey(t *testing.T) {
	for _, sh := range []string{"bash", "pwsh", "fish", "cmd"} {
		ops := envplan.Ops{
			Changed: true,
			Set:     map[string]string{`X;curl x|sh`: "value"},
		}
		if _, err := Emit(sh, ops); err == nil {
			t.Errorf("%s: expected an error for a hostile Set key", sh)
		}
		ops = envplan.Ops{Changed: true, Unset: []string{`X;curl x|sh`}}
		if _, err := Emit(sh, ops); err == nil {
			t.Errorf("%s: expected an error for a hostile Unset key", sh)
		}
		ops = envplan.Ops{Changed: true, Set: map[string]string{"OK_VAR": "a\nexport EVIL=1"}}
		if _, err := Emit(sh, ops); err == nil {
			t.Errorf("%s: expected an error for a value containing a newline", sh)
		}
	}
}

func TestEmit_AcceptsStateVars(t *testing.T) {
	ops := envplan.Ops{
		Changed: true,
		Set:     map[string]string{"__TQ_STATE": "x", "TQ_WS": "acme", "TQ_WS_ROOT": `C:\repos\acme`},
		Unset:   []string{"__TQ_STATE", "TQ_WS", "TQ_WS_ROOT"},
	}
	for _, sh := range Shells {
		if _, err := Emit(sh, ops); err != nil {
			t.Errorf("%s: unexpected error: %v", sh, err)
		}
	}
}
