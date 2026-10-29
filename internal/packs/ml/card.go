package ml

// ml/model-card (spec C §4.5): every declared model has a MODEL_CARD.md with
// the required headings filled in, a non-empty metrics table, a training
// snapshot whose sha256 matches the data manifest, no text the redaction
// detectors flag, and a card that changes when the model's code changes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/redact"
	"gopkg.in/yaml.v3"
)

// CardHeadings are the required "##" headings, matched by prefix after
// lower-casing ("Intended use / Out of scope" matches "intended use").
var CardHeadings = []struct{ Key, Title string }{
	{"model details", "Model details"},
	{"intended use", "Intended use / Out of scope"},
	{"training data", "Training data"},
	{"evaluation data", "Evaluation data"},
	{"metrics", "Metrics"},
	{"fairness and slices", "Fairness and slices"},
	{"limitations and risks", "Limitations and risks"},
	{"reproducibility", "Reproducibility"},
	{"monitoring and drift", "Monitoring and drift"},
	{"promotion history", "Promotion history"},
}

const cardMaxBytes = 1 << 20

type cardSection struct {
	Title string
	Key   string
	Line  int // the heading's line
	Body  []cardLine
}

type cardLine struct {
	N    int
	Text string
}

var (
	commentRe   = regexp.MustCompile(`(?s)<!--.*?-->`)
	todoRe      = regexp.MustCompile(`\bTODO\b`)
	tableSepRe  = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?\s*$`)
	sha256Re    = regexp.MustCompile(`\b[0-9a-fA-F]{64}\b`)
	labelOnlyRe = regexp.MustCompile(`^[-*+]?\s*\*\*[^*]+\*\*\s*$`)
)

// parseCard splits a card into its "##" sections. HTML comments are blanked
// (line numbers kept).
func parseCard(src string) []cardSection {
	src = commentRe.ReplaceAllStringFunc(src, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	})
	var out []cardSection
	inFence := false
	for i, l := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "```") {
			inFence = !inFence
		}
		if !inFence && strings.HasPrefix(t, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(t, "## "))
			out = append(out, cardSection{Title: title, Key: strings.ToLower(title), Line: i + 1})
			continue
		}
		if len(out) > 0 {
			out[len(out)-1].Body = append(out[len(out)-1].Body, cardLine{i + 1, l})
		}
	}
	return out
}

func findSection(ss []cardSection, key string) *cardSection {
	for i := range ss {
		if strings.HasPrefix(ss[i].Key, key) {
			return &ss[i]
		}
	}
	return nil
}

// contentLines are the lines that carry a value: not blank, not a table
// header or separator, not a bare "**Label:**".
func contentLines(s cardSection) []cardLine {
	var out []cardLine
	for i, l := range s.Body {
		t := strings.TrimSpace(l.Text)
		if t == "" || tableSepRe.MatchString(t) || labelOnlyRe.MatchString(t) {
			continue
		}
		if strings.HasPrefix(t, "|") && i+1 < len(s.Body) && tableSepRe.MatchString(strings.TrimSpace(s.Body[i+1].Text)) {
			continue // table header
		}
		out = append(out, l)
	}
	return out
}

// tableRows are the data rows of the first table of a section.
func tableRows(s cardSection) [][]string {
	var rows [][]string
	inTable, header := false, false
	for i, l := range s.Body {
		t := strings.TrimSpace(l.Text)
		if !strings.HasPrefix(t, "|") {
			if inTable {
				break
			}
			continue
		}
		if !inTable {
			inTable = true
			header = i+1 < len(s.Body) && tableSepRe.MatchString(strings.TrimSpace(s.Body[i+1].Text))
			if header {
				continue
			}
		}
		if tableSepRe.MatchString(t) {
			continue
		}
		var cells []string
		for _, c := range strings.Split(strings.Trim(t, "|"), "|") {
			cells = append(cells, strings.TrimSpace(c))
		}
		rows = append(rows, cells)
	}
	return rows
}

func fieldValue(line string) string {
	t := strings.TrimSpace(line)
	t = strings.TrimLeft(t, "-*+ ")
	if i := strings.Index(t, ":"); i >= 0 {
		t = t[i+1:]
	}
	t = strings.Trim(strings.TrimSpace(t), "*`_ ")
	if i := strings.Index(t, " ("); i >= 0 {
		t = t[:i]
	}
	return strings.Trim(strings.TrimSpace(t), "*`_ ")
}

