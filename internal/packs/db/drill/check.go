package drill

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// CheckID is the drill freshness check (§5, §13.6).
const CheckID = "db/restore-drill"

// Now is the clock (tests replace it).
var Now = func() time.Time { return time.Now().UTC() }

// RunCheck is db/restore-drill: every declared drill needs a fresh ok line
// in docs/ops/restore-drills.jsonl (the working tree, or the index with
// --staged). The framework runs it only when a drill is declared and a
// migration changed (decided at spec review); otherwise doctor warns.
func RunCheck(in *packs.Input) packs.Result {
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	drills, err := Parse(in.Settings())
	if err != nil {
		res.Status, res.Detail = packs.Fail, "packs.db.drill is invalid: "+err.Error()
		return res
	}
	if len(drills) == 0 {
		res.Status, res.Detail = packs.Skip, "no drill is declared"
		return res
	}
	raw, err := in.ReadNew(EvidenceFile, MaxEvidenceBytes)
	if err != nil {
		// a missing file (or one absent from the index) records no drill; a
		// file over the cap or unreadable cannot prove one: both fail below
		if !errors.Is(err, fs.ErrNotExist) {
			res.Findings = append(res.Findings, packs.Finding{File: EvidenceFile, Level: packs.LevelWarn, Rule: "drill-log",
				Message: "the evidence log cannot be read (missing from the index, unreadable, or over 1 MiB)"})
		}
		raw = nil
	}
	entries, bad := ParseLog(raw)
	for _, ln := range bad {
		res.Findings = append(res.Findings, packs.Finding{File: EvidenceFile, Line: ln, Level: packs.LevelWarn, Rule: "drill-line",
			Message: "this line is not a drill evidence line tq wrote; it never counts"})
	}
	now := Now()
	var stale, ok []string
	for _, d := range drills {
		f := Check(entries, d, now)
		if f.Found {
			packs.Count("db", "drill_age_days:"+d.Name, int(f.Age.Hours()/24))
		}
		if f.Fresh {
			ok = append(ok, fmt.Sprintf("%s %s old", d.Name, Days(f.Age)))
			continue
		}
		stale = append(stale, d.Name)
		res.Findings = append(res.Findings, packs.Finding{File: EvidenceFile, Level: packs.LevelError, Rule: "drill-stale",
			Message: fmt.Sprintf("drill %s: %s; %s", d.Name, f.Why, StaleText(d.Name))})
	}
	if len(stale) > 0 {
		res.Status = packs.Fail
		res.Detail = fmt.Sprintf("%d of %d drills are not fresh: %s", len(stale), len(drills), strings.Join(stale, ", "))
		return res
	}
	res.Detail = fmt.Sprintf("%d drills fresh (%s)", len(drills), joinWhy(ok))
	return res
}
