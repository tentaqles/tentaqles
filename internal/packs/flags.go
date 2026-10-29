package packs

import (
	"sort"
	"strings"
)

// RaiseFlags evaluates the deterministic flags of the enabled packs over
// in's changed set, plus the framework's baseline_change, and returns the
// raised names, sorted and de-duplicated. A Raise that panics counts as
// raised (fail safe: review more, never less).
func RaiseFlags(in *Input) []string {
	names, _ := RaiseFlagsDetail(in)
	return names
}

// RaiseFlagsDetail is RaiseFlags plus each flag's detail. The detail of
// baseline_change is the comma-separated packs whose baseline grew, which
// CrewDetail uses to add only those packs' reviewers.
func RaiseFlagsDetail(in *Input) ([]string, map[string]string) {
	set := map[string]bool{}
	var grew []string
	for _, p := range in.Config.Enabled() {
		pin := in.ForPack(p)
		for _, f := range Flags(p) {
			if f.Raise != nil && raise(f, pin) {
				set[f.Name] = true
			}
		}
		if BaselineGrew(pin) {
			set[FlagBaselineChange] = true
			grew = append(grew, p)
		}
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Strings(out)
	detail := map[string]string{}
	if len(grew) > 0 {
		sort.Strings(grew)
		detail[FlagBaselineChange] = strings.Join(grew, ",")
	}
	return out, detail
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
// baseline_change without a detail maps to every enabled pack's
// baseline_change reviewers (fail safe); see CrewDetail.
func Crew(cfg Config, flags []string) []string { return CrewDetail(cfg, flags, nil) }

// CrewDetail is Crew with the triage flag details: baseline_change's detail
// (the packs whose baseline grew) limits its reviewers to those packs.
func CrewDetail(cfg Config, flags []string, detail map[string]string) []string {
	want := map[string]bool{}
	for _, f := range flags {
		if f = strings.TrimSpace(f); f != "" {
			want[f] = true
		}
	}
	var grew map[string]bool
	if d := strings.TrimSpace(detail[FlagBaselineChange]); d != "" {
		grew = map[string]bool{}
		for _, p := range strings.Split(d, ",") {
			grew[strings.TrimSpace(p)] = true
		}
	}
	set := map[string]bool{}
	for _, p := range cfg.Enabled() {
		for _, f := range Flags(p) {
			if !want[f.Name] {
				continue
			}
			if f.Name == FlagBaselineChange && grew != nil && !grew[p] {
				continue
			}
			for _, r := range f.Reviewers {
				set[r] = true
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