// cardSnapshot reads the training snapshot name and sha256 from the card.
func cardSnapshot(s *cardSection) (name, sha string, line int) {
	if s == nil {
		return "", "", 0
	}
	for _, l := range s.Body {
		low := strings.ToLower(l.Text)
		switch {
		case strings.Contains(low, "sha256"):
			if m := sha256Re.FindString(l.Text); m != "" && sha == "" {
				sha, line = strings.ToLower(m), l.N
			}
		case strings.Contains(low, "snapshot") && name == "":
			if v := fieldValue(l.Text); v != "" && !todoRe.MatchString(v) {
				name = v
				if line == 0 {
					line = l.N
				}
			}
		}
	}
	return name, sha, line
}

// manifestSnapshots reads a data manifest: snapshots: {name: {sha256}} or
// snapshots: [{name, sha256}], or the same at the top level.
func manifestSnapshots(raw []byte) (map[string]string, error) {
	n, err := scan.ParseYAML(raw)
	if err != nil {
		return nil, err
	}
	if s := scan.Get(n, "snapshots"); s != nil {
		n = s
	}
	out := map[string]string{}
	add := func(name string, entry *yaml.Node) {
		if sha := scan.Scalar(scan.Get(entry, "sha256")); sha != "" {
			out[name] = strings.ToLower(strings.TrimPrefix(sha, "sha256:"))
		}
	}
	if items := scan.Items(n); len(items) > 0 {
		for _, it := range items {
			add(scan.Scalar(scan.Get(it, "name")), it)
		}
		return out, nil
	}
	for _, kv := range scan.Entries(n) {
		add(kv.Key, kv.Value)
	}
	return out, nil
}

// classesFor are the redaction classes of the workspace: its data classes
// plus secrets, or every detector when it is not classified.
func classesFor(in *packs.Input) []string {
	if in.Manifest == nil || !in.Manifest.Data.Enabled() {
		return []string{"all"}
	}
	return append(append([]string(nil), in.Manifest.Data.Classes...), "secrets")
}

// cardFindings checks one card's content (everything but the diff rule).
func cardFindings(in *packs.Input, st Settings, m Model) []packs.Finding {
	raw, err := in.ReadNew(m.Card, cardMaxBytes)
	if err != nil {
		msg := "the model card is missing: write it from plugin/templates/process/MODEL_CARD.md (skill model-card)"
		if !errors.Is(err, os.ErrNotExist) {
			msg = "the model card cannot be read: " + err.Error()
		}
		return []packs.Finding{{File: m.Card, Level: packs.LevelError, Rule: "card-missing", Message: msg}}
	}
	src := string(raw)
	ss := parseCard(src)
	var fs []packs.Finding
	add := func(line int, level, rule, msg string) {
		if line == 0 {
			line = 1
		}
		fs = append(fs, packs.Finding{File: m.Card, Line: line, Level: level, Rule: rule, Message: msg})
	}
	for _, h := range CardHeadings {
		s := findSection(ss, h.Key)
		if s == nil {
			add(1, packs.LevelError, "card-heading-missing", fmt.Sprintf("required heading \"## %s\" is missing", h.Title))
			continue
		}
		if h.Key == "promotion history" {
			continue // empty until the first promotion
		}
		lines := contentLines(*s)
		todo := 0
		for _, l := range lines {
			if todoRe.MatchString(l.Text) {
				todo++
			}
		}
		switch {
		case len(lines) == 0 && h.Key == "metrics" && hasTable(*s):
			// an empty metrics table is reported as card-metrics-empty below
		case len(lines) == 0:
			add(s.Line, packs.LevelError, "card-section-todo", fmt.Sprintf("section %q is empty", h.Title))
		case todo == len(lines):
			add(s.Line, packs.LevelError, "card-section-todo", fmt.Sprintf("section %q holds only TODO", h.Title))
		case todo > 0:
			add(s.Line, packs.LevelWarn, "card-section-todo", fmt.Sprintf("section %q still has %d TODO line(s)", h.Title, todo))
		}
	}
	if s := findSection(ss, "metrics"); s != nil && (hasTable(*s) || len(contentLines(*s)) > 0) && !(len(contentLines(*s)) > 0 && onlyTODO(*s)) {
		filled := 0
		for _, row := range tableRows(*s) {
			for _, c := range row {
				if c != "" && !todoRe.MatchString(c) {
					filled++
					break
				}
			}
		}
		if filled == 0 {
			add(s.Line, packs.LevelError, "card-metrics-empty", "the metrics table has no row: add metric, value, confidence interval and baseline")
		}
	}
	if st.DataManifest != "" && !strings.HasSuffix(st.DataManifest, "dvc.lock") {
		td := findSection(ss, "training data")
		name, sha, line := cardSnapshot(td)
		if td != nil && line == 0 {
			line = td.Line
		}
		mraw, err := in.ReadNew(st.DataManifest, cardMaxBytes)
		var snaps map[string]string
		if err == nil {
			snaps, err = manifestSnapshots(mraw)
		}
		switch {
		case td == nil:
		case err != nil:
			add(line, packs.LevelError, "card-snapshot-mismatch", "cannot read the data manifest "+st.DataManifest+" to compare the snapshot")
		case name == "" || sha == "":
			if len(contentLines(*td)) > 0 && !onlyTODO(*td) {
				add(line, packs.LevelError, "card-snapshot-missing", "Training data must name the snapshot and its sha256 from "+st.DataManifest)
			}
		case snaps[name] == "":
			add(line, packs.LevelError, "card-snapshot-mismatch", fmt.Sprintf("snapshot %q is not in %s", name, st.DataManifest))
		case snaps[name] != sha:
			add(line, packs.LevelError, "card-snapshot-mismatch", fmt.Sprintf("the sha256 of snapshot %q differs from %s", name, st.DataManifest))
		}
	}
	classes := classesFor(in)
	for i, l := range strings.Split(src, "\n") {
		hits := redact.CountAll(l, classes)
		if len(hits) == 0 {
			continue
		}
		names := make([]string, 0, len(hits))
		for k := range hits {
			names = append(names, k)
		}
		sort.Strings(names)
		add(i+1, packs.LevelError, "card-pii", "the card text matches the "+strings.Join(names, ", ")+" detector: remove the value (no personal data or secrets in a card)")
	}
	return fs
}

