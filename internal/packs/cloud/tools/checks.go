package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/policy"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// Tool names (packs.cloud.tools) and the IaC kinds each one reads.
const (
	Checkov  = "checkov"
	Trivy    = "trivy"
	Conftest = "conftest"
	Tflint   = "tflint"
	Bicep    = "bicep"
	CfnLint  = "cfn-lint"
)

// KindsFor lists the packs.cloud.iac kinds a tool reads from source.
var KindsFor = map[string][]string{
	Checkov:  {policy.KindTerraform, policy.KindTofu, policy.KindBicep, policy.KindARM, policy.KindCloudFormation, policy.KindCDK},
	Trivy:    {policy.KindTerraform, policy.KindTofu, policy.KindARM, policy.KindCloudFormation, policy.KindCDK},
	Conftest: policy.SourceKinds,
	Tflint:   {policy.KindTerraform, policy.KindTofu},
	Bicep:    {policy.KindBicep},
	CfnLint:  {policy.KindCloudFormation, policy.KindCDK},
}

// Env is what each tool needs from the parent environment beyond
// toolrun.BaseEnv (packs.Check.Env). Nothing here holds a credential: the
// tools read local files only, so no cloud, registry or GitHub credential
// is passed (TF_VAR_*, GITHUB_TOKEN, BC_API_KEY and the AWS_*/ARM_* keys
// stay out).
//   - checkov, cfn-lint (Python): APPDATA, where a Windows `pip install
//     --user` puts its site-packages (%APPDATA%\Python); cfn-lint also reads
//     the region it validates from AWS_REGION, then AWS_DEFAULT_REGION.
//   - trivy: TRIVY_CACHE_DIR, else os.UserCacheDir (XDG_CACHE_HOME on
//     Linux, LOCALAPPDATA on Windows).
//   - tflint: TFLINT_PLUGIN_DIR and TFLINT_CONFIG_FILE, plus TF_DATA_DIR
//     and TF_WORKSPACE, which it reads as Terraform does.
//   - conftest, bicep: nothing beyond BaseEnv.
var Env = map[string][]string{
	Checkov:  {"APPDATA"},
	Trivy:    {"TRIVY_CACHE_DIR", "XDG_CACHE_HOME", "LOCALAPPDATA"},
	Conftest: nil,
	Tflint:   {"TFLINT_PLUGIN_DIR", "TFLINT_CONFIG_FILE", "TF_DATA_DIR", "TF_WORKSPACE"},
	Bicep:    nil,
	CfnLint:  {"APPDATA", "AWS_REGION", "AWS_DEFAULT_REGION"},
}

// MinConftest is the first Conftest release that evaluates Rego v1 by
// default (it bundles OPA 1.x).
const MinConftest = "0.60.0"

// Applies reports whether tool has anything to read: a changed path under
// an entry of its kinds, or (checkov, conftest) a recorded plan.
func Applies(tool string) func(in *packs.Input) bool {
	return func(in *packs.Input) bool {
		cfg := policy.Load(in.Settings())
		if cfg.Changed(in, KindsFor[tool]...) {
			return true
		}
		return (tool == Checkov || tool == Conftest) && policy.HasPlans(in)
	}
}

// --- checkov -----------------------------------------------------------------

