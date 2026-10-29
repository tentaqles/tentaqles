package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// data/cost-estimate (external, opt-in with packs.data.cost.estimate_in_dod):
// compile the changed dbt models against the dev target into a scratch target
// path, then run the plan-only estimate on each compiled model. It needs a
// dev connection (tq dotenv run around tq pack check) and is never on by
// default. A production target is refused.

const checkCostEstimate = "data/cost-estimate"

// costEstimateMaxModels bounds one run.
const costEstimateMaxModels = 20

// costAdapterEngine maps dbt adapter types to engines.
var costAdapterEngine = map[string]string{
	"bigquery": engBigQuery, "snowflake": engSnowflake, "databricks": engDatabricks, "spark": engDatabricks,
	"postgres": engPostgres, "redshift": "", "fabric": engFabric, "synapse": engSynapse,
}

func costEstimateApplies(in *packs.Input) bool {
	return costOf(in.Settings()).EstimateInDoD() && in.AnyChanged("*.sql")
}

// costDBTProject is packs.data.dbt.project, else the repo root when it holds
// dbt_project.yml, else "" (no project).
func costDBTProject(in *packs.Input) string {
	if p := strings.Trim(in.Settings().String("dbt.project"), "/"); p != "" {
		return p
	}
	if _, err := os.Stat(filepath.Join(in.Root, "dbt_project.yml")); err == nil {
		return "."
	}
	return ""
}

// costModelPaths reads model-paths from dbt_project.yml (default models).
func costModelPaths(raw []byte) []string {
	n, err := scan.ParseYAML(raw)
	if err != nil {
		return []string{"models"}
	}
	if ps := scan.Strings(scan.Get(n, "model-paths")); len(ps) > 0 {
		return ps
	}
	return []string{"models"}
}

