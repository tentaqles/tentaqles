package model

import (
	"regexp"
	"sort"
	"strings"
)

// AllowTag is the resource tag that suppresses a built-in rule (spec D §5):
// tq-allow = "IAM002: <reason>". Several rules go in one value separated by
// ";": "IAM002: read-only role; IAC001: decommissioned".
const AllowTag = "tq-allow"

// Tags returns the resource's tags as a flat map, from the forms the inputs
// use: a "tags" (or "Tags") map, a CloudFormation Tags list of {Key, Value},
// and GCP "labels". Values that are not scalars are skipped.
func (r Resource) Tags() map[string]string {
	a := r.Attrs()
	if a == nil {
		return nil
	}
	out := map[string]string{}
	for _, key := range []string{"labels", "tags_all", "Tags", "tags"} { // later keys win
		switch t := a[key].(type) {
		case map[string]any:
			for k, v := range t {
				if s, ok := Str(v); ok {
					out[k] = s
				}
			}
		case []any:
			for _, item := range t {
				m := Map(item)
				k, ok1 := Str(m["Key"])
				v, ok2 := Str(m["Value"])
				if !ok1 {
					k, ok1 = Str(m["key"])
					v, ok2 = Str(m["value"])
				}
				if ok1 && ok2 {
					out[k] = v
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Suppression is one entry of a tq-allow tag. Reason "" is a suppression
// without a reason, which the rules must report as an error finding. Rule ""
// means the entry could not be parsed; Raw holds it.
type Suppression struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
	Raw    string `json:"raw,omitempty"`
}

// Valid reports a parsed rule id with a non-empty reason.
func (s Suppression) Valid() bool { return s.Rule != "" && s.Reason != "" }

var ruleIDRe = regexp.MustCompile(`^[A-Z]{2,5}[0-9]{3}$`)

// Suppressions parses the resource's tq-allow tag (the key is matched
// without regard to case, and tq_allow is accepted for providers that
// reject "-" in keys). When several keys match, "tq-allow" wins, then
// another case of it, then "tq_allow". Entries are returned sorted by rule.
func (r Resource) Suppressions() []Suppression {
	// A fixed order when several keys match: "tq-allow" exactly, then
	// "tq-allow" in another case, then "tq_allow" in any case; ties by key.
	var raw, best string
	bestRank := 99
	for k, v := range r.Tags() {
		rank := 99
		switch lk := strings.ToLower(k); {
		case k == AllowTag:
			rank = 0
		case lk == AllowTag:
			rank = 1
		case lk == "tq_allow":
			rank = 2
		}
		if rank < bestRank || (rank == bestRank && rank < 99 && k < best) {
			raw, best, bestRank = v, k, rank
		}
	}
	return ParseSuppressions(raw)
}

// ParseSuppressions parses a tq-allow value.
func ParseSuppressions(raw string) []Suppression {
	var out []Suppression
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		rule, reason, _ := strings.Cut(part, ":")
		rule = strings.ToUpper(strings.TrimSpace(rule))
		reason = strings.TrimSpace(reason)
		if !ruleIDRe.MatchString(rule) {
			out = append(out, Suppression{Raw: part})
			continue
		}
		out = append(out, Suppression{Rule: rule, Reason: reason})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rule < out[j].Rule })
	return out
}

// Allowed returns the suppression for rule, if the tag names it.
func (r Resource) Allowed(rule string) (Suppression, bool) {
	for _, s := range r.Suppressions() {
		if s.Rule == rule {
			return s, true
		}
	}
	return Suppression{}, false
}
