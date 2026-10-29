package ml

// ml/unsafe-deserialize (spec C §4.3): pickle-based loads (pickle, joblib,
// cloudpickle, dill, torch.load without weights_only=True) of a path that
// is not repo-relative or that comes from a download. Error in code, warn
// in tests.

import (
	"context"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
)

const ruleUnsafeDeserialize = "unsafe-deserialize"

var pickleModules = set("pickle", "cPickle", "_pickle", "dill", "cloudpickle", "joblib")

var downloadRe = regexp.MustCompile(`(?i)(download|urlopen|urlretrieve|requests\.|httpx\.|urllib|wget|get_file|fetch|hf_hub|snapshot_download|boto3|s3fs|gcsfs|fsspec)`)

func runDeserialize(ctx context.Context, in *packs.Input) packs.Result {
	var fs []packs.Finding
	n := 0
	for _, p := range in.ChangedPaths() {
		if ctx.Err() != nil || n >= maxFiles {
			break
		}
		if !pyLike(p) {
			continue
		}
		n++
		f, err := loadPy(in, p)
		if err != nil {
			fs = append(fs, packs.Finding{File: p, Level: packs.LevelWarn, Rule: "unreadable", Message: "cannot scan this file: " + shortErr(err)})
			continue
		}
		fs = append(fs, deserializeFile(f)...)
	}
	return packs.Result{Status: packs.OK, Detail: "deserialization checked in " + plural(n, "file"), Findings: fs}
}

// unsafeLoad reports whether a call is a pickle-based load, and its name.
func (f *pyFile) unsafeLoad(l scan.PyLine, c pcall) (string, bool) {
	recv := c.Receiver
	if i := strings.LastIndex(recv, "."); i >= 0 {
		recv = recv[i+1:]
	}
	switch {
	case (c.Name == "load" || c.Name == "loads") && pickleModules[recv]:
		return c.Func, true
	case (c.Name == "load" || c.Name == "loads") && c.Receiver == "" && pickleModules[f.fromImp[c.Name]]:
		return f.fromImp[c.Name] + "." + c.Name, true
	case c.Name == "load" && recv == "torch":
		if a, ok := c.kw("weights_only"); ok && a.Text == "True" {
			return "", false
		}
		return "torch.load without weights_only=True", true
	case c.Name == "read_pickle" && (recv == "pd" || recv == "pandas"):
		return c.Func, true
	case c.Name == "load" && (recv == "np" || recv == "numpy"):
		if a, ok := c.kw("allow_pickle"); ok && a.Text == "True" {
			return "np.load(allow_pickle=True)", true
		}
	}
	return "", false
}

type origin int

const (
	originUnknown origin = iota
	originRepo
	originOutside
	originDownload
)

// repoRelative reports whether a literal path stays inside the repo.
func repoRelative(p string) bool {
	q := strings.ReplaceAll(p, `\`, "/")
	if q == "" || strings.Contains(q, "://") || strings.HasPrefix(q, "/") || strings.HasPrefix(q, "~") || (len(q) > 1 && q[1] == ':') {
		return false
	}
	for _, seg := range strings.Split(q, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

func classify(p string) origin {
	if repoRelative(p) {
		return originRepo
	}
	return originOutside
}

var openAsRe = regexp.MustCompile(`^with\s+(?:.*,\s*)?(?:[\w.]*open)\s*\((.*?)\)\s+as\s+([A-Za-z_]\w*)\s*:?$`)

// originOf traces an argument back to a literal path, an open() of one, a
// download, or nothing (depth-bounded).
func (f *pyFile) originOf(line int, text string, depth int) origin {
	text = strings.TrimSpace(text)
	if depth > 3 || text == "" {
		return originUnknown
	}
	l := f.Lines[line]
	if downloadRe.MatchString(text) {
		return originDownload
	}
	if isStrLit(text) {
		if v, ok := litValue(l, arg{Text: text, From: strings.Index(l.Code, text)}); ok {
			return classify(v)
		}
	}
	// open("x", "rb") inline, or Path("x") / os.path.join("a", "b").
	for _, c := range f.Calls[line] {
		if (c.Name == "open" || c.Name == "Path" || c.Name == "join") && strings.HasPrefix(text, c.Func+"(") {
			if a, ok := c.argN(0, "file"); ok {
				return f.originOf(line, a.Text, depth+1)
			}
		}
	}
	name := firstIdent(text)
	if !identRe.MatchString(text) {
		if name == "" {
			return originUnknown
		}
	}
	if v, ok := f.consts[name]; ok {
		return classify(v)
	}
	// The latest earlier binding of name: `with open(x) as name`, or `name = …`.
	for j := line; j >= 0; j-- {
		lj := f.Lines[j]
		if m := openAsRe.FindStringSubmatch(lj.Code); m != nil && m[2] == name {
			first := strings.Split(m[1], ",")[0]
			return f.originOf(j, first, depth+1)
		}
		if j == line {
			continue
		}
		if lhs, rhs, _, ok := splitAssign(lj.Code); ok && containsStr(lhs, name) {
			if len(lhs) == 1 {
				return f.originOf(j, rhs, depth+1)
			}
			if downloadRe.MatchString(rhs) {
				return originDownload
			}
			return originUnknown
		}
	}
	return originUnknown
}

func deserializeFile(f *pyFile) []packs.Finding {
	var out []packs.Finding
	level := packs.LevelError
	test := isTestPath(f.Path)
	if test {
		level = packs.LevelWarn
	}
	for i, l := range f.Lines {
		for _, c := range f.Calls[i] {
			what, unsafe := f.unsafeLoad(l, c)
			if !unsafe {
				continue
			}
			a, ok := c.argN(0, "")
			o := originUnknown
			if ok {
				o = f.originOf(i, a.Text, 0)
			}
			var why string
			switch o {
			case originRepo:
				continue
			case originDownload:
				why = "from a download"
			case originOutside:
				why = "from a path outside the repo"
			default:
				why = "from a path tq cannot show is repo-relative"
			}
			if f.suppressed(i, ruleUnsafeDeserialize, "ml/unsafe-deserialize") {
				continue
			}
			out = append(out, f.find(i, level, ruleUnsafeDeserialize,
				what+" deserializes "+why+"; loading runs arbitrary code. Use safetensors or skops, torch.load(weights_only=True), or a pinned repo artifact"))
		}
	}
	return out
}
