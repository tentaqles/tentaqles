package data

import (
	"bytes"
	"context"
	"path"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// data/fabric-artifacts (spec C §3.4): Microsoft Fabric git-integration
// items. A notebook is a `<name>.Notebook/` folder with `.platform` and
// `notebook-content.<ext>`; a pipeline is `<name>.DataPipeline/` with
// `pipeline-content.json`, whose notebook activities reference a notebook's
// `.platform` logicalId as `notebookId`.
const (
	ruleFabricParams    = "fabric-no-parameters-cell"
	ruleFabricLakehouse = "fabric-pinned-lakehouse"
	ruleOneLakePath     = "fabric-literal-onelake-path"
	ruleFabricRetry     = "fabric-copy-no-retry"
)

// maxFabricItems bounds the tracked .platform and pipeline files read to
// resolve notebook references.
const maxFabricItems = 500

var (
	guidPat      = `[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`
	oneLakeRe    = regexp.MustCompile(`(?i)abfss://(` + guidPat + `)@onelake\.(?:dfs|blob)\.fabric\.microsoft\.com|onelake\.(?:dfs|blob)\.fabric\.microsoft\.com/(` + guidPat + `)`)
	lakehouseWS  = regexp.MustCompile(`"default_lakehouse_workspace_id"\s*:\s*"(` + guidPat + `)"`)
	paramsCellRe = regexp.MustCompile(`(?m)^# PARAMETERS(?: CELL)? \*{5,}`)
	tagsParamsRe = regexp.MustCompile(`"tags"\s*:\s*\[[^\]]*"parameters"`)
	notebookExts = []string{"notebook-content.py", "notebook-content.sql", "notebook-content.scala", "notebook-content.r", "notebook-content.ipynb"}
)

const zeroGUID = "00000000-0000-0000-0000-000000000000"

func fabricApplies(in *packs.Input) bool {
	return in.AnyChanged(fabricGlobs...) || pipelineApplies(in)
}

// itemDir returns the `<name>.Notebook` or `<name>.DataPipeline` folder of
// p, or "".
func itemDir(p, suffix string) string {
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if strings.HasSuffix(s, suffix) && i < len(parts)-1 {
			return strings.Join(parts[:i+1], "/")
		}
	}
	return ""
}

func runFabricArtifacts(_ context.Context, in *packs.Input) packs.Result {
	var fs findings
	n, skipped := 0, 0

	// Changed notebooks and pipelines.
	notebooks := map[string]bool{}
	var pipelines []string
	for _, p := range in.ChangedPaths() {
		if d := itemDir(p, ".Notebook"); d != "" {
			notebooks[d] = true
		}
		if d := itemDir(p, ".DataPipeline"); d != "" && path.Base(p) == "pipeline-content.json" {
			pipelines = append(pipelines, p)
		}
	}
	refs, ids := fabricIndex(in)

	// Pipelines: copy activities without a retry; the notebooks they run.
	for _, p := range pipelines {
		b, err := in.ReadNew(p, maxPipelineFile+1)
		if err != nil || len(b) > maxPipelineFile {
			skipped++
			continue
		}
		n++
		root, err := parseJSONLines(b)
		if err != nil {
			fs.addFile(p, 1, packs.LevelWarn, ruleFabricRetry, "pipeline-content.json does not parse; the copy retry rule was not checked")
			continue
		}
		copyRetries(p, root, &fs)
		for _, id := range notebookIDs(root) {
			if d := ids[strings.ToLower(id)]; d != "" {
				notebooks[d] = true
			}
		}
	}

	// Notebooks: parameters cell, pinned lakehouse.
	for d := range notebooks {
		file, b := notebookContent(in, d)
		if file == "" {
			continue
		}
		n++
		if m := lakehouseWS.FindSubmatchIndex(b); m != nil && string(b[m[2]:m[3]]) != zeroGUID {
			if !rawSuppressed(b, lineOf(b, m[0]), ruleFabricLakehouse) {
				fs.addFile(file, lineOf(b, m[0]), packs.LevelWarn, ruleFabricLakehouse,
					"the notebook pins its default lakehouse to a workspace GUID; bind the lakehouse per environment (deployment rules or notebookutils) instead")
			}
		}
		logical := platformLogicalID(in, d)
		if logical != "" && refs[strings.ToLower(logical)] && !hasParametersCell(file, b) {
			fs.addFile(file, 1, packs.LevelWarn, ruleFabricParams,
				"a pipeline runs this notebook but it has no parameters cell; add one so the pipeline passes the run date and environment")
		}
	}

	// Literal OneLake / ABFSS paths with a workspace GUID, in any changed
	// pipeline or Fabric file.
	seen := map[string]bool{}
	for _, p := range in.ChangedPaths() {
		if !(matchAny(fabricGlobs, p) || matchAny(pipelineGlobs(in), p) || ext(p) == ".py" || ext(p) == ".ipynb") || seen[p] {
			continue
		}
		seen[p] = true
		if path.Base(p) == ".platform" {
			continue
		}
		b, err := in.ReadNew(p, maxPipelineFile+1)
		if err != nil || len(b) > maxPipelineFile {
			continue
		}
		if !bytes.Contains(bytes.ToLower(b), []byte("onelake.")) {
			continue
		}
		for _, m := range oneLakeRe.FindAllSubmatchIndex(b, -1) {
			g := ""
			if m[2] >= 0 {
				g = string(b[m[2]:m[3]])
			} else {
				g = string(b[m[4]:m[5]])
			}
			if g == zeroGUID {
				continue
			}
			l := lineOf(b, m[0])
			if rawSuppressed(b, l, ruleOneLakePath) {
				continue
			}
			fs.addFile(p, l, packs.LevelError, ruleOneLakePath,
				"literal OneLake path with a workspace GUID; take the workspace and lakehouse from a parameter, the default lakehouse or a relative Tables/ or Files/ path")
		}
	}
	return result(n, "Fabric item", skipped, &fs)
}

