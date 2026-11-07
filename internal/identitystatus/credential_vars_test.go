package identitystatus

import (
	"encoding/json"
	"strings"
	"testing"
)

// L-20: a provider whose catalog entry names credential variables and has no CLI login reads
// credential missing when none of them is set, and present when one is. az, a CLI login, keeps
// none when nothing is set (the router's decision). The variable's value never reaches the output.
func TestStatusJSON_CredentialVariablesWithoutLoginReadMissing(t *testing.T) {
	res := workspace(t)
	res.Workspace.Manifest.ExtraScope = append(res.Workspace.Manifest.ExtraScope, "trello", "stripe", "notion", "az")
	unset := func(string) (string, bool) { return "", false }
	base := Deps{RunCLI: noRun, Environ: noEnv, LookPath: notFound, Env: unset}

	got := byID(Build(res, base))
	for _, id := range []string{"trello", "stripe", "notion"} {
		if p := got[id]; p.Credential != Missing {
			t.Errorf("%s with no credential variable set = %q, want missing", id, p.Credential)
		}
	}
	if p := got["az"]; p.Credential != None {
		t.Errorf("az (a CLI login) with nothing set = %q, want none", p.Credential)
	}

	const sentinel = "SENTINEL_VALUE_L20"
	for id, name := range map[string]string{"trello": "TRELLO_API_TOKEN", "stripe": "STRIPE_API_KEY", "notion": "NOTION_TOKEN"} {
		d := base
		d.Env = func(k string) (string, bool) {
			if k == name {
				return sentinel, true
			}
			return "", false
		}
		s := Build(res, d)
		if p := byID(s)[id]; p.Credential != Present {
			t.Errorf("%s with %s set = %q, want present", id, name, p.Credential)
		}
		b, err := json.Marshal(s)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), sentinel) {
			t.Errorf("%s: the variable's value reached the output", id)
		}
	}
}
