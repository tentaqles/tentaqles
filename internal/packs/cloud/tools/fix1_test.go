package tools_test

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// oldGit serves the old side of the diff from a map.
type oldGit struct {
	files []string
	old   map[string]string
}

func (g oldGit) Show(ref, p string) ([]byte, error) {
	if b, ok := g.old[p]; ok {
		return []byte(b), nil
	}
	return nil, os.ErrNotExist
}
func (g oldGit) ListFiles() ([]string, error) { return g.files, nil }

// TestFlagsDeletions: deleting an IaC file (or renaming it away) raises the
// iac flag, as a change does (review r0 #1).
func TestFlagsDeletions(t *testing.T) {
	isolate(t)
	root, _, tracked := repo(t)
	old := map[string]string{
		"sam/template.yaml": "AWSTemplateFormatVersion: '2010-09-09'\nResources: {}\n",
		"k8s/template.yaml": "apiVersion: v1\nkind: ConfigMap\n",
	}
	cases := []struct {
		name  string
		c     packs.ChangedFile
		raise bool
	}{
		{"deleted under an iac path", packs.ChangedFile{Path: "infra/terraform/database.tf", Status: 'D'}, true},
		{"deleted non-tf under an iac path", packs.ChangedFile{Path: "infra/cfn/db.json", Status: 'D'}, true},
		{"deleted *.tf anywhere", packs.ChangedFile{Path: "modules/net/main.tf", Status: 'D'}, true},
		{"deleted bicep", packs.ChangedFile{Path: "apps/web/main.bicep", Status: 'D'}, true},
		{"renamed away from an iac path", packs.ChangedFile{Path: "attic/database.txt", OldPath: "infra/terraform/database.tf", Status: 'R'}, true},
		{"deleted CFN template.yaml", packs.ChangedFile{Path: "sam/template.yaml", Status: 'D'}, true},
		{"deleted non-CFN template.yaml", packs.ChangedFile{Path: "k8s/template.yaml", Status: 'D'}, false},
		{"deleted unreadable template.yaml", packs.ChangedFile{Path: "gone/template.yaml", Status: 'D'}, true},
		{"deleted README", packs.ChangedFile{Path: "README.md", Status: 'D'}, false},
		{"renamed README", packs.ChangedFile{Path: "docs/README.md", OldPath: "README.md", Status: 'R'}, false},
	}
	for _, c := range cases {
		in := input(t, "[]", "", root, []packs.ChangedFile{c.c}, tracked)
		in.Git = oldGit{files: tracked, old: old}
		if got := contains(packs.RaiseFlags(in), "iac"); got != c.raise {
			t.Errorf("%s: iac raised %v, want %v", c.name, got, c.raise)
		}
	}
}

// TestIaCPolicyDeletionWithoutPlan: a PR that only deletes a file under an
// IaC path runs cloud/iac-policy, which warns that no plan shows what the
// deletion destroys; the external tools, which need files, stay off.
func TestIaCPolicyDeletionWithoutPlan(t *testing.T) {
	isolate(t)
	root, _, tracked := repo(t)
	changed := []packs.ChangedFile{{Path: "infra/terraform/database.tf", Status: 'D'}}
	in := input(t, "[]", "", root, changed, tracked)
	rep := packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}})
	r, ok := byID(rep)["cloud/iac-policy"]
	if !ok || r.Status == packs.Skip {
		t.Fatalf("iac-policy did not run on a deletion: %+v", rep.Results)
	}
	want := []string{"infra/terraform/database.tf:0 warn IAC001"}
	if !reflect.DeepEqual(findings(r), want) || !strings.Contains(r.Findings[0].Message, "no recorded plan") {
		t.Errorf("iac-policy findings = %v (%+v)", findings(r), r.Findings)
	}
	for _, x := range rep.Results {
		if strings.HasPrefix(x.ID, "cloud/") && x.ID != "cloud/iac-policy" && x.ID != "cloud/no-plan-files" && x.ID != "cloud/baseline" && x.Status != packs.Skip {
			t.Errorf("%s ran on a deletion only: %+v", x.ID, x)
		}
	}

	// With a recorded plan, the plan says what is destroyed: no note.
	withPlan(t)
	rep = packs.RunChecks(context.Background(), in, packs.RunOptions{Packs: []string{"cloud"}, BuiltinOnly: true})
	for _, f := range byID(rep)["cloud/iac-policy"].Findings {
		if f.File == "infra/terraform/database.tf" {
			t.Errorf("deletion note with a recorded plan: %+v", f)
		}
	}
}
