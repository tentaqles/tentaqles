package packs

import (
	"sort"
	"strings"
)

// RaiseFlags evaluates the deterministic flags of the enabled packs over
// in's changed set, plus the framework's baseline_change:<pack>, and returns the
// raised names, sorted and de-duplicated. A Raise that panics counts as
// raised (fail safe: review more, never less).
func RaiseFlags(in *Input) []string {
	set := map[string]bool{}
	for _, p := range in.Config.Enabled() {
		pin := in.ForPack(p)
		for _, f := range Flags(p) {
			if f.Raise != nil && raise(f, pin) {
				set[f.Name] = true
			}
		}
		if BaselineGrew(pin) {
			// Per pack, so the crew adds only that pack's reviewer.
			set[FlagBaselineChange+":"+p] = true
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func raise(f Flag, in *Input) (up bool) {
	defer func() {
		if recover() != nil {
			up = true
		}
	}()
	return f.Raise(in)
}

// Crew returns the reviewer agents the given flags map to in the enabled
// packs, de-duplicated and sorted. A flag with a Raise or a Question maps
// when it is in flags; a reviewer-only flag maps when its core flag is.
func Crew(cfg Config, flags []string) []string {
	want := map[string]bool{}
	for _, f := range flags {
		if f = strings.TrimSpace(f); f != "" {
			want[f] = true
		}
	}
	set := map[string]bool{}
	for _, p := range cfg.Enabled() {
		for _, f := range Flags(p) {
			if want[f.Name] || (f.Name == FlagBaselineChange && want[FlagBaselineChange+":"+p]) {
				for _, r := range f.Reviewers {
					set[r] = true
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}
