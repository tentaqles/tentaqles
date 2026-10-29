package codexcfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const user = "# my settings\nmodel = \"gpt-5\"\n\n[mcp_servers.docs]\ncommand = \"docs-mcp\"\n"

func write(t *testing.T, s string) string {
	t.Helper()
	dir := t.TempDir()
	if s != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func read(t *testing.T, dir string) string {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(dir, "config.toml"))
	return string(b)
}

func readFile(t *testing.T, p string) (string, bool) {
	t.Helper()
	b, err := os.ReadFile(p)
	return string(b), err == nil
}

func plan(approval, sandbox string, rules ...PrefixRule) *Plan {
	return &Plan{Mode: "default", Approval: approval, Sandbox: sandbox, Rules: rules}
}

func want(p *Plan) Want { return Want{Plan: p, Hook: p != nil} }

var iacPrompt = PrefixRule{ID: "tq/iac-apply", Decision: DecisionPrompt, Pattern: Pattern{{"terraform"}, {"apply"}}}

func TestRenderTopLevelOnAndOff(t *testing.T) {
	dir := write(t, user)
	res, err := Render(dir, want(plan("on-request", "workspace-write")), false)
	if err != nil || len(res.Changed) == 0 || len(res.Conflicts) != 0 {
		t.Fatalf("on: %+v %v", res, err)
	}
	got := read(t, dir)
	if !strings.HasPrefix(got, beginMarker+"\napproval_policy = \"on-request\"\nsandbox_mode = \"workspace-write\"\n"+endMarker+"\n") {
		t.Fatalf("managed block must lead the file:\n%s", got)
	}
	if !strings.HasSuffix(got, user) {
		t.Fatalf("user content changed:\n%s", got)
	}
	if res, _ := Render(dir, want(plan("on-request", "workspace-write")), false); len(res.Changed) != 0 {
		t.Fatalf("second render must be a no-op: %v", res.Changed)
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if read(t, dir) != user {
		t.Fatalf("removal must restore the user file exactly:\n%q", read(t, dir))
	}
	for _, f := range []string{RulesFile, HooksFile} {
		if _, ok := readFile(t, filepath.Join(dir, filepath.FromSlash(f))); ok {
			t.Errorf("%s must be removed", f)
		}
	}
}

// The unattended block (old markers) is replaced, not duplicated.
func TestRenderReplacesLegacyBlock(t *testing.T) {
	legacy := legacyBegin + "\napproval_policy = \"never\"\nsandbox_mode = \"workspace-write\"\n" + legacyEnd + "\n" + user
	dir := write(t, legacy)
	if _, err := Render(dir, want(plan("on-request", "workspace-write")), false); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir)
	if strings.Contains(got, legacyBegin) || strings.Count(got, "approval_policy") != 1 || !strings.HasSuffix(got, user) {
		t.Fatalf("legacy block must go:\n%s", got)
	}
}

func TestRenderRemovesOwnedProfileOnly(t *testing.T) {
	owned := legacyBegin + "\napproval_policy = \"never\"\nsandbox_mode = \"read-only\"\n" + legacyEnd + "\n"
	dir := write(t, user)
	pp := filepath.Join(dir, legacyProfile+".config.toml")
	if err := os.WriteFile(pp, []byte(owned), 0o600); err != nil {
		t.Fatal(err)
	}
	if !OwnsProfile(dir) {
		t.Fatal("tq-written profile must be owned")
	}
	res, err := Render(dir, want(plan("on-request", "workspace-write")), false)
	if err != nil || !hasPath(res.Changed, pp) {
		t.Fatalf("profile removal: %+v %v", res, err)
	}
	if _, ok := readFile(t, pp); ok {
		t.Fatal("a tq-owned profile file is removed (spec E §12)")
	}
	mine := "model = \"x\"\n"
	if err := os.WriteFile(pp, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dir, want(plan("on-request", "workspace-write")), false); err != nil {
		t.Fatal(err)
	}
	if b, _ := readFile(t, pp); b != mine {
		t.Fatal("a user profile file is never touched")
	}
}