// fabricIndex reads the tracked Fabric items: the set of notebook ids that
// pipelines reference, and logicalId -> notebook folder.
func fabricIndex(in *packs.Input) (refs map[string]bool, ids map[string]string) {
	refs, ids = map[string]bool{}, map[string]string{}
	if in.Git == nil {
		return
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		return
	}
	// Changed files that are not tracked yet count too.
	all := append(append([]string{}, files...), in.ChangedPaths()...)
	read := 0
	done := map[string]bool{}
	for _, f := range all {
		if done[f] {
			continue
		}
		done[f] = true
		isPlatform := path.Base(f) == ".platform" && itemDir(f, ".Notebook") != ""
		isPipeline := path.Base(f) == "pipeline-content.json" && itemDir(f, ".DataPipeline") != ""
		if !isPlatform && !isPipeline {
			continue
		}
		if read++; read > maxFabricItems {
			return
		}
		b, err := in.ReadNew(f, 1<<20)
		if err != nil {
			continue
		}
		root, err := parseJSONLines(b)
		if err != nil {
			continue
		}
		if isPlatform {
			if id := scan.Scalar(scan.Get(root, "config", "logicalId")); id != "" {
				ids[strings.ToLower(id)] = itemDir(f, ".Notebook")
			}
			continue
		}
		for _, id := range notebookIDs(root) {
			refs[strings.ToLower(id)] = true
		}
	}
	return
}

func notebookIDs(root *yaml.Node) []string {
	var out []string
	scan.Walk(root, func(p []string, n *yaml.Node) bool {
		if len(p) > 0 && p[len(p)-1] == "notebookId" {
			if s := scan.Scalar(n); s != "" {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

func platformLogicalID(in *packs.Input, dir string) string {
	b, err := in.ReadNew(dir+"/.platform", 1<<20)
	if err != nil {
		return ""
	}
	root, err := parseJSONLines(b)
	if err != nil {
		return ""
	}
	return scan.Scalar(scan.Get(root, "config", "logicalId"))
}

func notebookContent(in *packs.Input, dir string) (string, []byte) {
	for _, name := range notebookExts {
		p := dir + "/" + name
		if b, err := in.ReadNew(p, maxPipelineFile+1); err == nil && len(b) <= maxPipelineFile {
			return p, b
		}
	}
	return "", nil
}

// hasParametersCell: a `# PARAMETERS CELL` marker, a parameters cell found
// by the scanner, or a cell whose metadata tags it "parameters".
func hasParametersCell(file string, b []byte) bool {
	if paramsCellRe.Match(b) {
		return true
	}
	if strings.HasSuffix(file, ".ipynb") {
		return tagsParamsRe.Match(b)
	}
	for _, c := range scan.FabricCells(b) {
		if c.Kind == "parameters" {
			return true
		}
	}
	var meta strings.Builder
	for _, l := range strings.Split(string(b), "\n") {
		if t := strings.TrimSpace(l); strings.HasPrefix(t, "# META") {
			meta.WriteString(strings.TrimPrefix(t, "# META"))
			meta.WriteByte(' ')
		}
	}
	return tagsParamsRe.MatchString(meta.String())
}

// copyRetries warns on Copy activities whose policy.retry is 0 or absent
// (the default is no retry), at any nesting depth (ForEach, If, Until).
func copyRetries(file string, root *yaml.Node, fs *findings) {
	scan.Walk(root, func(_ []string, n *yaml.Node) bool {
		if n.Kind != yaml.MappingNode || scan.Scalar(scan.Get(n, "type")) != "Copy" {
			return true
		}
		name := scan.Scalar(scan.Get(n, "name"))
		r := scan.Get(n, "policy", "retry")
		switch {
		case r == nil:
			fs.addFile(file, n.Line, packs.LevelWarn, ruleFabricRetry, "copy activity "+name+" has no retry policy (the default is 0); set policy.retry and retryIntervalInSeconds")
		case scan.Scalar(r) == "0":
			fs.addFile(file, r.Line, packs.LevelWarn, ruleFabricRetry, "copy activity "+name+" has retry 0; set policy.retry and retryIntervalInSeconds")
		}
		return true
	})
}

// parseJSONLines parses JSON with line numbers (JSON is YAML; tabs used as
// indentation are turned into spaces first, which keeps every line number).
func parseJSONLines(b []byte) (*yaml.Node, error) {
	return scan.ParseYAML(bytes.ReplaceAll(b, []byte("\t"), []byte(" ")))
}

// rawSuppressed checks `tq:allow <rule>: <reason>` on file line n or the
// line above it.
func rawSuppressed(b []byte, n int, rule string) bool {
	lines := strings.Split(string(b), "\n")
	for _, k := range []int{n, n - 1} {
		if k >= 1 && k <= len(lines) && allowMarker(lines[k-1], rule) {
			return true
		}
	}
	return false
}
