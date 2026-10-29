package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// --- checkov -o json -------------------------------------------------------

type checkovRecord struct {
	CheckID       string `json:"check_id"`
	CheckName     string `json:"check_name"`
	FilePath      string `json:"file_path"`
	FileAbsPath   string `json:"file_abs_path"`
	FileLineRange []int  `json:"file_line_range"`
	Resource      string `json:"resource"`
	Severity      any    `json:"severity"`
}

type checkovReport struct {
	CheckType string `json:"check_type"`
	Results   struct {
		FailedChecks []checkovRecord `json:"failed_checks"`
	} `json:"results"`
}

// checkovRel: checkov's file_path is relative to the -d directory and
// written with a leading slash ("/main.tf"). On Linux that looks absolute
// and would escape the repository, so the slash is dropped; a Windows drive
// path or a file: URI is left to repoPath.
func checkovRel(p string) string {
	s := strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if strings.HasPrefix(s, "file:") || (len(s) > 1 && s[1] == ':') {
		return p
	}
	return strings.TrimLeft(s, "/")
}

// ParseCheckov parses `checkov -o json`: one report object, or a list of
// them (one per framework); a summary-only object (nothing scanned) has no
// findings. Every failed check is an error: Checkov's open-source output has
// no severity (spec D §7.3). root is where the tool ran and base the
// directory it scanned; display, when set, replaces the file (a plan).
func ParseCheckov(stdout []byte, root, base, display string) ([]packs.Finding, error) {
	t := bytes.TrimSpace(stdout)
	var reports []checkovReport
	switch {
	case len(t) == 0:
		return nil, fmt.Errorf("checkov printed nothing")
	case t[0] == '[':
		if err := json.Unmarshal(t, &reports); err != nil {
			return nil, fmt.Errorf("checkov output is not JSON: %w", err)
		}
	case t[0] == '{':
		var r checkovReport
		if err := json.Unmarshal(t, &r); err != nil {
			return nil, fmt.Errorf("checkov output is not JSON: %w", err)
		}
		reports = []checkovReport{r}
	default:
		return nil, fmt.Errorf("checkov output is not JSON")
	}
	out := []packs.Finding{}
	for _, rep := range reports {
		for _, c := range rep.Results.FailedChecks {
			f := packs.Finding{Level: packs.LevelError, Rule: "checkov:" + c.CheckID}
			switch {
			case display != "":
				f.File = display
			case c.FileAbsPath != "":
				f.File = repoPath(root, base, c.FileAbsPath)
			default:
				f.File = repoPath(root, base, checkovRel(c.FilePath))
			}
			if display == "" && len(c.FileLineRange) > 0 && c.FileLineRange[0] > 0 {
				f.Line = c.FileLineRange[0]
			}
			msg := c.CheckName
			if c.Resource != "" {
				msg = c.Resource + ": " + msg
			}
			f.Message = clean(msg)
			out = append(out, f)
		}
	}
	return out, nil
}

// --- trivy config --format json ----------------------------------------------

type trivyReport struct {
	Results []struct {
		Target            string `json:"Target"`
		Misconfigurations []struct {
			ID            string `json:"ID"`
			AVDID         string `json:"AVDID"`
			Title         string `json:"Title"`
			Message       string `json:"Message"`
			Severity      string `json:"Severity"`
			Status        string `json:"Status"`
			CauseMetadata struct {
				Resource  string `json:"Resource"`
				StartLine int    `json:"StartLine"`
			} `json:"CauseMetadata"`
		} `json:"Misconfigurations"`
	} `json:"Results"`
}

