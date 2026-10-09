package decide

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rec(t *testing.T, typ string, content any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": typ, "message": map[string]any{"content": content}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func use(id, name string, in map[string]any) map[string]any {
	return map[string]any{"type": "tool_use", "id": id, "name": name, "input": in}
}

func result(id string, isErr bool) map[string]any {
	return map[string]any{"type": "tool_result", "tool_use_id": id, "is_error": isErr}
}

func TestReadStopState(t *testing.T) {
	lines := []string{
		rec(t, "user", "old request"),
		rec(t, "assistant", []any{use("e0", "Edit", map[string]any{"file_path": "old.go"})}),
		rec(t, "user", []any{map[string]any{"type": "text", "text": "make the build green"}}),
		rec(t, "assistant", []any{use("b0", "Bash", map[string]any{"command": "go build ./..."})}),
		rec(t, "user", []any{result("b0", false)}),
		rec(t, "assistant", []any{use("e1", "Edit", map[string]any{"file_path": "a.go"}), use("e2", "MultiEdit", map[string]any{"file_path": "b.go"})}),
		rec(t, "user", []any{result("e1", false), result("e2", false)}),
		rec(t, "assistant", []any{use("b1", "PowerShell", map[string]any{"command": "go test ./..."})}),
		rec(t, "user", []any{result("b1", true)}),
		rec(t, "assistant", []any{use("b2", "Bash", map[string]any{"command": "go vet ./..."})}),
		`{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"tool_use","id":"s1","name":"Write","input":{"file_path":"side.go"}}]}}`,
		`not json`,
		rec(t, "assistant", []any{map[string]any{"type": "text", "text": "Done, everything passes."}}),
	}
	p := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := ReadStopState(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Request != "make the build green" {
		t.Fatalf("request = %q", st.Request)
	}
	if strings.Join(st.Edited, ",") != "a.go,b.go" {
		t.Fatalf("edited = %v", st.Edited)
	}
	if len(st.Commands) != 2 || st.Commands[0].Status != "error" || st.Commands[1].Status != "unknown" || st.Commands[1].Command != "go vet ./..." {
		t.Fatalf("commands = %+v", st.Commands)
	}
	if st.Final != "Done, everything passes." {
		t.Fatalf("final = %q", st.Final)
	}
}

func TestReadStopState_NoEditsAfterLastUserMessage(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(rec(t, "assistant", []any{use("e", "Write", map[string]any{"file_path": "x"})})+"\n"+
		rec(t, "user", "thanks")+"\n"+rec(t, "assistant", []any{map[string]any{"type": "text", "text": "np"}})+"\n"), 0o600)
	st, err := ReadStopState(p)
	if err != nil || st.HasEdits() {
		t.Fatalf("st=%+v err=%v", st, err)
	}
	if _, err := ReadStopState(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing transcript must error")
	}
}

func TestStopState_IsBounded(t *testing.T) {
	var lines []string
	lines = append(lines, rec(t, "user", strings.Repeat("r", 10_000)))
	lines = append(lines, rec(t, "assistant", []any{use("e", "Edit", map[string]any{"file_path": "x.go"})}))
	for i := 0; i < 100; i++ {
		lines = append(lines, rec(t, "assistant", []any{use("b", "Bash", map[string]any{"command": strings.Repeat("c", 2000)})}))
	}
	lines = append(lines, rec(t, "assistant", []any{map[string]any{"type": "text", "text": strings.Repeat("f", 20_000)}}))
	p := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(p, []byte(strings.Join(lines, "\n")), 0o600)
	st, _ := ReadStopState(p)
	raw, _ := json.Marshal(st)
	if len(raw) > 30_000 || len(st.Commands) != maxStopCommands {
		t.Fatalf("state %d bytes, %d commands", len(raw), len(st.Commands))
	}
}

