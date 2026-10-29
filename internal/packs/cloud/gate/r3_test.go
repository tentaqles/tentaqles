package gate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// Integration round 3: a flag tq does not know, written before the verb
// and followed by a non-flag word, may take that word as its value or not,
// so tq cannot tell the verb. The command counts as an apply (or a destroy)
// at the strictest environment (prod here), never at the resolved one.
func TestUnknownLeadingFlagIsStrictest(t *testing.T) {
	f := selectFixture(t, "dev")
	for _, cmd := range []string{
		// the documented global flags keep the resolution (dev, auto)
		"terraform -chdir=infra apply -auto-approve",
		"terraform apply -auto-approve",
		"pulumi --non-interactive up --yes --stack dev",
		"pulumi --emoji --logtostderr up --yes --stack dev",
		"pulumi -C . up --yes --stack dev",
		"az --only-show-errors deployment group create -g rg-app --template-file main.bicep",
		"az --debug deployment group create -g rg-app --template-file main.bicep",
		"az --subscription " + devSub + " deployment group create -g rg-app --template-file main.bicep",
	} {
		if a, why, h, _ := f.run(ApplyEnv, cmd); a != policy.Allow || !h {
			t.Errorf("control %q: %v %v %q", cmd, a, h, why)
		}
	}
	for _, cmd := range []string{
		"terraform -foo x apply -auto-approve",
		"tofu -foo x destroy -auto-approve",
		"pulumi --foo x up --yes --stack dev",
		"pulumi --foo x y up --yes --stack dev",
		"cdk --foo x deploy",
		"cdktf --foo x deploy",
		"az --foo x deployment group create -g rg-app --template-file main.bicep",
		"az --foo x y deployment group create -g rg-app --template-file main.bicep",
		"aws --foo cloudformation deploy --stack-name s --template-file t.yaml",
		"aws --foo x y cloudformation deploy --stack-name s --template-file t.yaml",
		"sam --foo x deploy",
	} {
		// with default_environment: dev too, the floor is prod
		for _, mk := range []func(*testing.T) *fixture{func(t *testing.T) *fixture { return selectFixture(t, "dev") }, defaultDevFixture} {
			a, why, h, _ := mk(t).run(ApplyEnv, cmd)
			if a != policy.Deny || !h {
				t.Errorf("%q: %v %v %q", cmd, a, h, why)
				continue
			}
			if !strings.Contains(why, "prod") {
				t.Errorf("%q: the floor is the strictest environment (prod): %q", cmd, why)
			}
		}
	}
}

// defaultDevFixture is selectFixture with default_environment: dev, so an
// unresolved target counts as dev (auto) and only the strict floor is prod.
func defaultDevFixture(t *testing.T) *fixture {
	f := newFixture(t, "default_environment: dev"+"\n"+selectBlock)
	os.MkdirAll(filepath.Join(f.repo, ".terraform"), 0o755)
	os.WriteFile(filepath.Join(f.repo, ".terraform", "environment"), []byte("dev"), 0o644)
	return f
}