// ParseTrivy parses `trivy config --format json`. LOW and MEDIUM are warn,
// HIGH and CRITICAL fail; an unknown severity fails (fail closed). Passed
// checks (Status PASS, with --include-non-failures) are skipped.
func ParseTrivy(stdout []byte, root, base string) ([]packs.Finding, error) {
	var rep trivyReport
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &rep); err != nil {
		return nil, fmt.Errorf("trivy output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, r := range rep.Results {
		for _, m := range r.Misconfigurations {
			if strings.EqualFold(m.Status, "PASS") || strings.EqualFold(m.Status, "EXCEPTION") {
				continue
			}
			level := packs.LevelError
			switch strings.ToUpper(m.Severity) {
			case "LOW", "MEDIUM":
				level = packs.LevelWarn
			}
			id := m.ID
			if id == "" {
				id = m.AVDID
			}
			msg := m.Title
			if m.Message != "" {
				msg = m.Message
			}
			if m.CauseMetadata.Resource != "" {
				msg = m.CauseMetadata.Resource + ": " + msg
			}
			out = append(out, packs.Finding{File: repoPath(root, base, r.Target), Line: m.CauseMetadata.StartLine,
				Level: level, Rule: "trivy:" + id, Message: clean(msg)})
		}
	}
	return out, nil
}

// --- conftest test --output json ---------------------------------------------

type conftestResult struct {
	Msg      string         `json:"msg"`
	Metadata map[string]any `json:"metadata"`
}

type conftestFile struct {
	Filename  string           `json:"filename"`
	Namespace string           `json:"namespace"`
	Failures  []conftestResult `json:"failures"`
	Warnings  []conftestResult `json:"warnings"`
}

// Locator maps a Conftest input file and the addr our Rego emits back to a
// file and line.
type Locator func(filename, addr string) (file string, line int)

// ParseConftest parses `conftest test --output json`: failures are errors,
// warnings are warn. The rule is metadata.rule (our bundle), else the
// namespace (a project policy).
func ParseConftest(stdout []byte, loc Locator) ([]packs.Finding, error) {
	var files []conftestFile
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &files); err != nil {
		return nil, fmt.Errorf("conftest output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	conv := func(f conftestFile, r conftestResult, level string) packs.Finding {
		addr, _ := r.Metadata["addr"].(string)
		rule, _ := r.Metadata["rule"].(string)
		if rule == "" {
			rule = f.Namespace
		}
		file, line := loc(f.Filename, addr)
		return packs.Finding{File: file, Line: line, Level: level, Rule: "conftest:" + rule, Message: clean(r.Msg)}
	}
	for _, f := range files {
		for _, r := range f.Failures {
			out = append(out, conv(f, r, packs.LevelError))
		}
		for _, r := range f.Warnings {
			out = append(out, conv(f, r, packs.LevelWarn))
		}
	}
	return out, nil
}

// --- tflint --format json ----------------------------------------------------

type tflintPos struct {
	Line int `json:"line"`
}

type tflintRange struct {
	Filename string    `json:"filename"`
	Start    tflintPos `json:"start"`
}

type tflintOutput struct {
	Issues []struct {
		Rule struct {
			Name     string `json:"name"`
			Severity string `json:"severity"`
		} `json:"rule"`
		Message string      `json:"message"`
		Range   tflintRange `json:"range"`
	} `json:"issues"`
	Errors []struct {
		Summary  string       `json:"summary"`
		Message  string       `json:"message"`
		Severity string       `json:"severity"`
		Range    *tflintRange `json:"range"`
	} `json:"errors"`
}

// ParseTflint parses `tflint --format json`. Issues of severity error fail,
// warning and notice warn. Entries under "errors" (tflint itself failed:
// a parse error, a missing plugin) are returned as the second value.
func ParseTflint(stdout []byte, root, base string) ([]packs.Finding, []string, error) {
	var o tflintOutput
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &o); err != nil {
		return nil, nil, fmt.Errorf("tflint output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, is := range o.Issues {
		level := packs.LevelWarn
		if strings.EqualFold(is.Rule.Severity, "error") {
			level = packs.LevelError
		}
		out = append(out, packs.Finding{File: repoPath(root, base, is.Range.Filename), Line: is.Range.Start.Line,
			Level: level, Rule: "tflint:" + is.Rule.Name, Message: clean(is.Message)})
	}
	var errs []string
	for _, e := range o.Errors {
		m := e.Message
		if e.Summary != "" {
			m = e.Summary + ": " + m
		}
		errs = append(errs, clean(m))
	}
	return out, errs, nil
}

