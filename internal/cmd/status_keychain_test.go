package cmd

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// L-05: status --json reads the integrity key through resolve, which has no deadline of its own.
// A key that does not answer within the limit gives the untrusted shape, naming the timeout, and
// the command returns in time. trust keeps its keychain seam unexported, so the test blocks the
// resolve that waits on the key (statusResolve) and runs the command as a user would.
func TestStatusJSON_BlockingIntegrityKeyIsBounded(t *testing.T) {
	root := mkUntrusted(t, "acme")
	chdir(t, root)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	prevResolve, prevLimit := statusResolve, statusKeyLimit
	statusResolve = func(string, *registry.Config) resolve.Result {
		<-release
		return resolve.Result{}
	}
	statusKeyLimit = 200 * time.Millisecond
	t.Cleanup(func() { statusResolve, statusKeyLimit = prevResolve, prevLimit })

	start := time.Now()
	out, err := runTq(t, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v\n%s", err, out)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("status --json took %s with a 200ms key limit", d)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("not one JSON object: %v\n%s", err, out)
	}
	if obj["identity"] != "acme" || obj["trusted"] != false {
		t.Errorf("identity/trusted: %v %v", obj["identity"], obj["trusted"])
	}
	reason, _ := obj["reason"].(string)
	if !strings.Contains(reason, "did not answer within 200ms") {
		t.Errorf("the reason does not name the timeout: %q", reason)
	}
	if r, _ := obj["root"].(string); !strings.EqualFold(filepath.Clean(r), filepath.Clean(root)) {
		t.Errorf("root = %q, want %q", r, root)
	}
	if p, ok := obj["providers"].([]any); !ok || len(p) != 0 {
		t.Errorf("providers = %v, want none", obj["providers"])
	}
	if s, ok := obj["scope"].([]any); !ok || len(s) != 0 {
		t.Errorf("scope = %v, want none", obj["scope"])
	}
}

// A folder in no base has no identity to name: the timeout reads as neutral, identity null.
func TestStatusJSON_BlockingResolveInNeutralFolder(t *testing.T) {
	isolateHome(t)
	chdir(t, t.TempDir())
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	prevResolve, prevLimit := statusResolve, statusKeyLimit
	statusResolve = func(string, *registry.Config) resolve.Result {
		<-release
		return resolve.Result{}
	}
	statusKeyLimit = 100 * time.Millisecond
	t.Cleanup(func() { statusResolve, statusKeyLimit = prevResolve, prevLimit })

	out, err := runTq(t, "status", "--json")
	if err != nil {
		t.Fatalf("status --json: %v\n%s", err, out)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(out), &obj); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if v, ok := obj["identity"]; !ok || v != nil {
		t.Errorf("identity = %v, want null", v)
	}
	if reason, _ := obj["reason"].(string); !strings.Contains(reason, "did not answer within 100ms") {
		t.Errorf("reason = %q", reason)
	}

}