func TestRenderDryRun(t *testing.T) {
	dir := write(t, "")
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("removal on an empty dir must not create files: %v", entries)
	}
	res, err := Render(dir, want(plan("on-request", "workspace-write", iacPrompt)), true)
	if err != nil || len(res.Changed) != 3 {
		t.Fatalf("dry run: %+v %v", res, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("dry run must not write: %v", entries)
	}
}

func TestRenderConfigConflictStillWritesRulesAndHook(t *testing.T) {
	dir := write(t, "approval_policy = \"on-request\"\n"+user)
	res, err := Render(dir, want(plan("never", "workspace-write", iacPrompt)), false)
	if err != nil || len(res.Conflicts) != 1 || !strings.Contains(res.Conflicts[0], "approval_policy") {
		t.Fatalf("want one conflict: %+v %v", res, err)
	}
	if read(t, dir) != "approval_policy = \"on-request\"\n"+user {
		t.Fatal("config.toml must be unchanged on a conflict")
	}
	if _, ok := readFile(t, filepath.Join(dir, "rules", "tq.rules")); !ok {
		t.Fatal("the rules file is independent of the config conflict")
	}
	if _, ok := readFile(t, filepath.Join(dir, HooksFile)); !ok {
		t.Fatal("the hook is independent of the config conflict")
	}
}

func TestRenderUnterminatedBlockIsConflict(t *testing.T) {
	broken := beginMarker + "\napproval_policy = \"never\"\n" + user
	for _, w := range []Want{want(plan("on-request", "workspace-write")), {}} {
		dir := write(t, broken)
		res, err := Render(dir, w, false)
		if err != nil || len(res.Conflicts) == 0 || !strings.Contains(res.Conflicts[0], "no end marker") {
			t.Fatalf("want conflict for %+v: %+v %v", w, res, err)
		}
		if read(t, dir) != broken {
			t.Fatalf("file must be byte-identical:\n%q", read(t, dir))
		}
	}
}

