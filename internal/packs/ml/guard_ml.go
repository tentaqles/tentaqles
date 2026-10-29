package ml

// Guard tq/model-promote (spec C §4.4, §5.3): an agent never promotes a
// model itself. It denies registry promotion commands (MLflow REST, Python
// one-liners and heredocs calling the promotion APIs, SageMaker, Vertex AI,
// Databricks, Azure ML), the declared promotion command run directly, and
// tq pack ml promote apply (human only). The human runs them in their own
// terminal; the guard only sees agent tool calls. Pure: regexes over the
// call and the loaded config.

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
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
	// The registry CLIs: global options (--profile, --region, -p, --project)
	// may sit between the binary and the service, within one command
	// segment ([^;&|]* never crosses a ; & or |).
	sagemakerRe  = regexp.MustCompile(`\baws(\.exe|\.cmd)?\s([^;&|]*\s)?sagemaker\s+update-model-package\b[\s\S]*(--model-approval-status[\s=]+["']?approved\b|--cli-input-(json|yaml)\b[\s\S]*modelapprovalstatus[\s\S]*approved)`)
	gcloudRe     = regexp.MustCompile(`\bgcloud(\.exe|\.cmd)?\s([^;&|]*\s)?(alpha\s+|beta\s+)?ai\s+models\b[\s\S]*--version-aliases\b`)
	databricksRe = regexp.MustCompile(`\bdatabricks(\.exe)?\s([^;&|]*\s)?(registered-models\s+set-alias|model-registry\s+(transition-stage|approve-transition-request))\b`)
	azmlRe       = regexp.MustCompile(`\baz(\.exe|\.cmd)?\s([^;&|]*\s)?ml\s+model\s+(update|create)\b[\s\S]*(\s--stage\b|\s-s\s)`)
	// HTTP writes by other clients: wget, httpie/xh, and Python's requests,
	// httpx and urllib in an interpreter command.
	writeHTTP2Re = regexp.MustCompile(`(--post-data\b|--post-file\b|--method[\s=]+["']?(post|put|patch|delete)\b|\b(post|put|patch|delete)\s+["']?https?://|\b(requests|httpx)\.(post|put|patch|delete)\s*\(|\b(requests|httpx)\.request\s*\(\s*["'](post|put|patch|delete)|\burllib[\s\S]*method\s*=\s*["'](post|put|patch|delete))`)
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
		return writeHTTPRe.MatchString(c) || writeHTTP2Re.MatchString(c)
	}
	return false
}

// commandMarkers are lowercase literals that identify the declared promotion
// command: its script paths and "-m module" pairs, or its leading words
// (matched anywhere). For leading words, atStart also holds the words from
// the first non-generic one on ("promote-model" of "uv run promote-model"):
// a console script run without its runner, matched only where a command
// starts so "tq pack ml promote request" is not mistaken for "promote".
func commandMarkers(cmd string) (anywhere, atStart []string) {
	argv, err := SplitCommand(cmd)
	if err != nil {
		return nil, nil
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
		return out, nil
	}
	var lead []string
	for _, a := range argv {
		if placeRe.MatchString(a) {
			break
		}
		lead = append(lead, strings.ToLower(a))
	}
	generic := map[string]bool{"uv": true, "run": true, "python": true, "python3": true, "poetry": true, "pdm": true, "pixi": true, "make": true, "just": true, "npm": true, "bash": true, "sh": true, "pwsh": true}
	for i, w := range lead {
		if !generic[w] {
			full := strings.Join(lead, " ")
			if i == 0 {
				return []string{full}, nil
			}
			return []string{full}, []string{strings.Join(lead[i:], " ")}
		}
	}
	return nil, nil
}

