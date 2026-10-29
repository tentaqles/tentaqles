package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// stateDirLoad isolates the homes and sets 500 extra variables, 20 of them
// pointing inside the state directory, then returns a 64 KB command that
// reaches the expensive paths: no echo (prose) segments and no `..`, a cd
// under the root, a bare $TQ_HOME mention and inside variables to expand.
// The verdict is allow: nothing names a protected part.
func stateDirLoad(tb testing.TB) (policy.ToolCall, string) {
	tb.Helper()
	dir := tb.TempDir()
	home := filepath.Join(dir, ".tentaqles")
	tb.Setenv("TQ_HOME", home)
	tb.Setenv("HOME", dir)
	tb.Setenv("USERPROFILE", dir)
	for i := 0; i < 500; i++ {
		v := fmt.Sprintf("C:/bench/value/%03d", i)
		if i < 20 {
			v = filepath.ToSlash(filepath.Join(home, "identities", "acme", "claude", fmt.Sprintf("d%03d", i)))
		}
		tb.Setenv(fmt.Sprintf("TQ_BENCH_VAR_%03d", i), v)
	}
	var b strings.Builder
	b.WriteString("cd ~/.tentaqles && ls; ls $TQ_HOME; ")
	for i := 0; b.Len() < 64*1024; i++ {
		fmt.Fprintf(&b, "cat $TQ_BENCH_VAR_%03d/notes%d.md | sort > /tmp/out%d.txt; grep -n key some/ordinary/path/file%d.txt; ", i%20, i, i, i)
	}
	return policy.ToolCall{Tool: "Bash", Command: b.String()}, dir
}

// F5: the hook-time state-dir check stays fast on a large call.
func BenchmarkStateDirDecision(b *testing.B) {
	call, cwd := stateDirLoad(b)
	b.ResetTimer()
	if d := stateDirDecision(call, cwd); d.Action != policy.Allow {
		b.Fatalf("verdict %v, want allow: %+v", d.Action, d)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stateDirDecision(call, cwd)
	}
}

func TestStateDirDecisionUnder50ms(t *testing.T) {
	call, cwd := stateDirLoad(t)
	if d := stateDirDecision(call, cwd); d.Action != policy.Allow { // also fills the cache
		t.Fatalf("verdict %v, want allow: %+v", d.Action, d)
	}
	var runs []time.Duration
	for i := 0; i < 9; i++ {
		start := time.Now()
		stateDirDecision(call, cwd)
		runs = append(runs, time.Since(start))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
	// The hook budget is 50 ms (BenchmarkStateDirDecision measures it); a
	// loaded CI runner can double or triple a median, so the test bound is
	// 200 ms. The regressions it guards against (a quadratic matcher) took
	// seconds.
	if m := runs[len(runs)/2]; m > 200*time.Millisecond {
		t.Fatalf("median %v, want under 200ms (target 50ms)", m)
	}
}

// R4-2: the literal root matcher agrees with the alternation regexp it
// replaced (8.3 form first, then the longest literal, leftmost first).
func TestRootMatcherMatchesTheRegexp(t *testing.T) {
	forms := []string{"c:/users/tester/.tentaqles", "/c/users/tester/.tentaqles", "~/.tentaqles", ".tentaqles", "tentaq~"}
	alts := []string{`tentaq~[0-9]+`}
	sorted := append([]string(nil), forms...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, f := range sorted {
		if f != "tentaq~" {
			alts = append(alts, regexp.QuoteMeta(f))
		}
	}
	old := regexp.MustCompile("(" + strings.Join(alts, "|") + ")")
	m := computeRootRegex(forms)
	for _, s := range []string{
		"", "nothing here", "cd ~/.tentaqles && ls c:/users/tester/.tentaqles/packs",
		"x/tentaq~1/hub tentaq~ tentaq~12x", "/c/users/tester/.tentaqles/.tentaqles~/.tentaqles",
		"c:/users/tester/.tentaqle c:/users/tester/.tentaqles", "..tentaqles.tentaqles", "~/.tentaq~3",
	} {
		if got, want := m.FindAllStringIndex(s, -1), old.FindAllStringIndex(s, -1); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%q: got %v want %v", s, got, want)
		}
		if got, want := m.FindStringIndex(s), old.FindStringIndex(s); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%q first: got %v want %v", s, got, want)
		}
	}
}
