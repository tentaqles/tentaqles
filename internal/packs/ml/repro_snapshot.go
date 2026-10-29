package ml

// ml/data-snapshot (spec C §4.3): a training file reads only data pinned by
// data_manifest (name with sha256) or DVC, and never a moving reference.

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

const (
	ruleSnapUnpinned   = "snapshot-unpinned"
	ruleSnapMoving     = "snapshot-moving"
	ruleSnapUnresolved = "snapshot-unresolved"
)

// fileReaders read a file or a URI given as the first argument.
var fileReaders = strSet("read_csv", "read_parquet", "read_json", "read_excel", "read_feather", "read_table", "read_orc",
	"read_hdf", "read_fwf", "scan_parquet", "scan_csv", "scan_ndjson", "read_ndjson", "load_dataset", "loadtxt", "genfromtxt")

// sparkFileReaders are DataFrameReader methods (spark.read.parquet(...)).
var sparkFileReaders = strSet("parquet", "csv", "json", "orc", "text", "load")

var (
	latestRe = regexp.MustCompile(`(?i)(^|[/:_.@=-])latest($|[/:_.@-])`)
	asOfRe   = regexp.MustCompile(`(?i)(\bas\s+of\b|\bat\s*\(\s*(timestamp|offset|statement)\b|\bbefore\s*\(\s*(timestamp|offset|statement)\b|\bfor\s+system_time\b)`)
	selectRe = regexp.MustCompile(`(?i)\bselect\b`)
)

type pin struct{ paths []string }

// loadPins reads data_manifest (YAML name -> {uri, sha256}, a list, or a
// dvc.lock) and the root dvc.lock.
func loadPins(in *packs.Input) pin {
	var p pin
	if m := in.Settings().String("data_manifest"); m != "" {
		if b, err := in.ReadNew(m, 4<<20); err == nil {
			if path.Base(m) == "dvc.lock" {
				p.paths = append(p.paths, dvcLockPaths(b)...)
			} else {
				p.paths = append(p.paths, manifestPins(b)...)
			}
		}
	}
	if in.Settings().String("data_manifest") != "dvc.lock" {
		if b, err := in.ReadNew("dvc.lock", 4<<20); err == nil {
			p.paths = append(p.paths, dvcLockPaths(b)...)
		}
	}
	return p
}

// manifestPins returns the names and URIs of entries that carry a sha256.
func manifestPins(b []byte) []string {
	root, err := scan.ParseYAML(b)
	if err != nil || root == nil {
		return nil
	}
	var out []string
	node := root
	if s := scan.Get(root, "snapshots"); s != nil {
		node = s
	}
	add := func(name string, n *yaml.Node) {
		sha := scan.Scalar(scan.Get(n, "sha256"))
		if sha == "" {
			return
		}
		if name != "" {
			out = append(out, name)
		}
		for _, k := range []string{"uri", "path"} {
			if v := scan.Scalar(scan.Get(n, k)); v != "" {
				out = append(out, v)
			}
		}
	}
	for _, kv := range scan.Entries(node) {
		add(kv.Key, kv.Value)
	}
	for _, it := range scan.Items(node) {
		add(scan.Scalar(scan.Get(it, "name")), it)
	}
	return out
}