func hasTable(s cardSection) bool {
	for _, l := range s.Body {
		if strings.HasPrefix(strings.TrimSpace(l.Text), "|") {
			return true
		}
	}
	return false
}

func onlyTODO(s cardSection) bool {
	ls := contentLines(s)
	for _, l := range ls {
		if !todoRe.MatchString(l.Text) {
			return false
		}
	}
	return true
}

// modelTouched reports whether the diff touches the model's card, code,
// metrics or the data manifest; codeOnly is the code part alone.
func modelTouched(in *packs.Input, st Settings, m Model) (any, code, card bool) {
	globs := st.CodePaths(m)
	code = len(globs) > 0 && in.AnyChanged(globs...)
	card = in.AnyChanged(m.Card)
	other := (m.Metrics != "" && in.AnyChanged(m.Metrics)) || (st.DataManifest != "" && in.AnyChanged(st.DataManifest))
	return code || card || other, code, card
}

func modelCardApplies(in *packs.Input) bool {
	st := loadIn(in)
	if len(st.Models) == 0 {
		return false
	}
	if in.All {
		return true
	}
	for _, m := range st.Models {
		if a, _, _ := modelTouched(in, st, m); a {
			return true
		}
	}
	return false
}

func runModelCard(ctx context.Context, in *packs.Input) packs.Result {
	st := loadIn(in)
	var fs []packs.Finding
	n := 0
	for _, m := range st.Models {
		touched, code, card := modelTouched(in, st, m)
		if !in.All && !touched {
			continue
		}
		n++
		fs = append(fs, cardFindings(in, st, m)...)
		if !in.All && in.Base != "" && code && !card {
			fs = append(fs, packs.Finding{File: m.Card, Line: 1, Level: packs.LevelError, Rule: "card-not-updated",
				Message: fmt.Sprintf("the code of model %s changed and its card did not: update the card (skill model-card)", m.Name)})
		}
	}
	return packs.Result{Status: packs.OK, Detail: plural(n, "model card"), Findings: fs}
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// appendPromotionRow adds one row to the card's Promotion history table.
func appendPromotionRow(card string, cells []string) (string, error) {
	lines := strings.Split(card, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "## promotion history") {
			start = i
			break
		}
	}
	if start < 0 {
		return "", errors.New("the card has no \"## Promotion history\" section")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	last := -1
	for i := start + 1; i < end; i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			last = i
		}
	}
	row := "| " + strings.Join(cells, " | ") + " |"
	var out []string
	if last < 0 {
		out = append(out, lines[:start+1]...)
		out = append(out, "", "| Date | Version | Target | Decision | Actor |", "|---|---|---|---|---|", row)
		out = append(out, lines[start+1:]...)
	} else {
		out = append(out, lines[:last+1]...)
		out = append(out, row)
		out = append(out, lines[last+1:]...)
	}
	return strings.Join(out, "\n"), nil
}