// --- SARIF (bicep lint --diagnostics-format sarif) ---------------------------

type sarifLog struct {
	Runs []struct {
		Results []struct {
			RuleID  string `json:"ruleId"`
			Level   string `json:"level"`
			Message struct {
				Text string `json:"text"`
			} `json:"message"`
			Locations []struct {
				PhysicalLocation struct {
					ArtifactLocation struct {
						URI string `json:"uri"`
					} `json:"artifactLocation"`
					Region struct {
						StartLine int `json:"startLine"`
					} `json:"region"`
				} `json:"physicalLocation"`
			} `json:"locations"`
		} `json:"results"`
	} `json:"runs"`
}

// ParseSARIF parses a SARIF 2.1 log. Level error fails; warning, note and
// none warn. A result with no level is a warning (the SARIF default).
// fallback is the file a result without a location is put on.
func ParseSARIF(stdout []byte, prefix, root, fallback string) ([]packs.Finding, error) {
	var lg sarifLog
	if err := json.Unmarshal(bytes.TrimSpace(stdout), &lg); err != nil {
		return nil, fmt.Errorf("the SARIF output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, run := range lg.Runs {
		for _, r := range run.Results {
			level := packs.LevelWarn
			if strings.EqualFold(r.Level, "error") {
				level = packs.LevelError
			}
			f := packs.Finding{File: fallback, Level: level, Rule: prefix + ":" + r.RuleID, Message: clean(r.Message.Text)}
			if len(r.Locations) > 0 {
				pl := r.Locations[0].PhysicalLocation
				if pl.ArtifactLocation.URI != "" {
					f.File = repoPath(root, "", pl.ArtifactLocation.URI)
				}
				f.Line = pl.Region.StartLine
			}
			out = append(out, f)
		}
	}
	return out, nil
}

// --- cfn-lint -f json --------------------------------------------------------

type cfnLintMatch struct {
	Rule struct {
		ID string `json:"Id"`
	} `json:"Rule"`
	Location struct {
		Start struct {
			LineNumber int `json:"LineNumber"`
		} `json:"Start"`
	} `json:"Location"`
	Level    string `json:"Level"`
	Message  string `json:"Message"`
	Filename string `json:"Filename"`
}

// ParseCfnLint parses `cfn-lint -f json`. Level Error fails; Warning and
// Informational warn.
// An empty stdout is zero findings only with exit 0: a non-zero exit says
// cfn-lint found something, so silence there is a failed run.
func ParseCfnLint(stdout []byte, root string, exit int) ([]packs.Finding, error) {
	t := bytes.TrimSpace(stdout)
	if len(t) == 0 {
		if exit != 0 {
			return nil, fmt.Errorf("cfn-lint exited %d but printed no JSON", exit)
		}
		return []packs.Finding{}, nil // no matches: cfn-lint may print nothing
	}
	var ms []cfnLintMatch
	if err := json.Unmarshal(t, &ms); err != nil {
		return nil, fmt.Errorf("cfn-lint output is not JSON: %w", err)
	}
	out := []packs.Finding{}
	for _, m := range ms {
		level := packs.LevelWarn
		if strings.EqualFold(m.Level, "error") {
			level = packs.LevelError
		}
		out = append(out, packs.Finding{File: repoPath(root, "", m.Filename), Line: m.Location.Start.LineNumber,
			Level: level, Rule: "cfn-lint:" + m.Rule.ID, Message: clean(m.Message)})
	}
	return out, nil
}