// dvcLockPaths returns the hashed deps and outs paths of a dvc.lock.
func dvcLockPaths(b []byte) []string {
	root, err := scan.ParseYAML(b)
	if err != nil || root == nil {
		return nil
	}
	var out []string
	for _, st := range scan.Entries(scan.Get(root, "stages")) {
		for _, k := range []string{"outs", "deps"} {
			for _, it := range scan.Items(scan.Get(st.Value, k)) {
				if scan.Scalar(scan.Get(it, "md5")) == "" && scan.Scalar(scan.Get(it, "hash")) == "" && scan.Scalar(scan.Get(it, "etag")) == "" && scan.Scalar(scan.Get(it, "checksum")) == "" {
					continue
				}
				if v := scan.Scalar(scan.Get(it, "path")); v != "" {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

func normData(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), `\`, "/")
	s = strings.TrimPrefix(s, "./")
	return strings.TrimSuffix(s, "/")
}

// pinned reports whether a source is pinned: listed in the pins (equal, a
// suffix at a / boundary, or under a pinned directory), or tracked by a
// .dvc file next to it or to a parent directory.
func (p pin) pinned(in *packs.Input, src string) bool {
	s := normData(src)
	for _, q := range p.paths {
		q = normData(q)
		if q == "" {
			continue
		}
		if s == q || strings.HasSuffix(s, "/"+q) || strings.HasSuffix(q, "/"+s) || strings.HasPrefix(s, q+"/") {
			return true
		}
	}
	if strings.Contains(s, "://") || path.IsAbs(s) {
		return false
	}
	for d := s; d != "." && d != "/" && d != ""; d = path.Dir(d) {
		if existsNew(in, d+".dvc") {
			return true
		}
		if path.Dir(d) == d {
			break
		}
	}
	return false
}

func runSnapshot(ctx context.Context, in *packs.Input) packs.Result {
	files, fs := selectFiles(ctx, in, false)
	pins := loadPins(in)
	n := 0
	for _, s := range files {
		if !s.training {
			continue
		}
		n++
		fs = append(fs, snapshotFile(in, s.f, pins)...)
	}
	return packs.Result{Status: packs.OK, Detail: "data reads checked in " + lintPlural(n, "training file"), Findings: fs}
}

// describe names a source in a message: a repo path as is, a URI by its
// scheme only.
func describe(src string) string {
	if i := strings.Index(src, "://"); i > 0 {
		return "a " + src[:i] + ":// source"
	}
	return fmt.Sprintf("%q", src)
}

func snapshotFile(in *packs.Input, f *pyFile, pins pin) []packs.Finding {
	var out []packs.Finding
	add := func(i int, level, rule, msg string) {
		if !f.suppressed(i, rule, "ml/data-snapshot") {
			out = append(out, f.find(i, level, rule, msg))
		}
	}
	for i, l := range f.Lines {
		lineText := strings.Join(l.Strings, " ")
		versioned := strings.Contains(lineText, "versionAsOf") || strings.Contains(lineText, "timestampAsOf") ||
			strings.Contains(strings.ToLower(l.Code), "snapshot_id") || strings.Contains(l.Code, "as_of")
		for _, c := range f.Calls[i] {
			spark := strings.Contains(c.Receiver, "read") || strings.Contains(l.Code, "spark.read")
			switch {
			case c.Name == "read_sql" || c.Name == "read_sql_query" || c.Name == "read_gbq" || (c.Name == "sql" && strings.Contains(c.Receiver, "spark")):
				a, ok := c.argN(0, "sql")
				if !ok {
					a, ok = c.argN(0, "query")
				}
				q, resolved := "", false
				if ok {
					q, resolved = f.strValue(l, a)
				}
				switch {
				case !resolved:
					add(i, packs.LevelWarn, ruleSnapUnresolved, c.Name+" runs a query tq cannot read; bound it with an as-of clause and pin it in data_manifest")
				case selectRe.MatchString(q) && !asOfRe.MatchString(q):
					add(i, packs.LevelError, ruleSnapMoving, c.Name+" runs a SELECT with no as-of bound (FOR SYSTEM_TIME AS OF, VERSION AS OF, TIMESTAMP AS OF, AT/BEFORE)")
				}
			case c.Name == "read_sql_table" || (c.Name == "table" && (strings.Contains(c.Receiver, "spark") || spark)) || c.Name == "read_delta" || c.Name == "scan_delta" || c.Name == "forName":
				if versioned || hasAnyKw(c, "version", "as_of", "snapshot_id", "timestamp") {
					continue
				}
				name := "a table"
				if a, ok := c.argN(0, ""); ok {
					if v, ok := f.strValue(l, a); ok && !strings.Contains(v, "://") {
						name = fmt.Sprintf("table %q", v)
					}
				}
				add(i, packs.LevelError, ruleSnapMoving, "reads "+name+" with no versionAsOf/timestampAsOf/snapshot id")
			case fileReaders[c.Name] || (sparkFileReaders[c.Name] && spark):
				if c.Name == "load" && containsStr(l.Strings, "delta") {
					if !versioned {
						add(i, packs.LevelError, ruleSnapMoving, "loads a Delta table with no versionAsOf/timestampAsOf")
					}
					continue
				}
				a, ok := c.argN(0, "path")
				if !ok {
					a, ok = c.argN(0, "filepath_or_buffer")
				}
				if !ok {
					continue
				}
				src, resolved := f.strValue(l, a)
				switch {
				case !resolved:
					add(i, packs.LevelWarn, ruleSnapUnresolved, c.Name+" reads a source tq cannot resolve; use a literal path pinned in data_manifest or DVC")
				case latestRe.MatchString(src):
					add(i, packs.LevelError, ruleSnapMoving, c.Name+" reads a moving reference ("+describe(src)+" names latest)")
				case versioned:
				case !pins.pinned(in, src):
					add(i, packs.LevelError, ruleSnapUnpinned, c.Name+" reads "+describe(src)+", which is not pinned by data_manifest (with sha256) or DVC")
				}
			}
		}
	}
	return out
}

func hasAnyKw(c pcall, keys ...string) bool {
	for _, k := range keys {
		if _, ok := c.kw(k); ok {
			return true
		}
	}
	return false
}