func TestRenderRulesFile(t *testing.T) {
	dir := write(t, "")
	rp := filepath.Join(dir, "rules", "tq.rules")
	if _, err := Render(dir, want(plan("on-request", "workspace-write", iacPrompt)), false); err != nil {
		t.Fatal(err)
	}
	got, ok := readFile(t, rp)
	if !ok || got != RenderRules("default", []PrefixRule{iacPrompt}) {
		t.Fatalf("rules file:\n%s", got)
	}
	rules, err := ReadRules(dir)
	if err != nil || len(rules) != 1 || rules[0].ID != "tq/iac-apply" {
		t.Fatalf("read back: %+v %v", rules, err)
	}
	// A user's own rules file next to it is left alone.
	other := filepath.Join(dir, "rules", "default.rules")
	if err := os.WriteFile(other, []byte("prefix_rule(pattern = [\"ls\"])\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A hand edit of tq's file is put back.
	if err := os.WriteFile(rp, []byte(got+"prefix_rule(pattern = [\"git\"], decision = \"allow\")\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dir, want(plan("on-request", "workspace-write", iacPrompt)), false); err != nil {
		t.Fatal(err)
	}
	if b, _ := readFile(t, rp); b != got {
		t.Fatalf("tq's file is tq-owned:\n%s", b)
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFile(t, rp); ok {
		t.Fatal("removal deletes tq's rules file")
	}
	if _, ok := readFile(t, other); !ok {
		t.Fatal("the user's rules file must stay")
	}
}

func TestRenderForeignRulesFileIsConflict(t *testing.T) {
	dir := write(t, "")
	rp := filepath.Join(dir, "rules", "tq.rules")
	if err := os.MkdirAll(filepath.Dir(rp), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := "prefix_rule(pattern = [\"ls\"])\n"
	if err := os.WriteFile(rp, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, w := range []Want{want(plan("on-request", "workspace-write", iacPrompt)), {}} {
		res, err := Render(dir, w, false)
		if err != nil {
			t.Fatal(err)
		}
		if w.Plan != nil && !conflictOn(res, "tq.rules") {
			t.Fatalf("a user-written tq.rules is a conflict: %+v", res)
		}
		if b, _ := readFile(t, rp); b != mine {
			t.Fatal("a user-written tq.rules is never touched")
		}
	}
}

func hookDoc(t *testing.T, dir string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, HooksFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatalf("hooks.json: %v\n%s", err, b)
	}
	return doc
}

func TestRenderHooksMergesWithUserHooks(t *testing.T) {
	dir := write(t, "")
	userHooks := `{
  "description": "mine",
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "python3 ~/check.py", "timeout": 5}]}
    ],
    "Stop": [{"hooks": [{"type": "command", "command": "say done"}]}]
  }
}
`
	hp := filepath.Join(dir, HooksFile)
	if err := os.WriteFile(hp, []byte(userHooks), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Render(dir, want(plan("on-request", "workspace-write")), false); err != nil {
		t.Fatal(err)
	}
	doc := hookDoc(t, dir)
	if doc["description"] != "mine" {
		t.Fatalf("user keys kept: %v", doc)
	}
	hooks := doc["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 || !strings.Contains(toJSON(pre[0]), "check.py") || !strings.Contains(toJSON(pre[1]), PreToolUseCommand) {
		t.Fatalf("tq's entry is appended after the user's: %s", toJSON(pre))
	}
	if !strings.Contains(toJSON(hooks["SessionStart"]), SessionStartCommand) || hooks["Stop"] == nil {
		t.Fatalf("hooks: %s", toJSON(hooks))
	}
	if g, h, ok := FindHook(dir, "PreToolUse"); !ok || g != 1 || h != 0 {
		t.Fatalf("FindHook: %d %d %v", g, h, ok)
	}
	if res, _ := Render(dir, want(plan("on-request", "workspace-write")), false); len(res.Changed) != 0 {
		t.Fatalf("second render must be a no-op: %v", res.Changed)
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	doc = hookDoc(t, dir)
	if s := toJSON(doc); strings.Contains(s, "claude-hook") || !strings.Contains(s, "check.py") || !strings.Contains(s, "say done") {
		t.Fatalf("removal keeps only the user's hooks: %s", s)
	}
}

func TestRenderHooksOwnFileRemoved(t *testing.T) {
	dir := write(t, "")
	if _, err := Render(dir, want(plan("on-request", "workspace-write")), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFile(t, filepath.Join(dir, HooksFile)); !ok {
		t.Fatal("hooks.json written")
	}
	if _, err := Render(dir, Want{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok := readFile(t, filepath.Join(dir, HooksFile)); ok {
		t.Fatal("a hooks.json holding only tq's entries is removed")
	}
}

func TestRenderHooksBadJSONIsConflict(t *testing.T) {
	dir := write(t, "")
	hp := filepath.Join(dir, HooksFile)
	for _, bad := range []string{"{not json", `{"hooks": []}`, `{"hooks": {"PreToolUse": {}}}`} {
		if err := os.WriteFile(hp, []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Render(dir, want(plan("on-request", "workspace-write")), false)
		if err != nil || !conflictOn(res, "hooks.json") {
			t.Fatalf("%s: want a conflict: %+v %v", bad, res, err)
		}
		if b, _ := readFile(t, hp); b != bad {
			t.Fatal("must not touch it")
		}
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hasPath(xs []string, p string) bool {
	for _, x := range xs {
		if x == p {
			return true
		}
	}
	return false
}

func conflictOn(res Result, name string) bool {
	for _, c := range res.Conflicts {
		if strings.Contains(c, name) {
			return true
		}
	}
	return false
}
