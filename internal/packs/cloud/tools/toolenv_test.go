package tools_test

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// wantEnv is what each external check declares (Check.Env): the variables
// the tool needs beyond toolrun.BaseEnv, and nothing that holds a
// credential.
var wantEnv = map[string][]string{
	"cloud/checkov":    {"APPDATA"},
	"cloud/trivy":      {"TRIVY_CACHE_DIR", "XDG_CACHE_HOME", "LOCALAPPDATA"},
	"cloud/conftest":   nil,
	"cloud/tflint":     {"TFLINT_PLUGIN_DIR", "TFLINT_CONFIG_FILE", "TF_DATA_DIR", "TF_WORKSPACE"},
	"cloud/bicep-lint": nil,
	"cloud/cfn-lint":   {"APPDATA", "AWS_REGION", "AWS_DEFAULT_REGION"},
}

// wantTool is the tool each external check runs.
var wantTool = map[string]string{
	"cloud/checkov": "checkov", "cloud/trivy": "trivy", "cloud/conftest": "conftest",
	"cloud/tflint": "tflint", "cloud/bicep-lint": "bicep", "cloud/cfn-lint": "cfn-lint",
}

func TestExternalChecksDeclareTheirEnv(t *testing.T) {
	got := map[string][]string{}
	for _, c := range packs.Checks("cloud") {
		if c.Kind == packs.External {
			got[c.ID] = c.Env
		}
	}
	if len(got) != len(wantEnv) {
		t.Fatalf("external checks = %v", got)
	}
	for id, want := range wantEnv {
		if !reflect.DeepEqual(got[id], want) {
			t.Errorf("%s Env = %v, want %v", id, got[id], want)
		}
	}
}

// TestToolsSeeOnlyTheirEnv runs every external check with each declared
// name set, plus credentials and an undeclared variable, and reads back
// which of them each fake tool saw.
func TestToolsSeeOnlyTheirEnv(t *testing.T) {
	isolate(t)
	withPlan(t)
	f := installFakes(t, "checkov", "trivy", "tflint", "bicep", "cfn-lint", "conftest")
	f.out(t, "checkov", "checkov.json", 0)
	f.out(t, "trivy", "trivy.json", 0)
	f.out(t, "tflint", "tflint.json", 2)
	f.out(t, "cfn-lint", "cfn-lint.json", 6)
	f.out(t, "conftest", "conftest.json", 0)
	f.out(t, "bicep", "bicep.sarif", 1)
	all := map[string]bool{"TQ_CP_UNDECLARED": true, "GITHUB_TOKEN": true, "BC_API_KEY": true, "AWS_SECRET_ACCESS_KEY": true, "TF_VAR_db_password": true}
	for _, ns := range wantEnv {
		for _, n := range ns {
			all[n] = true
		}
	}
	var names []string
	for n := range all {
		names = append(names, n)
		t.Setenv(n, filepath.Join(t.TempDir(), "x"))
	}
	sort.Strings(names)
	t.Setenv("TQ_FAKE_ENV_NAMES", strings.Join(names, ","))

	root, changed, tracked := repo(t)
	in := input(t, "[checkov, trivy, conftest, tflint, bicep, cfn-lint]", "", root, changed, tracked)
	f.runner(in)
	packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})

	for id, want := range wantEnv {
		tool := wantTool[id]
		b, err := os.ReadFile(filepath.Join(f.argv, tool+".env"))
		if err != nil {
			t.Errorf("%s did not run: %v", tool, err)
			continue
		}
		var seen []string
		if s := strings.TrimSpace(string(b)); s != "" {
			seen = strings.Split(s, ",")
		}
		var w []string
		w = append(w, want...)
		sort.Strings(w)
		if !reflect.DeepEqual(seen, w) {
			t.Errorf("%s saw %v, want %v", tool, seen, w)
		}
	}
}
