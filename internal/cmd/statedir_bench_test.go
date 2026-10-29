package cmd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/policy"
)

// stateDirLoad isolates the homes and sets 500 extra variables, then returns a
// 64 KB command that mentions the bare root and changes directory.
func stateDirLoad(tb testing.TB) (policy.ToolCall, string) {
	tb.Helper()
	dir := tb.TempDir()
	tb.Setenv("TQ_HOME", filepath.Join(dir, ".tentaqles"))
	tb.Setenv("HOME", dir)
	tb.Setenv("USERPROFILE", dir)
	for i := 0; i < 500; i++ {
		tb.Setenv(fmt.Sprintf("TQ_BENCH_VAR_%03d", i), fmt.Sprintf("C:/bench/value/%03d", i))
	}
	var b strings.Builder
	b.WriteString("cd ~/.tentaqles && ls; ")
	for b.Len() < 64*1024 {
		b.WriteString("echo some/ordinary/path/../file.txt --flag=value %41 + more words; ")
	}
	return policy.ToolCall{Tool: "Bash", Command: b.String()}, dir
}

// F5: the hook-time state-dir check stays fast on a large call.
func BenchmarkStateDirDecision(b *testing.B) {
	call, cwd := stateDirLoad(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stateDirDecision(call, cwd)
	}
}

func TestStateDirDecisionUnder50ms(t *testing.T) {
	call, cwd := stateDirLoad(t)
	stateDirDecision(call, cwd) // fill the per-process cache
	var runs []time.Duration
	for i := 0; i < 9; i++ {
		start := time.Now()
		stateDirDecision(call, cwd)
		runs = append(runs, time.Since(start))
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i] < runs[j] })
	if m := runs[len(runs)/2]; m > 50*time.Millisecond {
		t.Fatalf("median %v, want under 50ms", m)
	}
}
