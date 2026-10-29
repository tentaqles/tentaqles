package ml

// Guard tq/model-promote (spec C §4.4, §5.3): an agent never promotes a
// model itself. It denies registry promotion commands (MLflow REST, Python
// one-liners and heredocs calling the promotion APIs, SageMaker, Vertex AI,
// Databricks, Azure ML), the declared promotion command run directly, and
// tq pack ml promote apply (human only). The human runs them in their own
// terminal; the guard only sees agent tool calls. Pure: regexes over the
// call and the loaded config.

import (
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/policy"
)

const promoteDeny = "model promotion is gated: run tq pack ml promote request <model> <version> --to <target>; a human answers and runs tq pack ml promote apply in their own terminal"

var (
	wsRe         = regexp.MustCompile(`\s+`)
	applyRe      = regexp.MustCompile(`\btq(\.exe)?\s+pack\s+ml\s+promote\s+apply\b`)
	interpRe     = regexp.MustCompile(`(^|[\s;&|(/\\])(python[0-9.]*|py|ipython|jupyter|uv|poetry|pdm|pipenv|pixi|conda|hatch)(\.exe)?(\s|$)`)
	apiNameRe    = regexp.MustCompile(`\b(transition_model_version_stage|set_registered_model_alias|add_version_aliases|merge_version_aliases)\b`)
	pkgApproveRe = regexp.MustCompile(`\bupdate_model_package\b[\s\S]*approved`)
	writeHTTPRe  = regexp.MustCompile(`(^|\s)(-x\s*(post|put|patch|delete)|--request[\s=]+(post|put|patch|delete)|-d\b|--data(-raw|-binary|-urlencode)?\b|--json\b|-method\s+(post|put|patch|delete)|-body\b)`)
	sagemakerRe  = regexp.MustCompile(`\baws\s+sagemaker\s+update-model-package\b[\s\S]*--model-approval-status[\s=]+["']?approved\b`)
	gcloudRe     = regexp.MustCompile(`\bgcloud\s+(alpha\s+|beta\s+)?ai\s+models\b[\s\S]*--version-aliases\b`)
	databricksRe = regexp.MustCompile(`\bdatabricks\s+(registered-models\s+set-alias|model-registry\s+(transition-stage|approve-transition-request))\b`)
	azmlRe       = regexp.MustCompile(`\baz\s+ml\s+model\s+(update|create)\b[\s\S]*(\s--stage\b|\s-s\s)`)
	mcpRe        = regexp.MustCompile(`(transition_model_version_stage|transition_stage|set_registered_model_alias|registered_model_alias|merge_version_aliases|add_version_aliases|approve_transition_request)`)
)

// restPromotion: MLflow and Databricks registry REST endpoints that change
// a stage or an alias. Alias reads (GET) are allowed.
func restPromotion(c string) bool {
	switch {
	case strings.Contains(c, "model-versions/transition-stage"), strings.Contains(c, "transition-requests/approve"),
		strings.Contains(c, ":mergeversionaliases"):
		return true
	case strings.Contains(c, "registered-models/alias"),
		strings.Contains(c, "unity-catalog/models/") && strings.Contains(c, "/aliases/"):
		return writeHTTPRe.MatchString(c)
	}
	return false
}

// commandMarkers are lowercase literals that identify the declared promotion
// command: its script paths and "-m module" pairs, or its leading words.
func commandMarkers(cmd string) []string {
	argv, err := SplitCommand(cmd)
	if err != nil {
		return nil
	}
	var out []string
	for i, a := range argv {
		if placeRe.MatchString(a) {
			continue
		}
		a = strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(a, `\`, "/"), "./"))
		switch {
		case a == "-m" && i+1 < len(argv) && !placeRe.MatchString(argv[i+1]):
			out = append(out, "-m "+strings.ToLower(argv[i+1]))
		case strings.Contains(a, "/") || strings.HasSuffix(a, ".py") || strings.HasSuffix(a, ".sh") || strings.HasSuffix(a, ".ps1"):
			out = append(out, a)
		}
	}
	if len(out) > 0 {
		return out
	}
	var lead []string
	for _, a := range argv {
		if placeRe.MatchString(a) {
			break
		}
		lead = append(lead, strings.ToLower(a))
	}
	generic := map[string]bool{"uv": true, "run": true, "python": true, "python3": true, "poetry": true, "pdm": true, "pixi": true, "make": true, "just": true, "npm": true, "bash": true, "sh": true, "pwsh": true}
	for _, w := range lead {
		if !generic[w] {
			return []string{strings.Join(lead, " ")}
		}
	}
	return nil
}

func containsWord(hay, needle string) bool {
	for off := 0; ; {
		i := strings.Index(hay[off:], needle)
		if i < 0 {
			return false
		}
		at := off + i
		end := at + len(needle)
		beforeOK := at == 0 || strings.IndexByte(" \t\"'/=;&|(", hay[at-1]) >= 0
		afterOK := end == len(hay) || strings.IndexByte(" \t\"';&|)", hay[end]) >= 0
		if beforeOK && afterOK {
			return true
		}
		off = at + 1
	}
}

// checkModelPromote is the guard function.
func checkModelPromote(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	if call.IsMCP() {
		name := strings.ToLower(call.Tool)
		if mcpRe.MatchString(name) || (strings.Contains(name, "update_model_package") && strings.Contains(strings.ToLower(call.Content), "approved")) {
			return policy.Deny, "tq/model-promote: a registry promotion tool. " + promoteDeny, true
		}
		return policy.Allow, "", false
	}
	if !call.IsShell() {
		return policy.Allow, "", false
	}
	c := strings.ToLower(wsRe.ReplaceAllString(call.Command, " "))
	cs := strings.ReplaceAll(c, `\`, "/")
	why := ""
	// Cheap substring tests gate every regex, so a long command costs little.
	has := func(s string) bool { return strings.Contains(c, s) }
	switch {
	case has("promote") && applyRe.MatchString(c):
		why = "tq pack ml promote apply is human-only"
	case restPromotion(c):
		why = "a registry REST call that changes a stage or an alias"
	case (has("_model_version_stage") || has("registered_model_alias") || has("version_aliases") || has("update_model_package")) &&
		(apiNameRe.MatchString(c) || pkgApproveRe.MatchString(c)) && interpRe.MatchString(c):
		why = "Python code calling a registry promotion API"
	case has("update-model-package") && sagemakerRe.MatchString(c):
		why = "a SageMaker model package approval"
	case has("--version-aliases") && gcloudRe.MatchString(c):
		why = "a Vertex AI version alias change"
	case has("databricks") && databricksRe.MatchString(c):
		why = "a Databricks registry alias or stage change"
	case has(" ml ") && has("model") && azmlRe.MatchString(c):
		why = "an Azure ML model stage change"
	default:
		st := load(g.Settings())
		for _, mk := range commandMarkers(st.Promotion.Command) {
			if containsWord(cs, mk) {
				why = "the declared promotion command run directly"
				break
			}
		}
	}
	if why == "" {
		return policy.Allow, "", false
	}
	return policy.Deny, "tq/model-promote: " + why + ". " + promoteDeny, true
}