func TestCheckStop(t *testing.T) {
	st := StopState{Edited: []string{"a.go"}, Final: "tests pass"}
	cases := []struct {
		name            string
		pol             Policy
		claims, evid    float64
		cmds            bool
		wantWould, want bool
	}{
		{"claim no commands enforce", Policy{Backend: "typesafe", Mode: "enforce"}, 0.9, 0, false, true, true},
		{"claim no commands shadow", Policy{Backend: "typesafe"}, 0.9, 0, false, true, false},
		{"claim with evidence", Policy{Backend: "typesafe", Mode: "enforce"}, 0.9, 0.7, true, false, false},
		{"claim weak evidence", Policy{Backend: "typesafe", Mode: "enforce"}, 0.9, 0.1, true, true, true},
		{"no claim", Policy{Backend: "typesafe", Mode: "enforce"}, 0.5, 0, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake(t, func(w http.ResponseWriter, body map[string]any) {
				ans := map[string]any{"claims": map[string]any{"noul": tc.claims}}
				if _, ok := body["questions"].(map[string]any)["evidence"]; ok {
					ans["evidence"] = map[string]any{"noul": tc.evid}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"answers": ans})
			})
			s := st
			if tc.cmds {
				s.Commands = []StopCommand{{Tool: "Bash", Command: "go test", Status: "ok"}}
			}
			v := CheckStop(t.Context(), client(f), tc.pol, s)
			if v.Err != nil || v.Would != tc.wantWould || v.Apply != tc.want {
				t.Fatalf("%+v", v)
			}
		})
	}
}

func TestSkillIndex_CachesOnMtimes(t *testing.T) {
	cfg, cwd, cache := t.TempDir(), t.TempDir(), t.TempDir()
	write := func(dir, name, desc string) string {
		d := filepath.Join(dir, name)
		os.MkdirAll(d, 0o755)
		p := filepath.Join(d, "SKILL.md")
		os.WriteFile(p, []byte("---\nname: "+name+"\ndescription: |\n  "+desc+"\n---\nbody\n"), 0o600)
		return p
	}
	p := write(filepath.Join(cfg, "skills"), "alpha", "first skill")
	write(filepath.Join(cwd, ".claude", "skills"), "beta", "project skill")
	os.MkdirAll(filepath.Join(cfg, "skills", "notaskill"), 0o755)

	got := SkillIndex(cache, cfg, cwd)
	if len(got) != 2 || got[0].Name != "beta" || got[0].Source != "project" || got[1].Description != "first skill" {
		t.Fatalf("index = %+v", got)
	}

	// Same size and mtime: the cached index is served, the file is not re-read.
	fi, _ := os.Stat(p)
	os.WriteFile(p, []byte("---\nname: alpha\ndescription: |\n  FIRST SKILL\n---\nbody\n"), 0o600)
	os.Chtimes(p, fi.ModTime(), fi.ModTime())
	if got := SkillIndex(cache, cfg, cwd); got[1].Description != "first skill" {
		t.Fatalf("cache not used: %+v", got)
	}

	// A changed mtime rebuilds it.
	later := fi.ModTime().Add(time.Minute)
	os.Chtimes(p, later, later)
	if got := SkillIndex(cache, cfg, cwd); got[1].Description != "FIRST SKILL" {
		t.Fatalf("cache not invalidated: %+v", got)
	}
}

func TestSkillIndex_CapsAndTruncates(t *testing.T) {
	cfg := t.TempDir()
	for i := 0; i < MaxSkills+10; i++ {
		d := filepath.Join(cfg, "skills", "s"+strings.Repeat("x", i%5)+string(rune('a'+i%26))+string(rune('a'+i/26)))
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\ndescription: "+strings.Repeat("d", 500)+"\n---\n"), 0o600)
	}
	got := SkillIndex(t.TempDir(), cfg, "")
	if len(got) != MaxSkills {
		t.Fatalf("len = %d", len(got))
	}
	for _, s := range got {
		if len(s.Description) > maxSkillDescription+len("…") || s.Name == "" {
			t.Fatalf("skill %+v", s)
		}
	}
}
