package testutil

import (
	"runtime"
	"sort"
	"testing"
	"time"
)

// Latency tests ("a guard on a 64 KiB command stays inside its 50 ms
// budget") cannot compare one wall-clock reading with a production budget:
// a loaded CI runner (GitHub's Windows and macOS runners, go test running
// many packages at once, -cpu 1) has been measured at four times the
// budget for a guard that is fine. What these tests exist to catch is a
// guard whose cost grows faster than its input (a quadratic scan took
// seconds) or a guard that blocks. CheckLatency checks both in a way load
// cannot flake:
//
//   - scaling: the fastest of several runs on the full input is at most
//     MaxGrowth times the fastest run on an eighth of it. The runs are
//     interleaved, so a load spike slows both sizes; the minimum is the
//     run least disturbed by other work. Linear work grows 8x, quadratic
//     work 64x.
//   - ceiling: the median on the full input stays under the budget times
//     LoadFactor, a documented test-only allowance for a loaded machine.
//
// The production budgets are unchanged and still enforced at runtime; the
// Benchmark* functions measure them on a quiet machine.
const (
	// ScaleDown is how much smaller the small input is than the full one.
	ScaleDown = 8
	// MaxGrowth bounds how much slower the full input may be than the small
	// one: 3x the linear 8x, well under the quadratic 64x.
	MaxGrowth = 24
	// LoadFactor is the test-only allowance over a production budget for a
	// loaded runner (measured: 4x on macOS and Windows CI). Only a blocking
	// or quadratic regression comes near it.
	LoadFactor  = 20
	latencyRuns = 9
)

// CheckLatency runs build(1) (the full input) and build(ScaleDown) (an
// eighth of it) interleaved and fails t when the cost does not scale
// linearly or the full input's median is over budget*LoadFactor. build
// returns the operation to time; it is called once per size, outside the
// timing.
func CheckLatency(t testing.TB, name string, budget time.Duration, build func(div int) func()) {
	t.Helper()
	CheckLatencyN(t, name, budget, latencyRuns, build)
}

// CheckLatencyN is CheckLatency with runs runs of each size, for an
// operation too slow to run nine times (an odd runs keeps the median a
// measured run).
func CheckLatencyN(t testing.TB, name string, budget time.Duration, runs int, build func(div int) func()) {
	t.Helper()
	full, small := build(1), build(ScaleDown)
	full() // warm caches and lazily compiled regexes outside the timing
	small()
	var fulls, smalls []time.Duration
	for i := 0; i < runs; i++ {
		smalls = append(smalls, timeOnce(small))
		fulls = append(fulls, timeOnce(full))
	}
	sortDurations(fulls)
	sortDurations(smalls)
	fmin, smin, fmed := fulls[0], smalls[0], fulls[len(fulls)/2]
	floor := smin
	if floor < time.Millisecond {
		floor = time.Millisecond // timer noise dominates under a millisecond
	}
	t.Logf("%s: full input min %v median %v; 1/%d input min %v", name, fmin, fmed, ScaleDown, smin)
	if r := float64(fmin) / float64(floor); r > MaxGrowth {
		t.Errorf("%s: %dx the input took %.1fx as long (%v vs %v): the cost grows faster than the input", name, ScaleDown, r, fmin, smin)
	}
	if limit := budget * LoadFactor; fmed > limit {
		t.Errorf("%s: median %v on the full input, over %v (the %v budget x%d for a loaded machine)", name, fmed, limit, budget, LoadFactor)
	}
}

// MedianUnder fails t when the median of several runs of f is over
// budget*LoadFactor. It is the ceiling half of CheckLatency, for an
// operation whose input cannot be scaled.
func MedianUnder(t testing.TB, name string, budget time.Duration, f func()) {
	t.Helper()
	f()
	var ds []time.Duration
	for i := 0; i < latencyRuns; i++ {
		ds = append(ds, timeOnce(f))
	}
	sortDurations(ds)
	med := ds[len(ds)/2]
	t.Logf("%s: min %v median %v", name, ds[0], med)
	if limit := budget * LoadFactor; med > limit {
		t.Errorf("%s: median %v, over %v (the %v budget x%d for a loaded machine)", name, med, limit, budget, LoadFactor)
	}
}

func timeOnce(f func()) time.Duration {
	runtime.GC() // a collection owed by an earlier run is not charged to this one
	start := time.Now()
	f()
	return time.Since(start)
}

func sortDurations(ds []time.Duration) {
	sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
}