func runCostEstimate(ctx context.Context, in *packs.Input) packs.Result {
	res := packs.Result{Status: packs.OK, Findings: []packs.Finding{}}
	s := in.Settings()
	proj := costDBTProject(in)
	if proj == "" {
		return packs.Result{Status: packs.Skip, Detail: "no dbt project (set packs.data.dbt.project)", Findings: []packs.Finding{}}
	}
	projAbs := filepath.Join(in.Root, filepath.FromSlash(proj))
	raw, err := os.ReadFile(filepath.Join(projAbs, "dbt_project.yml"))
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "cannot read " + path.Join(proj, "dbt_project.yml"), Findings: []packs.Finding{}}
	}
	target := s.String("dbt.dev_target")
	if target == "" {
		target = "dev"
	}
	prod := s.Strings("dbt.prod_targets")
	if len(prod) == 0 {
		prod = []string{"prod", "production"}
	}
	for _, p := range prod {
		if p == target {
			return packs.Result{Status: packs.Fail, Detail: "dbt.dev_target " + target + " is a production target; the estimate only runs against dev", Findings: []packs.Finding{}}
		}
	}
	// Changed models, relative to the project.
	var models []string
	mps := costModelPaths(raw)
	for _, p := range in.ChangedPaths() {
		if !strings.HasSuffix(p, ".sql") {
			continue
		}
		rel := p
		if proj != "." {
			if !strings.HasPrefix(p, proj+"/") {
				continue
			}
			rel = strings.TrimPrefix(p, proj+"/")
		}
		for _, mp := range mps {
			if strings.HasPrefix(rel, strings.Trim(mp, "/")+"/") {
				models = append(models, rel)
				break
			}
		}
	}
	sort.Strings(models)
	if len(models) == 0 {
		return packs.Result{Status: packs.Skip, Detail: "no changed dbt model", Findings: []packs.Finding{}}
	}
	extra := 0
	if len(models) > costEstimateMaxModels {
		extra = len(models) - costEstimateMaxModels
		models = models[:costEstimateMaxModels]
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "scratch dir: " + err.Error(), Findings: []packs.Finding{}}
	}
	defer cleanup()
	targetPath := filepath.Join(scratch, "target")
	args := []string{"compile", "--target", target, "--target-path", targetPath, "--log-path", filepath.Join(scratch, "logs"), "--select"}
	for _, m := range models {
		args = append(args, "path:"+m)
	}
	out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "data", Tool: "dbt", Args: args, Dir: projAbs, Timeout: 600 * time.Second, PassEnv: dbtRunEnv(projAbs)})
	tool := &packs.ToolRun{Name: "dbt", Version: out.Version, Path: out.Path, ExitCode: out.ExitCode, DurationMS: out.Duration.Milliseconds(), TimedOut: out.TimedOut}
	res.Tool = tool
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && out.ExitCode == 0 && !out.TimedOut {
		return packs.Result{Status: packs.Fail, Detail: "dbt compile did not run: " + err.Error(), Findings: []packs.Finding{}, Tool: tool}
	}
	if out.TimedOut || out.ExitCode != 0 {
		return packs.Result{Status: packs.Fail, Detail: fmt.Sprintf("dbt compile failed (exit %d); run dbt compile yourself to see why", out.ExitCode), Findings: []packs.Finding{}, Tool: tool}
	}
	mraw, err := costReadCapped(filepath.Join(targetPath, "manifest.json"), 64<<20)
	if err != nil {
		return packs.Result{Status: packs.Fail, Detail: "dbt compile wrote no manifest.json", Findings: []packs.Finding{}, Tool: tool}
	}
	var man struct {
		Metadata struct {
			AdapterType string `json:"adapter_type"`
		} `json:"metadata"`
		Nodes map[string]struct {
			ResourceType     string `json:"resource_type"`
			OriginalFilePath string `json:"original_file_path"`
			CompiledCode     string `json:"compiled_code"`
			CompiledSQL      string `json:"compiled_sql"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(mraw, &man); err != nil {
		return packs.Result{Status: packs.Fail, Detail: "manifest.json did not parse", Findings: []packs.Finding{}, Tool: tool}
	}
	engine, known := costAdapterEngine[man.Metadata.AdapterType]
	if !known || engine == "" {
		return packs.Result{Status: packs.Skip, Detail: "dbt adapter " + man.Metadata.AdapterType + " has no plan-only estimate", Findings: []packs.Finding{}, Tool: tool}
	}
	compiled := map[string]string{}
	for _, n := range man.Nodes {
		if n.ResourceType != "model" {
			continue
		}
		code := n.CompiledCode
		if code == "" {
			code = n.CompiledSQL
		}
		compiled[filepath.ToSlash(n.OriginalFilePath)] = code
	}
	counts := map[string]int{}
	for _, m := range models {
		file := m
		if proj != "." {
			file = proj + "/" + m
		}
		code, ok := compiled[m]
		if !ok || strings.TrimSpace(code) == "" {
			res.Findings = append(res.Findings, packs.Finding{File: file, Level: packs.LevelWarn, Rule: "cost-not-compiled", Message: "dbt compiled no SQL for this model; not estimated"})
			continue
		}
		est, err := RunEstimate(ctx, in.Tools, s, EstimateRequest{Engine: engine, SQL: []byte(code), ScratchDir: scratch, Dir: projAbs})
		switch {
		case errors.Is(err, ErrCostRefused):
			res.Findings = append(res.Findings, packs.Finding{File: file, Level: packs.LevelWarn, Rule: "cost-not-estimable", Message: "the compiled model is not a single read-only SELECT the estimate can send (" + costRefusal(err) + ")"})
		case err != nil:
			res.Findings = append(res.Findings, packs.Finding{File: file, Level: packs.LevelError, Rule: "cost-estimate-failed", Message: costErrorLine(err)})
		case est.Verdict == VerdictOver:
			res.Findings = append(res.Findings, packs.Finding{File: file, Level: packs.LevelError, Rule: "cost-over-cap", Message: fmt.Sprintf("%s %s is above %s (%s)", est.Metric, costFormat(est), est.Cap, est.CapKey)})
		case est.Verdict == VerdictAsk:
			res.Findings = append(res.Findings, packs.Finding{File: file, Level: packs.LevelWarn, Rule: "cost-unknown", Message: fmt.Sprintf("%s %s: no cap to judge it (%s)", est.Metric, costFormat(est), costFirst(est.Note, "ask a human"))})
		}
		counts[est.Verdict]++
	}
	errs, _ := res.Counts()
	if errs > 0 {
		res.Status = packs.Fail
	}
	res.Detail = fmt.Sprintf("%d models estimated on %s: %d within, %d over, %d unknown", len(models), engine, counts[VerdictWithin], counts[VerdictOver], counts[VerdictAsk])
	if extra > 0 {
		res.Detail += fmt.Sprintf(" (%d more changed models not estimated)", extra)
	}
	return res
}

func costReadCapped(p string, max int64) ([]byte, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if fi.Size() > max {
		return nil, fmt.Errorf("%s is larger than %d bytes", p, max)
	}
	return os.ReadFile(p)
}

// costRefusal is the refusal without the query.
func costRefusal(err error) string {
	return strings.TrimPrefix(err.Error(), ErrCostRefused.Error()+": ")
}

// costErrorLine is an engine error as one line (it never holds the query).
func costErrorLine(err error) string {
	return strings.SplitN(err.Error(), "\n", 2)[0]
}

func costFirst(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
