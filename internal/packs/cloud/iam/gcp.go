package iam

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
)

// gcpIAMRe matches the google provider's IAM grant resources:
// google_<resource>_iam_member, _iam_binding and _iam_policy. GCP001 reads
// Terraform plans only (decided at spec review).
var gcpIAMRe = regexp.MustCompile(`^google_[a-z0-9_]+_iam_(member|binding|policy)$`)

type grant struct {
	role    string
	members []string
}

func gcpRules(t string, attrs map[string]any) []hit {
	var grants []grant
	switch {
	case strings.HasSuffix(t, "_iam_member"):
		m, _ := model.Str(attrs["member"])
		grants = append(grants, grant{first(attrs, "role"), []string{m}})
	case strings.HasSuffix(t, "_iam_binding"):
		grants = append(grants, grant{first(attrs, "role"), model.Strings(attrs["members"])})
	case strings.HasSuffix(t, "_iam_policy"):
		s, _ := attrs["policy_data"].(string)
		var pd struct {
			Bindings []struct {
				Role    string   `json:"role"`
				Members []string `json:"members"`
			} `json:"bindings"`
		}
		if s != "" && !model.IsRedacted(s) && json.Unmarshal([]byte(s), &pd) == nil {
			for _, b := range pd.Bindings {
				grants = append(grants, grant{b.Role, b.Members})
			}
		}
	}
	var hits []hit
	for _, g := range grants {
		for _, r := range gcpPrimitiveRoles {
			if strings.EqualFold(strings.TrimSpace(g.role), r) {
				hits = append(hits, hit{"GCP001", packs.LevelError, "grants the primitive role " + r + "; grant a predefined or custom role"})
			}
		}
		for _, m := range g.members {
			for _, pub := range gcpPublicMembers {
				if strings.TrimSpace(m) == pub {
					hits = append(hits, hit{"GCP001", packs.LevelError, "grants " + strings.TrimSpace(g.role) + " to " + pub + " (public)"})
				}
			}
		}
	}
	return hits
}
