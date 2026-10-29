package ml

// ml/leakage (spec C §4.2): a heuristic over call order and names, not a
// dataflow analysis. Rules L1–L8; L1–L4 are errors, L5–L8 warnings.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
)

// Rule ids of the leakage lint.
const (
	ruleL1 = "L1-fit-before-split"
	ruleL2 = "L2-test-in-fit"
	ruleL3 = "L3-cv-outside-pipeline"
	ruleL4 = "L4-target-in-features"
	ruleL5 = "L5-lookahead"
	ruleL6 = "L6-timeseries-shuffle"
	ruleL7 = "L7-group-leak"
	ruleL8 = "L8-torch-eval"
)

// suppression keys of a rule: "L2", "L2-test-in-fit", "test-in-fit", "ml/leakage".
func ruleKeys(rule string) []string {
	k := []string{rule, "ml/leakage"}
	if i := strings.Index(rule, "-"); i > 0 {
		k = append(k, rule[:i], rule[i+1:])
	}
	return k
}

// preprocessors are the preprocessing objects of L1 (spec list plus a few
// close relatives).
var preprocessors = set("StandardScaler", "MinMaxScaler", "RobustScaler", "MaxAbsScaler", "OneHotEncoder", "OrdinalEncoder",
	"LabelEncoder", "SimpleImputer", "KNNImputer", "IterativeImputer", "PCA", "TfidfVectorizer", "CountVectorizer", "SelectKBest",
	"SelectPercentile", "TargetEncoder", "QuantileTransformer", "PowerTransformer", "KBinsDiscretizer")

// splitters are the splitter classes; groupSplitters keep groups together.
var splitters = set("KFold", "StratifiedKFold", "GroupKFold", "StratifiedGroupKFold", "TimeSeriesSplit", "ShuffleSplit",
	"StratifiedShuffleSplit", "GroupShuffleSplit", "RepeatedKFold", "RepeatedStratifiedKFold", "LeaveOneGroupOut", "LeavePGroupsOut")

var groupSplitters = set("GroupKFold", "StratifiedGroupKFold", "GroupShuffleSplit", "LeaveOneGroupOut", "LeavePGroupsOut")

var searchers = set("GridSearchCV", "RandomizedSearchCV", "HalvingGridSearchCV", "HalvingRandomSearchCV", "BayesSearchCV")

var cvFuncs = set("cross_val_score", "cross_validate", "cross_val_predict", "learning_curve", "validation_curve", "permutation_test_score")