// startsCommand reports whether needle appears as a whole word that begins a
// command: at the start, or after ; & | or (, optionally behind a path
// (.venv/bin/promote-model).
func startsCommand(hay, needle string) bool {
	for off := 0; ; {
		i := strings.Index(hay[off:], needle)
		if i < 0 {
			return false
		}
		at := off + i
		off = at + 1
		end := at + len(needle)
		if end != len(hay) && strings.IndexByte(" \t\"';&|)", hay[end]) < 0 {
			continue
		}
		tok := strings.LastIndexAny(hay[:at], " \t;&|(") + 1
		if pre := hay[tok:at]; pre != "" && !strings.HasSuffix(pre, "/") {
			continue
		}
		before := strings.TrimRight(hay[:tok], " \t")
		if before == "" || strings.IndexByte(";&|(", before[len(before)-1]) >= 0 {
			return true
		}
	}
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

// maxPwshDepth bounds how deep decoded PowerShell commands are re-read.
const maxPwshDepth = 3

// checkModelPromote is the guard function.
func checkModelPromote(call policy.ToolCall, g packs.GuardCtx) (policy.Action, string, bool) {
	return checkModelPromoteAt(call, g, 0)
}

// checkModelPromoteAt also reads what PowerShell runs: a decoded
// -EncodedCommand is checked like the command itself, and a script the
// guard cannot read (-File, a pwsh bare script, stdin, an encoded command
// that does not decode, a nest past maxPwshDepth or a command past the word
// cap) is asked about (fail closed).
func checkModelPromoteAt(call policy.ToolCall, g packs.GuardCtx, depth int) (policy.Action, string, bool) {
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
		st := load(g.Settings(), g.Manifest())
		anywhere, atStart := commandMarkers(st.Promotion.Command)
		for _, mk := range anywhere {
			if containsWord(cs, mk) {
				why = "the declared promotion command run directly"
				break
			}
		}
		for _, mk := range atStart {
			if why == "" && startsCommand(cs, mk) {
				why = "the declared promotion command run directly"
			}
		}
	}
	if why == "" && (has("pwsh") || has("powershell")) {
		runs, truncated := scan.PowerShellRuns(call.Command)
		if truncated || depth >= maxPwshDepth {
			return policy.Ask, "tq/model-promote: a PowerShell command too long or too deeply nested to read; confirm it promotes no model. " + promoteDeny, true
		}
		for _, r := range runs {
			if r.Opaque != "" {
				return policy.Ask, "tq/model-promote: PowerShell runs " + r.Opaque + " that the guard cannot read; confirm it promotes no model. " + promoteDeny, true
			}
			if r.Encoded {
				if a, reason, handled := checkModelPromoteAt(policy.ToolCall{Tool: call.Tool, Command: r.Script}, g, depth+1); handled {
					return a, reason, handled
				}
			}
		}
	}
	if why == "" && depth < maxPwshDepth {
		// A shell script file the command runs (bash x.sh, ./x.sh, source
		// x.sh): read it through ReadLocal (at most 64 KiB) and ask when its
		// content is a promotion. A script that cannot be read stays allowed:
		// the command line itself named no promotion.
		for _, sp := range scan.ScriptFiles(call.Command) {
			abs := sp
			if !filepath.IsAbs(abs) {
				if g.Cwd() == "" {
					continue
				}
				abs = filepath.Join(g.Cwd(), filepath.FromSlash(sp))
			}
			b, err := g.ReadLocal(abs, maxScriptRead)
			if err != nil {
				continue
			}
			if a, _, handled := checkModelPromoteAt(policy.ToolCall{Tool: call.Tool, Command: string(b)}, g, depth+1); handled && a != policy.Allow {
				return policy.Ask, "tq/model-promote: the script " + sp + " promotes a model, and the guard does not run scripts; confirm it. " + promoteDeny, true
			}
		}
	}
	if why == "" {
		return policy.Allow, "", false
	}
	return policy.Deny, "tq/model-promote: " + why + ". " + promoteDeny, true
}

// maxScriptRead bounds the read of a shell script a command runs.
const maxScriptRead = 64 << 10