// RunCheckov scans each changed source entry with `checkov -d`, and each
// recorded Terraform plan (the redacted copy) with `checkov -f
// --framework terraform_plan`.
func RunCheckov(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	entries := cfg.ChangedEntries(in, KindsFor[Checkov]...)
	root, cleanup, err := tree(in, paths(entries))
	if err != nil {
		r.fail(Checkov, "checkov did not run: "+err.Error())
		return r.done("checkov")
	}
	defer cleanup()
	common := []string{"-o", "json", "--quiet", "--compact", "--soft-fail", "--skip-download"}
	for _, e := range entries {
		out, ok := r.exec(ctx, in, Checkov, append([]string{"-d", e.Path}, common...), root, nil, exitIn(0), in.Classes())
		if !ok {
			continue
		}
		fs, err := ParseCheckov(out.Stdout, root, e.Path, "")
		if err != nil {
			r.fail(Checkov, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		r.add(fs...)
	}
	plans, err := policy.Plans(in)
	if err != nil {
		r.fail(Checkov, "cannot list the recorded plans")
	}
	for _, p := range plans {
		if p.Kind != model.KindTFPlan {
			continue
		}
		name := planName(p)
		args := append([]string{"-f", p.Path, "--framework", "terraform_plan"}, common...)
		out, ok := r.exec(ctx, in, Checkov, args, in.Root, nil, exitIn(0), in.Classes())
		if !ok {
			continue
		}
		fs, err := ParseCheckov(out.Stdout, root, "", name)
		if err != nil {
			r.fail(Checkov, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		r.add(fs...)
	}
	return r.done("checkov")
}

func planName(p policy.Plan) string {
	if p.Name != "" {
		return p.Name
	}
	return "plan:" + p.ID
}

// --- trivy -------------------------------------------------------------------

// trivyEnv keeps trivy offline: no check-bundle download, no version notice.
// Unknown variables are ignored by releases that predate them.
var trivyEnv = []string{"TRIVY_SKIP_CHECK_UPDATE=true", "TRIVY_SKIP_POLICY_UPDATE=true", "TRIVY_SKIP_VERSION_CHECK=true"}

// RunTrivy runs `trivy config --format json --exit-code 0` over each
// changed source entry.
func RunTrivy(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	entries := cfg.ChangedEntries(in, KindsFor[Trivy]...)
	root, cleanup, err := tree(in, paths(entries))
	if err != nil {
		r.fail(Trivy, "trivy did not run: "+err.Error())
		return r.done("trivy")
	}
	defer cleanup()
	for _, e := range entries {
		args := []string{"config", "--format", "json", "--exit-code", "0", "--quiet", e.Path}
		out, ok := r.exec(ctx, in, Trivy, args, root, trivyEnv, exitIn(0), in.Classes())
		if !ok {
			continue
		}
		fs, err := ParseTrivy(out.Stdout, root, e.Path)
		if err != nil {
			r.fail(Trivy, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		r.add(fs...)
	}
	return r.done("trivy")
}

// --- tflint ------------------------------------------------------------------

// RunTflint runs `tflint --format json --chdir <path>` over each changed
// Terraform/OpenTofu entry. Exit 0 (clean) and 2 (issues) are normal; the
// JSON "errors" list fails the run.
func RunTflint(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	entries := cfg.ChangedEntries(in, KindsFor[Tflint]...)
	root, cleanup, err := tree(in, paths(entries))
	if err != nil {
		r.fail(Tflint, "tflint did not run: "+err.Error())
		return r.done("tflint")
	}
	defer cleanup()
	for _, e := range entries {
		out, ok := r.exec(ctx, in, Tflint, []string{"--format", "json", "--chdir", e.Path}, root, nil, exitIn(0, 1, 2), in.Classes())
		if !ok {
			continue
		}
		fs, errs, err := ParseTflint(out.Stdout, root, e.Path)
		if err != nil {
			r.fail(Tflint, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		r.add(fs...)
		if len(errs) > 0 {
			r.fail(Tflint, "tflint reported an error in "+e.Path+": "+errs[0])
		} else if out.ExitCode == 1 {
			r.fail(Tflint, "tflint exited 1: "+out.Tail(20, in.Classes()))
		}
	}
	return r.done("tflint")
}

// --- bicep lint --------------------------------------------------------------

// bicepFiles are the changed .bicep files under bicep entries (every
// tracked one with --all).
func bicepFiles(in *packs.Input, cfg policy.Config) ([]string, error) {
	entries := cfg.ChangedEntries(in, policy.KindBicep)
	var cands []string
	if in.All {
		fs, err := in.Git.ListFiles()
		if err != nil {
			return nil, err
		}
		cands = fs
	} else {
		cands = in.ChangedPaths()
	}
	var out []string
	for _, p := range cands {
		p = policy.CleanPath(p)
		if !strings.EqualFold(path.Ext(p), ".bicep") {
			continue
		}
		for _, e := range entries {
			if policy.Under(e.Path, p) {
				out = append(out, p)
				break
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// RunBicep runs `bicep lint <file> --diagnostics-format sarif --no-restore`
// for each changed .bicep file (the standalone Bicep CLI; --no-restore keeps
// it offline). Exit 1 means it found errors.
func RunBicep(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	files, err := bicepFiles(in, cfg)
	if err != nil {
		r.fail(Bicep, "cannot list the tracked files")
		return r.done("bicep lint")
	}
	if len(files) > policy.MaxSourceFiles {
		r.fail(Bicep, fmt.Sprintf("%d .bicep files, more than %d", len(files), policy.MaxSourceFiles))
		return r.done("bicep lint")
	}
	root, cleanup, err := tree(in, paths(cfg.ChangedEntries(in, policy.KindBicep)))
	if err != nil {
		r.fail(Bicep, "bicep did not run: "+err.Error())
		return r.done("bicep lint")
	}
	defer cleanup()
	for _, f := range files {
		args := []string{"lint", f, "--diagnostics-format", "sarif", "--no-restore"}
		out, ok := r.exec(ctx, in, Bicep, args, root, nil, exitIn(0, 1), in.Classes())
		if !ok {
			continue
		}
		fs, err := ParseSARIF(out.Stdout, "bicep", root, f)
		if err != nil {
			r.fail(Bicep, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		if out.ExitCode == 1 && len(fs) == 0 {
			r.fail(Bicep, "bicep exited 1 without a diagnostic: "+out.Tail(20, in.Classes()))
		}
		r.add(fs...)
	}
	return r.done("bicep lint")
}

// --- cfn-lint ----------------------------------------------------------------

// cfnTemplates are the changed CloudFormation templates under
// cloudformation and cdk entries, plus the cdk.out templates.
func cfnTemplates(in *packs.Input, cfg policy.Config) ([]string, error) {
	files, _, err := policy.SourceFiles(in, cfg, KindsFor[CfnLint]...)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		raw, err := readNew(in, f)
		if err != nil {
			continue
		}
		if detect(f, raw) == model.KindCFN {
			out = append(out, f)
		}
	}
	cdk, _ := policy.CDKTemplates(in, cfg)
	out = append(out, cdk...)
	return out, nil
}

var detect = readers.Detect

// cfnLintOK: cfn-lint's exit code ORs 2 (error), 4 (warning) and 8
// (informational); anything else (1, 16, 32…) is a failed run.
func cfnLintOK(c int) bool { return c >= 0 && c <= 14 && c%2 == 0 }

// RunCfnLint runs `cfn-lint -f json <templates>` in batches.
func RunCfnLint(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	files, err := cfnTemplates(in, cfg)
	if err != nil {
		r.fail(CfnLint, "cfn-lint did not run: "+err.Error())
		return r.done("cfn-lint")
	}
	if len(files) == 0 {
		r.res.Status, r.res.Detail = packs.Skip, "no CloudFormation template changed"
		return r.done("cfn-lint")
	}
	root, cleanup, err := tree(in, paths(cfg.ChangedEntries(in, KindsFor[CfnLint]...)))
	if err != nil {
		r.fail(CfnLint, "cfn-lint did not run: "+err.Error())
		return r.done("cfn-lint")
	}
	defer cleanup()
	if in.Staged {
		// cdk.out is not in the index: copy the build output as it is.
		for _, f := range files {
			if strings.Contains(f, "/cdk.out/") || strings.HasPrefix(f, "cdk.out/") {
				if b, err := os.ReadFile(filepath.Join(in.Root, filepath.FromSlash(f))); err == nil {
					dst := filepath.Join(root, filepath.FromSlash(f))
					_ = os.MkdirAll(filepath.Dir(dst), 0o700)
					_ = os.WriteFile(dst, b, 0o600)
				}
			}
		}
	}
	for start := 0; start < len(files); start += Batch {
		end := min(start+Batch, len(files))
		args := append([]string{"-f", "json", "--"}, files[start:end]...)
		out, ok := r.exec(ctx, in, CfnLint, args, root, nil, cfnLintOK, in.Classes())
		if !ok {
			continue
		}
		fs, err := ParseCfnLint(out.Stdout, root, out.ExitCode)
		if err != nil {
			r.fail(CfnLint, err.Error()+": "+out.Tail(20, in.Classes()))
			continue
		}
		r.add(fs...)
	}
	return r.done("cfn-lint")
}

// --- conftest ----------------------------------------------------------------

// RunConftest runs the shipped Rego bundle (plus packs.cloud.conftest.policy)
// over the policy inputs: the redacted, normalised form of every recorded
// plan and changed template (policy.ConftestInput). One run per namespace
// and production flag.
func RunConftest(ctx context.Context, in *packs.Input) packs.Result {
	r := newResult()
	cfg := policy.Load(in.Settings())
	docs, _, err := policy.Collect(in, cfg)
	if err != nil {
		r.fail(Conftest, "conftest did not run: "+err.Error())
		return r.done("conftest")
	}
	bundle, err := policy.ExtractBundle(in.StateDir())
	if err != nil {
		r.fail(Conftest, "cannot extract the policy bundle: "+err.Error())
		return r.done("conftest")
	}
	scratch, cleanup, err := toolrun.Scratch(in.Workspace())
	if err != nil {
		r.fail(Conftest, "no scratch directory: "+err.Error())
		return r.done("conftest")
	}
	defer cleanup()

	type group struct {
		ns   string
		prod bool
	}
	byGroup := map[group][]string{}
	where := map[string]*policy.Doc{}
	for i := range docs {
		ns, body, err := policy.ConftestInput(docs[i])
		if err != nil {
			r.fail(Conftest, "cannot build the input of "+docs[i].Name)
			continue
		}
		if ns == "" {
			continue
		}
		name := fmt.Sprintf("in%04d.json", i)
		if err := os.WriteFile(filepath.Join(scratch, name), body, 0o600); err != nil {
			r.fail(Conftest, "cannot write a conftest input: "+err.Error())
			return r.done("conftest")
		}
		where[name] = &docs[i]
		g := group{ns, docs[i].Prod()}
		byGroup[g] = append(byGroup[g], name)
	}
	if len(byGroup) == 0 {
		r.res.Status, r.res.Detail = packs.Skip, "no plan or template for the bundle to judge"
		return r.done("conftest")
	}
	var project []string
	for _, p := range cfg.Conftest.Policy {
		project = append(project, filepath.Join(in.Root, filepath.FromSlash(policy.CleanPath(p))))
	}
	loc := func(filename, addr string) (string, int) {
		d := where[filepath.Base(filepath.FromSlash(filename))]
		if d == nil {
			return filepath.ToSlash(filename), 0
		}
		for _, res := range d.Resources {
			if addr != "" && res.Addr == addr {
				return res.Pos.File, res.Pos.Line
			}
		}
		return d.Name, 0
	}
	groups := make([]group, 0, len(byGroup))
	for g := range byGroup {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].ns != groups[j].ns {
			return groups[i].ns < groups[j].ns
		}
		return !groups[i].prod && groups[j].prod
	})
	for gi, g := range groups {
		dataDir := filepath.Join(scratch, fmt.Sprintf("data%d", gi))
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			r.fail(Conftest, err.Error())
			continue
		}
		tags := cfg.Tags
		if tags == nil {
			tags = []string{}
		}
		data, _ := json.Marshal(map[string]any{"tqconfig": map[string]any{"tags": tags, "prod": g.prod}})
		if err := os.WriteFile(filepath.Join(dataDir, "tqconfig.json"), data, 0o600); err != nil {
			r.fail(Conftest, err.Error())
			continue
		}
		args := []string{"test", "--output", "json", "--no-fail", "--policy", bundle}
		for _, p := range project {
			args = append(args, "--policy", p)
		}
		args = append(args, "--data", dataDir, "--namespace", g.ns)
		if len(project) > 0 {
			args = append(args, "--namespace", "main")
		}
		names := byGroup[g]
		for start := 0; start < len(names); start += Batch {
			end := min(start+Batch, len(names))
			full := append(append([]string{}, args...), names[start:end]...)
			out, ok := r.exec(ctx, in, Conftest, full, scratch, nil, exitIn(0), in.Classes())
			if !ok {
				continue
			}
			fs, err := ParseConftest(out.Stdout, loc)
			if err != nil {
				r.fail(Conftest, err.Error()+": "+out.Tail(20, in.Classes()))
				continue
			}
			r.add(fs...)
		}
	}
	return r.done("conftest")
}