func set(xs ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func runLeakage(ctx context.Context, in *packs.Input) packs.Result {
	files, fs := selectFiles(ctx, in, true)
	var featGroups map[string]string // id column -> feature file
	ps := pathsOf(in)
	if len(ps.features) > 0 {
		featGroups = featureGroupBys(ctx, in, ps.features)
	}
	for _, s := range files {
		fs = append(fs, leakFile(s, featGroups)...)
	}
	return packs.Result{Status: packs.OK, Detail: "leakage lint over " + plural(len(files), "file"), Findings: fs}
}

// leakFile runs L1–L8 over one file.
func leakFile(s selected, featGroups map[string]string) []packs.Finding {
	f := s.f
	var out []packs.Finding
	add := func(i int, level, rule, msg string) {
		if !f.suppressed(i, ruleKeys(rule)...) {
			out = append(out, f.find(i, level, rule, msg))
		}
	}
	firstSplit := f.firstSplit()
	pre := map[string]bool{}     // preprocessing object variables
	search := map[string]bool{}  // search CV variables
	fullT := map[string]bool{}   // arrays transformed with a fit on the full data
	fitFull := map[string]bool{} // transformers fitted on the full data
	for i, l := range f.Lines {
		lhs, rhs, _, isAssign := splitAssign(l.Code)
		calls := f.Calls[i]
		for _, c := range calls {
			if c.Name != "fit" && c.Name != "fit_transform" && c.Name != "partial_fit" {
				continue
			}
			x, hasX := c.argN(0, "X")
			// L1: a preprocessing fit before the first split, not on train data.
			if c.Name != "partial_fit" && firstSplit >= 0 && i < firstSplit && hasX && !trainish(x.Text) && f.isPreprocessor(i, c, pre) {
				add(i, packs.LevelError, ruleL1, fmt.Sprintf("%s fits a preprocessing step on %q before the first train/test split (%s); fit it on the training split or inside a Pipeline", c.Name, x.Text, f.at(firstSplit)))
			}
			// L2: a held-out set passed to fit.
			if name, bad := heldOutArg(c); bad {
				add(i, packs.LevelError, ruleL2, fmt.Sprintf("%s is called on %q, which looks like test, holdout or validation data (pass a validation set as eval_set= for early stopping)", c.Name, name))
			}
			// L3 bookkeeping and the search CV fit.
			if c.Name == "fit" && search[c.Receiver] && hasX && fullT[firstIdent(x.Text)] {
				add(i, packs.LevelError, ruleL3, fmt.Sprintf("%s.fit runs cross-validation on %q, transformed with a fit on the full data; put the transformer in a Pipeline", c.Receiver, firstIdent(x.Text)))
			}
			if c.Name == "fit" && hasX && !trainish(x.Text) && c.Receiver != "" && !search[c.Receiver] {
				fitFull[c.Receiver] = true
			}
		}
		for _, c := range calls {
			if cvFuncs[c.Name] {
				if x, ok := c.argN(1, "X"); ok && fullT[firstIdent(x.Text)] {
					add(i, packs.LevelError, ruleL3, fmt.Sprintf("%s runs on %q, transformed with a fit on the full data; cross-validate a Pipeline that holds the transformer", c.Name, firstIdent(x.Text)))
				}
			}
		}
		if isAssign && len(lhs) == 1 {
			v := lhs[0]
			delete(fullT, v)
			delete(pre, v)
			delete(search, v)
			r := strings.TrimSpace(rhs)
			if top, ok := topCall(calls, l.Code, r); ok {
				switch {
				case preprocessors[top.Name]:
					pre[v] = true
				case searchers[top.Name]:
					search[v] = true
				}
			}
			for _, c := range calls {
				if c.Name == "fit_transform" {
					if x, ok := c.argN(0, "X"); ok && !trainish(x.Text) {
						fullT[v] = true
					}
				}
				if c.Name == "transform" && fitFull[c.Receiver] {
					fullT[v] = true
				}
			}
		}
	}
	out = append(out, f.l4(add)...)
	if s.feature {
		f.l5(add)
	}
	f.l6(add)
	f.l7(add, featGroups)
	f.l8(add)
	return out
}

// at names the position of line index i: "line N", or "cell N, line M".
func (f *pyFile) at(i int) string {
	if f.cellLine != nil {
		c, l := f.cellLine(f.Lines[i].Line)
		return fmt.Sprintf("cell %d, line %d", c, l)
	}
	return fmt.Sprintf("line %d", f.Lines[i].Line)
}

// topCall returns the call that the right side of an assignment starts with.
func topCall(calls []pcall, code, rhs string) (pcall, bool) {
	off := strings.Index(code, rhs)
	for _, c := range calls {
		if c.From == off || (off >= 0 && strings.HasPrefix(rhs, c.Func+"(")) {
			return c, true
		}
	}
	return pcall{}, false
}

func firstIdent(text string) string {
	if ids := idents(text); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

// isPreprocessor reports whether a fit call is on a preprocessing object: a
// variable built from one, or a constructor chained on the same line.
func (f *pyFile) isPreprocessor(i int, c pcall, pre map[string]bool) bool {
	if pre[c.Receiver] {
		return true
	}
	if strings.HasPrefix(c.Func, ".") && c.From >= 2 {
		for _, o := range f.Calls[i] {
			if o.Close == c.From-2 && preprocessors[o.Name] {
				return true
			}
		}
	}
	return false
}

// heldOutArg reports a held-out name among a fit's data arguments. Keyword
// arguments whose own name says eval, val or test (eval_set=,
// validation_data=) are the allowed early-stopping inputs.
func heldOutArg(c pcall) (string, bool) {
	for _, a := range c.Args {
		if a.Key != "" && (hasSeg(a.Key, "eval", "val", "valid", "validation", "test", "early", "callbacks") || a.Key == "callbacks") {
			continue
		}
		for _, id := range idents(a.Text) {
			if heldOut(id) {
				return id, true
			}
		}
	}
	return "", false
}

// firstSplit returns the line index of the first split call, or -1.
func (f *pyFile) firstSplit() int {
	splitVars := map[string]bool{}
	for i, l := range f.Lines {
		lhs, _, _, ok := splitAssign(l.Code)
		for _, c := range f.Calls[i] {
			switch {
			case c.Name == "train_test_split" || c.Name == "random_split":
				return i
			case splitters[c.Name]:
				if ok {
					for _, v := range lhs {
						splitVars[v] = true
					}
				}
				return i
			case c.Name == "split" && (splitVars[c.Receiver] || splitLike(c)):
				return i
			}
		}
	}
	return -1
}

// splitLike tells a CV `.split(X, …)` from a string split: the first
// argument is a name, not a literal, and the receiver is not a string
// helper.
func splitLike(c pcall) bool {
	r := strings.ToLower(c.Receiver)
	if r == "re" || strings.HasSuffix(r, "path") || strings.HasSuffix(r, ".str") || r == "str" || r == "shlex" {
		return false
	}
	p := c.pos()
	return len(p) > 0 && identRe.MatchString(firstIdent(p[0].Text)) && !isStrLit(p[0].Text)
}

// --- L4 -----------------------------------------------------------------------

var (
	subscriptRe = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?:\.loc\s*\[\s*:\s*,\s*|\[\s*)([rRbBfFuU]{0,2}(?:""|'')|[A-Za-z_]\w*)\s*\]$`)
	listSubRe   = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?:\.loc\s*\[\s*:\s*,\s*|\[\s*)\[(.*)\]\s*\]$`)
	nameSubRe   = regexp.MustCompile(`^([A-Za-z_]\w*)\s*(?:\.loc\s*\[\s*:\s*,\s*|\[\s*)([A-Za-z_]\w*)\s*\]$`)
	frameRe     = regexp.MustCompile(`^([A-Za-z_]\w*)(?:\.copy\(\s*\))?$`)
)

func targetName(v string) bool { return hasSeg(v, "y", "target", "label", "labels") }

func featureName(v string) bool {
	return v == "X" || v == "x" || hasSeg(v, "x", "features", "feature", "feats")
}

type target struct{ frame, col, constName string }

// l4 finds a target column that stays in the feature frame.
func (f *pyFile) l4(add func(int, string, string, string)) []packs.Finding {
	var targets []target
	for i, l := range f.Lines {
		lhs, rhs, from, ok := splitAssign(l.Code)
		if !ok || len(lhs) != 1 || !targetName(lhs[0]) {
			continue
		}
		m := subscriptRe.FindStringSubmatch(strings.TrimSpace(rhs))
		if m == nil {
			continue
		}
		t := target{frame: m[1]}
		if isStrLit(m[2]) {
			if v := stringsIn(l, from, len(l.Code)); len(v) == 1 {
				t.col = v[0]
			}
		} else if v, ok := f.consts[m[2]]; ok {
			t.col, t.constName = v, m[2]
		}
		if t.col != "" {
			targets = append(targets, t)
		}
		_ = i
	}
	if len(targets) == 0 {
		return nil
	}
	for i, l := range f.Lines {
		lhs, rhs, from, ok := splitAssign(l.Code)
		if !ok || len(lhs) != 1 || !featureName(lhs[0]) {
			continue
		}
		r := strings.TrimSpace(rhs)
		for _, t := range targets {
			// X = df (or df.copy()) with no drop of the target.
			if m := frameRe.FindStringSubmatch(r); m != nil && m[1] == t.frame && !f.dropsTarget(t, lhs[0]) {
				add(i, packs.LevelError, ruleL4, fmt.Sprintf("%s takes every column of %s, including the target %q; drop it from the features", lhs[0], t.frame, t.col))
				continue
			}
			// X = df[[…, "t", …]] or X = df[cols] with cols holding t.
			var cols []string
			if m := listSubRe.FindStringSubmatch(r); m != nil && m[1] == t.frame {
				cols = stringsIn(l, from, len(l.Code))
				for _, id := range idents(m[2]) {
					if v, ok := f.consts[id]; ok {
						cols = append(cols, v)
					}
				}
			} else if m := nameSubRe.FindStringSubmatch(r); m != nil && m[1] == t.frame {
				cols = f.lists[m[2]]
			}
			for _, c := range cols {
				if c == t.col {
					add(i, packs.LevelError, ruleL4, fmt.Sprintf("%s selects the target column %q as a feature", lhs[0], t.col))
					break
				}
			}
		}
	}
	return nil
}

// dropsTarget reports whether the file drops, pops or deletes the target
// column from the frame or the feature variable anywhere.
func (f *pyFile) dropsTarget(t target, x string) bool {
	for _, l := range f.Lines {
		c := l.Code
		if !strings.Contains(c, "drop") && !strings.Contains(c, "pop") && !strings.HasPrefix(c, "del ") {
			continue
		}
		ids := idents(c)
		if !containsStr(ids, t.frame) && !containsStr(ids, x) {
			continue
		}
		if containsStr(l.Strings, t.col) || (t.constName != "" && containsStr(ids, t.constName)) {
			return true
		}
	}
	return false
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// --- L5 -----------------------------------------------------------------------

var negIntRe = regexp.MustCompile(`^-\s*\d+$`)

func (f *pyFile) l5(add func(int, string, string, string)) {
	for i, l := range f.Lines {
		if f.onTarget(l.Code, i) {
			continue
		}
		for _, c := range f.Calls[i] {
			switch c.Name {
			case "shift":
				if a, ok := c.argN(0, "periods"); ok && negIntRe.MatchString(a.Text) {
					add(i, packs.LevelWarn, ruleL5, "shift("+strings.ReplaceAll(a.Text, " ", "")+") reads future rows into a feature")
				}
			case "rolling":
				if a, ok := c.kw("center"); ok && a.Text == "True" {
					add(i, packs.LevelWarn, ruleL5, "rolling(…, center=True) averages over future rows")
				}
			case "bfill", "backfill":
				add(i, packs.LevelWarn, ruleL5, c.Name+"() fills a feature from later rows")
			case "fillna":
				if a, ok := c.kw("method"); ok {
					if v, ok := litValue(l, a); ok && (v == "bfill" || v == "backfill") {
						add(i, packs.LevelWarn, ruleL5, "fillna(method="+v+") fills a feature from later rows")
					}
				}
			}
		}
	}
}

// onTarget reports whether the assignment on a line writes the target.
func (f *pyFile) onTarget(code string, i int) bool {
	left := lhsText(code)
	if left == "" {
		return false
	}
	for _, id := range idents(left) {
		if targetName(id) {
			return true
		}
	}
	_, _, from, _ := splitAssign(code)
	for _, s := range stringsIn(f.Lines[i], 0, from) {
		if targetName(s) {
			return true
		}
	}
	return false
}

// --- L6 -----------------------------------------------------------------------

func timeName(s string) bool {
	segs := segments(s)
	for _, g := range segs {
		switch g {
		case "date", "datetime", "time", "timestamp", "ts", "dt", "day", "week", "month", "year", "period":
			return true
		}
	}
	return len(segs) > 1 && segs[len(segs)-1] == "at"
}

// timeOrdered reports whether the file sorts or indexes on a datetime
// column, or uses TimeSeriesSplit.
func (f *pyFile) timeOrdered() bool {
	dated := false
	for i, l := range f.Lines {
		for _, c := range f.Calls[i] {
			switch c.Name {
			case "TimeSeriesSplit", "to_datetime", "DatetimeIndex":
				if c.Name == "TimeSeriesSplit" {
					return true
				}
				dated = true
			case "sort_values", "set_index":
				for _, s := range stringsIn(l, c.Open, c.Close) {
					if timeName(s) {
						return true
					}
				}
				for _, a := range c.pos() {
					if timeName(a.Text) {
						return true
					}
				}
			}
			if a, ok := c.kw("index_col"); ok {
				if v, ok := litValue(l, a); ok && timeName(v) {
					return true
				}
			}
			if _, ok := c.kw("parse_dates"); ok {
				dated = true
			}
		}
	}
	if dated {
		for i := range f.Lines {
			for _, c := range f.Calls[i] {
				if c.Name == "sort_index" {
					return true
				}
			}
		}
	}
	return false
}

func (f *pyFile) l6(add func(int, string, string, string)) {
	var at []int
	for i := range f.Lines {
		for _, c := range f.Calls[i] {
			if c.Name == "train_test_split" {
				if a, ok := c.kw("shuffle"); !ok || a.Text != "False" {
					at = append(at, i)
				}
			}
		}
	}
	if len(at) == 0 || !f.timeOrdered() {
		return
	}
	for _, i := range at {
		add(i, packs.LevelWarn, ruleL6, "train_test_split shuffles time-ordered data; pass shuffle=False or use TimeSeriesSplit")
	}
}

// --- L7 -----------------------------------------------------------------------

func idName(s string) bool {
	l := strings.ToLower(s)
	return l == "id" || strings.HasSuffix(l, "_id") || strings.HasSuffix(l, "id") && hasSeg(s, "id") || strings.Contains(l, "uuid")
}

// groupBys returns the id-like columns a file groups by.
func (f *pyFile) groupBys() []string {
	var out []string
	for i, l := range f.Lines {
		for _, c := range f.Calls[i] {
			if c.Name != "groupby" {
				continue
			}
			for _, s := range stringsIn(l, c.Open, c.Close) {
				if idName(s) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// featureGroupBys reads the paths.features files (tracked, bounded) and
// returns the id columns they group by, with the file that does.
func featureGroupBys(ctx context.Context, in *packs.Input, globs []string) map[string]string {
	out := map[string]string{}
	if in.Git == nil {
		return out
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		return out
	}
	n := 0
	for _, p := range files {
		if ctx.Err() != nil || n >= maxFeatureFiles {
			break
		}
		if !pyLike(p) || !matchAny(globs, p) {
			continue
		}
		n++
		b, err := in.ReadNew(p, 1<<20)
		if err != nil {
			continue
		}
		f, err := parsePy(p, b)
		if err != nil {
			continue
		}
		for _, c := range f.groupBys() {
			if _, ok := out[c]; !ok {
				out[c] = p
			}
		}
	}
	return out
}

func (f *pyFile) mentions(name string) bool {
	for _, l := range f.Lines {
		if containsStr(l.Strings, name) || containsStr(idents(l.Code), name) {
			return true
		}
	}
	return false
}

func (f *pyFile) l7(add func(int, string, string, string), featGroups map[string]string) {
	groups := map[string]string{}
	for k, v := range featGroups {
		groups[k] = v
	}
	if len(featGroups) == 0 {
		for _, c := range f.groupBys() {
			groups[c] = f.Path
		}
	}
	var col, where string
	for c, p := range groups {
		if f.mentions(c) && (col == "" || c < col) {
			col, where = c, p
		}
	}
	if col == "" {
		return
	}
	for i := range f.Lines {
		for _, c := range f.Calls[i] {
			grouped := groupSplitters[c.Name]
			if _, ok := c.kw("groups"); ok {
				grouped = true
			}
			if grouped || (c.Name != "train_test_split" && !(splitters[c.Name] && c.Name != "TimeSeriesSplit")) {
				continue
			}
			add(i, packs.LevelWarn, ruleL7, fmt.Sprintf("%s splits rows without groups while %q is an entity id (grouped by in %s); use a Group* splitter so one entity stays on one side", c.Name, col, where))
		}
	}
}

// --- L8 -----------------------------------------------------------------------

var forRe = regexp.MustCompile(`^(?:async )?for (.+?) in (.+):$`)

func (f *pyFile) l8(add func(int, string, string, string)) {
	if !f.imports("torch") {
		return
	}
	for i, l := range f.Lines {
		m := forRe.FindStringSubmatch(l.Code)
		if m == nil {
			continue
		}
		loopy := false
		for _, id := range idents(m[2]) {
			if heldOut(id) {
				loopy = true
			}
		}
		if !loopy {
			continue
		}
		start := 0
		for j := i - 1; j >= 0; j-- {
			c := f.Lines[j].Code
			if f.Lines[j].Indent < l.Indent && (strings.HasPrefix(c, "def ") || strings.HasPrefix(c, "async def ")) {
				start = j
				for start > 0 && strings.HasPrefix(f.Lines[start-1].Code, "@") {
					start--
				}
				break
			}
		}
		end := i
		for j := i + 1; j < len(f.Lines) && f.Lines[j].Indent > l.Indent; j++ {
			end = j
		}
		hasEval, hasNoGrad := false, false
		for j := start; j <= end; j++ {
			c := f.Lines[j].Code
			if strings.Contains(c, "no_grad") || strings.Contains(c, "inference_mode") || strings.Contains(c, "set_grad_enabled(False)") {
				hasNoGrad = true
			}
			for _, k := range f.Calls[j] {
				if k.Name == "eval" && len(k.Args) == 0 {
					hasEval = true
				}
			}
		}
		var miss []string
		if !hasEval {
			miss = append(miss, "model.eval()")
		}
		if !hasNoGrad {
			miss = append(miss, "torch.no_grad()/inference_mode()")
		}
		if len(miss) > 0 {
			add(i, packs.LevelWarn, ruleL8, "an evaluation loop over "+strings.TrimSpace(m[2])+" runs without "+strings.Join(miss, " and "))
		}
	}
}
