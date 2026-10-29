package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/approval"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/gate"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/model"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/readers"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
	"github.com/tentaqles/tentaqles/internal/redact"
)

// DefaultPlanTimeout bounds tq pack cloud plan's command (§7.1).
const DefaultPlanTimeout = 10 * time.Minute

// stsTimeout bounds aws sts get-caller-identity.
const stsTimeout = 30 * time.Second

// now is the clock (tests replace it).
var now = func() time.Time { return time.Now().UTC() }

func commands() []*cobra.Command {
	return []*cobra.Command{newPlanCmd(), newShowCmd(), newApproveCmd(), newPruneCmd()}
}

// cmdContext is the run context of a cloud subcommand inside a workspace
// with the pack enabled.
func cmdContext(c *cobra.Command) (*packs.Input, string, error) {
	in, err := packs.FromCommand(c)
	if err != nil {
		return nil, "", err
	}
	if in.Workspace() == "" {
		return nil, "", errors.New("tq pack cloud needs a trusted workspace (run it inside one)")
	}
	if !in.Config.IsEnabled("cloud") {
		return nil, "", errors.New("the cloud pack is not enabled in this workspace (packs.cloud in .tentaqles.yaml)")
	}
	sd := in.StateDir()
	if sd == "" {
		return nil, "", errors.New("the cloud pack has no state directory for this workspace")
	}
	return in, sd, nil
}

// localHost reads small local files for the plan command (not the guard):
// relative to the repo root, never a .env file.
func localHost(root string) gate.Host {
	home, _ := os.UserHomeDir()
	return gate.Host{Getenv: os.Getenv, Home: home, ReadLocal: func(p string, max int64) ([]byte, error) {
		for _, seg := range strings.Split(strings.ReplaceAll(p, `\`, "/"), "/") {
			if strings.HasPrefix(strings.ToLower(seg), ".env") {
				return nil, fmt.Errorf("%s: .env files are never read", p)
			}
			if seg == ".." {
				return nil, fmt.Errorf("%s: leaves the repository", p)
			}
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, filepath.FromSlash(p))
		}
		fi, err := os.Stat(p)
		if err != nil {
			return nil, err
		}
		if !fi.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", p)
		}
		if max > 0 && fi.Size() > max {
			return nil, fmt.Errorf("%s is over %d bytes", p, max)
		}
		return os.ReadFile(p)
	}}
}

// ---- the plan tools' environment ---------------------------------------

// planCommon are the names every plan tool gets beyond toolrun.BaseEnv:
// config and data folders, the Windows system folders the CLIs' runtimes
// read, and proxy and CA settings.
var planCommon = []string{"APPDATA", "LOCALAPPDATA", "ProgramData", "ProgramFiles", "ProgramFiles(x86)", "CommonProgramFiles",
	"SystemDrive", "windir", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_RUNTIME_DIR",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR",
	"REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"}

// planPrefixes are the variable families a plan tool reads, per tool: its
// own (TF_*, PULUMI_*, CDK_*), the providers' credentials and settings
// (ARM_*, AZURE_*, AWS_*, GOOGLE_*, CLOUDSDK_*) and, for the tools that run
// the user's program (cdk synth, pulumi preview), the language runtimes.
// Credentials pass because the user runs the plan with them on purpose
// (toolrun still withholds secret-looking names from a repo-local binary).
var planPrefixes = map[string][]string{
	"terraform": {"TF_", "ARM_", "AZURE_", "AWS_", "GOOGLE_", "CLOUDSDK_", "MSI_", "IDENTITY_"},
	"az":        {"AZURE_", "MSI_", "IDENTITY_"},
	"aws":       {"AWS_"},
	"cdk":       {"CDK_", "JSII_", "AWS_", "NODE_", "NPM_CONFIG_", "PYTHON", "VIRTUAL_ENV", "GO", "JAVA_", "DOTNET_"},
	"pulumi":    {"PULUMI_", "ARM_", "AZURE_", "AWS_", "GOOGLE_", "CLOUDSDK_", "MSI_", "IDENTITY_", "NODE_", "NPM_CONFIG_", "PYTHON", "VIRTUAL_ENV", "GO", "JAVA_", "DOTNET_"},
}

// PlanPassEnv names the parent variables a plan tool may see (round 5's
// filtered tool environment): planCommon plus the parent's variables in
// the tool's families. Only names are read here, never values.
func PlanPassEnv(tool string) []string {
	fam := tool
	switch tool {
	case "tofu":
		fam = "terraform"
	case "cdktf":
		fam = "cdk"
	}
	out := append([]string(nil), planCommon...)
	pre := planPrefixes[fam]
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		u := strings.ToUpper(k)
		for _, p := range pre {
			if strings.HasPrefix(u, p) {
				out = append(out, k)
				break
			}
		}
	}
	return out
}

// ---- plan --------------------------------------------------------------

func newPlanCmd() *cobra.Command {
	var env string
	var timeout time.Duration
	var asJSON bool
	c := &cobra.Command{
		Use:   "plan --env E [--timeout 10m] -- <plan command>",
		Short: "Run a plan and record it for the apply gate (prints the summary only)",
		Long: `Runs the plan command (terraform/tofu plan, az deployment … what-if,
cdk synth, aws cloudformation create-change-set, pulumi preview) with the
environment and identity checks, stores the plan under the cloud pack's state
directory (0600, outside the repository) with a redacted JSON copy, and
appends a plan record. It prints the summary and the apply form the gate
accepts, never the plan.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if env == "" {
				return errors.New("--env is required")
			}
			if timeout <= 0 || timeout > toolrun.MaxTimeout {
				return fmt.Errorf("--timeout must be between 1s and %s", toolrun.MaxTimeout)
			}
			in, sd, err := cmdContext(c)
			if err != nil {
				return err
			}
			r, err := recordPlan(c.Context(), in, sd, env, timeout, args)
			if err != nil {
				return err
			}
			return printRecord(c.OutOrStdout(), r, asJSON, sd)
		},
	}
	c.Flags().StringVar(&env, "env", "", "the environment the plan is for (packs.cloud.environments)")
	c.Flags().DurationVar(&timeout, "timeout", DefaultPlanTimeout, "how long the plan command may run")
	c.Flags().BoolVar(&asJSON, "json", false, "print the record as JSON")
	c.Flags().SetInterspersed(false)
	return c
}

func cwdRel(root string) string {
	wd, err := os.Getwd()
	if err != nil {
		return "?"
	}
	r, err := filepath.Rel(root, wd)
	if err != nil || strings.HasPrefix(r, "..") {
		return "?"
	}
	r = filepath.ToSlash(r)
	if r == "." {
		return ""
	}
	return r
}

// planTarget finds the one tool invocation of a plan argv.
func planTarget(argv []string) (gate.Invocation, error) {
	var found []gate.Invocation
	for _, iv := range gate.AnalyzeArgv(argv) {
		if iv.Tool != "" {
			found = append(found, iv)
		}
		if iv.Opaque {
			return iv, errors.New("tq cannot read this command's wrapper; run the plan tool directly (tq dotenv run -- is fine)")
		}
	}
	if len(found) != 1 {
		return gate.Invocation{}, errors.New("the plan command must be one terraform, tofu, az, cdk, aws or pulumi command")
	}
	iv := found[0]
	ok := false
	switch iv.Tool {
	case "terraform", "tofu":
		ok = len(iv.Verb) == 1 && iv.Verb[0] == "plan"
	case "az":
		ok = len(iv.Verb) == 3 && iv.Verb[0] == "deployment" && iv.Verb[2] == "what-if"
	case "cdk":
		ok = len(iv.Verb) == 1 && (iv.Verb[0] == "synth" || iv.Verb[0] == "synthesize")
	case "aws":
		ok = len(iv.Verb) == 2 && iv.Verb[0] == "cloudformation" && iv.Verb[1] == "create-change-set"
	case "pulumi":
		ok = len(iv.Verb) == 1 && (iv.Verb[0] == "preview" || iv.Verb[0] == "pre")
	}
	if !ok {
		return iv, fmt.Errorf("%s %s is not a plan form: use terraform/tofu plan, az deployment <scope> what-if, cdk synth, aws cloudformation create-change-set or pulumi preview", iv.Tool, strings.Join(iv.Verb, " "))
	}
	return iv, nil
}

// planRun is one recording in progress.
type planRun struct {
	ctx     context.Context
	in      *packs.Input
	sd      string
	env     string
	timeout time.Duration
	argv    []string
	iv      gate.Invocation
	host    gate.Host
	rec     plans.Record
	files   []string // store files written (removed on failure)
	inputs  []string // repo paths the inputs hash covers
	raw     []byte   // the JSON the summary is read from
	kind    model.Kind
}

func (p *planRun) fail(err error) error {
	for _, f := range p.files {
		os.Remove(filepath.Join(p.sd, filepath.FromSlash(f)))
	}
	return err
}

// run executes argv (the tool at argv[toolAt]; a tq dotenv wrapper in front
// runs through tq itself).
func (p *planRun) run(argv []string, toolAt int, timeout time.Duration) (toolrun.Output, error) {
	tool := gate.ToolName(argv[toolAt])
	args := argv[toolAt+1:]
	if toolAt > 0 {
		tool, args = gate.ToolName(argv[0]), argv[1:]
	}
	wd, _ := os.Getwd()
	out, err := p.in.Tools.Run(p.ctx, toolrun.Spec{Pack: "cloud", Tool: tool, Args: args, Dir: wd, Timeout: timeout, PassEnv: PlanPassEnv(tool)})
	if err != nil {
		return out, fmt.Errorf("%s: %w", tool, err)
	}
	if out.TimedOut {
		return out, fmt.Errorf("%s did not finish within %s (the process tree was stopped)", tool, timeout)
	}
	return out, nil
}

func (p *planRun) tail(out toolrun.Output) string {
	return out.Tail(20, p.in.Classes())
}

func recordPlan(ctx context.Context, in *packs.Input, sd, env string, timeout time.Duration, argv []string) (plans.Record, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	iv, err := planTarget(argv)
	if err != nil {
		return plans.Record{}, err
	}
	gt := envgate.FromManifest(envgate.Cloud, in.Manifest, in.Config)
	if !gt.Declared(env) {
		return plans.Record{}, fmt.Errorf("environment %q is not declared (packs.cloud.environments: %s)", env, strings.Join(gt.Envs(), ", "))
	}
	if d := cwdRel(in.Root); iv.Dir == "" {
		iv.Dir = d
	}
	host := localHost(in.Root)
	// The weak Pulumi listing uses the guard's own lister, so plan time and
	// apply time see the same names (the same roots and refusals).
	host.ListLocal = packs.NewGuardCtx(packs.GuardEnv{Config: in.Config, Manifest: in.Manifest, Workspace: in.Workspace(),
		WorkspaceRoot: in.WorkspaceRoot(), RepoRoot: in.Root}, "cloud").ListLocal
	action := envgate.Apply
	if res := gt.Resolve(action, gate.Signals(iv, host)); res.Resolved && res.Env != env {
		return plans.Record{}, fmt.Errorf("the command targets environment %s (%s %s), not %s", res.Env, res.By.Key, res.By.Value, env)
	}
	ident := gate.ResolveIdentity(iv, host, len(gt.Match(env)["subscriptions"]) > 0)
	if r := gate.Drift(gt, env, ident, "", in.Manifest); r != "" {
		return plans.Record{}, fmt.Errorf("identity drift: %s; switch the identity (tq doctor --verify) before planning", r)
	}
	ts := now()
	p := &planRun{ctx: ctx, in: in, sd: sd, env: env, timeout: timeout, argv: append([]string(nil), argv...), iv: iv, host: host}
	p.rec = plans.Record{ID: plans.NewID(ts), Env: env, Tool: iv.Tool, Identity: ident.String(), TS: ts.Format(time.RFC3339)}
	if _, err := plans.EnsureDir(sd); err != nil {
		return p.rec, err
	}
	// The AWS account, resolved here (a network call is fine in a command,
	// never in the hook).
	if iv.Tool == "aws" || iv.Tool == "cdk" || iv.Tool == "cdktf" || len(gt.Match(env)["accounts"]) > 0 {
		acct, err := p.account(ident)
		if err != nil && len(gt.Match(env)["accounts"]) > 0 {
			return p.rec, fmt.Errorf("cannot resolve the AWS account (aws sts get-caller-identity): %v", err)
		}
		p.rec.Account = acct
		if r := gate.Drift(gt, env, ident, acct, in.Manifest); r != "" {
			return p.rec, fmt.Errorf("identity drift: %s", r)
		}
	}
	switch iv.Tool {
	case "terraform", "tofu":
		err = p.terraform()
	case "az":
		err = p.az()
	case "cdk":
		err = p.cdk()
	case "aws":
		err = p.changeSet()
	case "pulumi":
		err = p.pulumi()
	}
	if err != nil {
		return p.rec, p.fail(err)
	}
	if err := p.finish(); err != nil {
		return p.rec, p.fail(err)
	}
	return p.rec, nil
}

func (p *planRun) account(ident gate.Identity) (string, error) {
	args := []string{"sts", "get-caller-identity", "--output", "json"}
	if ident.Profile != "" {
		args = append(args, "--profile", ident.Profile)
	}
	out, err := p.in.Tools.Run(p.ctx, toolrun.Spec{Pack: "cloud", Tool: "aws", Args: args, Dir: p.in.Root, Timeout: stsTimeout, PassEnv: PlanPassEnv("aws")})
	if err != nil {
		return "", err
	}
	if out.TimedOut || out.ExitCode != 0 {
		return "", errors.New("aws sts get-caller-identity failed")
	}
	var v struct {
		Account string `json:"Account"`
	}
	if json.Unmarshal(out.Stdout, &v) != nil || v.Account == "" {
		return "", errors.New("aws sts get-caller-identity printed no account")
	}
	return v.Account, nil
}

// abs is a store name's absolute path, with forward slashes (it works in
// bash, PowerShell and the tools on every OS).
func (p *planRun) abs(name string) string {
	return filepath.ToSlash(filepath.Join(p.sd, filepath.FromSlash(name)))
}

func (p *planRun) store(name string, data []byte) error {
	if err := plans.WriteFile(p.sd, name, data); err != nil {
		return err
	}
	p.files = append(p.files, name)
	return nil
}

func (p *planRun) hashStore(name string) error {
	fp := filepath.Join(p.sd, filepath.FromSlash(name))
	b, err := os.ReadFile(fp)
	if err != nil {
		return fmt.Errorf("the plan was not written: %w", err)
	}
	fi, _ := os.Stat(fp)
	_ = os.Chmod(fp, 0o600)
	p.rec.PlanFile = name
	p.rec.PlanSHA256 = packs.HashBytes(b)
	p.rec.PlanSize = fi.Size()
	p.rec.PlanMTime = fi.ModTime().UTC().Format(time.RFC3339Nano)
	return nil
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

func relJoin(dir, p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if dir == "" || dir == "." || path.IsAbs(p) || (len(p) > 1 && p[1] == ':') {
		return path.Clean(p)
	}
	return path.Clean(path.Join(dir, p))
}

func (p *planRun) terraform() error {
	iv := p.iv
	at := iv.ToolAt
	argv := append([]string(nil), p.argv...)
	name := plans.PlanName(p.rec.ID, "tfplan")
	out := "-out=" + p.abs(name)
	// rewrite -out=- (or any -out) to the store, or inject it
	replaced := false
	for i := at + 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-out" || a == "--out":
			if i+1 < len(argv) {
				argv = append(argv[:i], argv[i+1:]...)
			}
			argv[i] = out
			replaced = true
		case strings.HasPrefix(a, "-out=") || strings.HasPrefix(a, "--out="):
			argv[i] = out
			replaced = true
		}
	}
	if !replaced {
		argv = append(argv, out)
	}
	p.rec.Destroy = iv.Has("destroy")
	dir := gate.WorkDir(iv)
	if dir == "?" {
		return errors.New("the plan's folder is unknown; run it from the repository")
	}
	p.rec.Dir = dir
	p.rec.Subject = plans.Subject(dir)
	for _, vf := range iv.FlagAll("var-file") {
		p.rec.VarFiles = append(p.rec.VarFiles, relJoin(dir, vf))
	}
	p.inputs = append([]string{dir}, p.rec.VarFiles...)
	p.rec.Workspace = strings.TrimSpace(p.host.Getenv("TF_WORKSPACE"))
	if v := iv.Env["TF_WORKSPACE"]; v != "" {
		p.rec.Workspace = v
	}
	if p.rec.Workspace == "" {
		if b, err := p.host.ReadLocal(path.Join(dir, ".terraform", "environment"), 64<<10); err == nil {
			p.rec.Workspace = strings.TrimSpace(string(b))
		}
	}
	p.files = append(p.files, name)
	res, err := p.run(argv, at, p.timeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 && !(res.ExitCode == 2 && hasFlag(argv, "-detailed-exitcode")) {
		return fmt.Errorf("%s plan failed (exit %d):\n%s", iv.Tool, res.ExitCode, p.tail(res))
	}
	if err := p.hashStore(name); err != nil {
		return err
	}
	// terraform show -json <plan>, with the same -chdir
	show := []string{iv.Tool}
	if c, ok := iv.Flag("chdir"); ok {
		show = append(show, "-chdir="+c)
	}
	show = append(show, "show", "-json", p.abs(name))
	if at > 0 {
		show = append(append([]string(nil), p.argv[:at]...), show...)
	}
	sj, err := p.run(show, at, p.timeout)
	if err != nil {
		return err
	}
	if sj.ExitCode != 0 {
		return fmt.Errorf("%s show -json failed (exit %d):\n%s", iv.Tool, sj.ExitCode, p.tail(sj))
	}
	if sj.StdoutTruncated {
		return fmt.Errorf("the plan JSON is over %d bytes, more than tq can read; split the stack", toolrun.MaxOutput)
	}
	p.raw, p.kind = sj.Stdout, model.KindTFPlan
	p.rec.Apply = []string{strings.TrimSpace(iv.Tool + " " + chdirArg(iv) + "apply " + p.abs(name))}
	return nil
}

func chdirArg(iv gate.Invocation) string {
	if c, ok := iv.Flag("chdir"); ok {
		return "-chdir=" + c + " "
	}
	return ""
}

func (p *planRun) az() error {
	iv := p.iv
	argv := append([]string(nil), p.argv...)
	if !hasFlag(argv, "--result-format", "-r") {
		argv = append(argv, "--result-format", "FullResourcePayloads")
	}
	if !hasFlag(argv, "--no-pretty-print") {
		argv = append(argv, "--no-pretty-print")
	}
	if !hasFlag(argv, "--output", "-o") {
		argv = append(argv, "--output", "json")
	}
	p.rec.Scope = gate.AzScope(iv)
	if strings.HasSuffix(p.rec.Scope, ":") {
		return errors.New("the what-if names no resource group or management group")
	}
	t, prm, err := gate.AzBinding(iv, p.host)
	if err != nil {
		return err
	}
	p.rec.TemplateSH, p.rec.ParamsSHA = t, prm
	tf, _ := iv.Flag("template-file", "f")
	p.rec.Template = relJoin(iv.Dir, tf)
	p.rec.Dir = path.Dir(p.rec.Template)
	p.rec.Subject = plans.Subject(p.rec.Template)
	p.inputs = []string{p.rec.Dir}
	for _, prmf := range iv.FlagAll("parameters", "p") {
		f := strings.TrimPrefix(prmf, "@")
		if strings.HasPrefix(prmf, "@") || strings.HasSuffix(f, ".json") || strings.HasSuffix(f, ".bicepparam") {
			p.inputs = append(p.inputs, relJoin(iv.Dir, f))
		}
	}
	if m, _ := iv.Flag("mode"); strings.EqualFold(m, "complete") {
		p.rec.Destroy = true
	}
	res, err := p.run(argv, iv.ToolAt, p.timeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("az what-if failed (exit %d):\n%s", res.ExitCode, p.tail(res))
	}
	if res.StdoutTruncated {
		return fmt.Errorf("the what-if JSON is over %d bytes; split the deployment", toolrun.MaxOutput)
	}
	name := plans.PlanName(p.rec.ID, "whatif.json")
	if err := p.store(name, res.Stdout); err != nil {
		return err
	}
	if err := p.hashStore(name); err != nil {
		return err
	}
	p.raw, p.kind = res.Stdout, model.KindWhatIf
	create := append([]string(nil), p.argv[:iv.ToolAt]...)
	create = append(create, iv.Words[iv.ToolAt].Text)
	for i := iv.ToolAt + 1; i < len(p.argv); i++ {
		a := p.argv[i]
		switch {
		case a == "what-if":
			create = append(create, "create")
		case a == "--result-format" || a == "-r" || a == "--exclude-change-types" || a == "-x":
			i++
		case a == "--no-pretty-print":
		default:
			create = append(create, a)
		}
	}
	p.rec.Apply = []string{strings.Join(create, " ")}
	return nil
}

func (p *planRun) cdk() error {
	iv := p.iv
	dir := gate.WorkDir(iv)
	if dir == "?" {
		return errors.New("the synth's folder is unknown; run it from the repository")
	}
	res, err := p.run(p.argv, iv.ToolAt, p.timeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("cdk synth failed (exit %d):\n%s", res.ExitCode, p.tail(res))
	}
	out, ok := iv.Flag("output", "o")
	if !ok || out == "" {
		out = "cdk.out"
	}
	app := relJoin(dir, out)
	p.rec.App, p.rec.Dir, p.rec.Subject = app, dir, plans.Subject(app)
	p.inputs = []string{dir}
	man, err := p.host.ReadLocal(path.Join(app, "manifest.json"), 1<<20)
	if err != nil {
		return fmt.Errorf("the cloud assembly %s has no readable manifest.json: %v", app, err)
	}
	stacks, err := gate.CDKManifest(man)
	if err != nil {
		return err
	}
	if len(stacks) == 0 {
		return fmt.Errorf("the cloud assembly %s holds no stack", app)
	}
	var all []model.Resource
	for i := range stacks {
		h, err := gate.CDKStackHash(p.host, app, stacks[i])
		if err != nil {
			return err
		}
		stacks[i].Hash = h
		tpl, err := p.host.ReadLocal(path.Join(app, stacks[i].Template), 64<<20)
		if err != nil {
			return err
		}
		rs, err := readers.CFN(path.Join(app, stacks[i].Template), tpl, readers.Options{Env: p.env})
		if err != nil {
			return fmt.Errorf("%s: %w", stacks[i].Template, err)
		}
		all = append(all, rs...)
		red, err := readers.Redact(model.KindCFN, tpl)
		if err != nil {
			return err
		}
		rn := plans.Dir + "/" + p.rec.ID + ".stack-" + fmt.Sprint(i) + ".redacted.json"
		if err := p.store(rn, red); err != nil {
			return err
		}
		p.rec.Redacted = append(p.rec.Redacted, rn)
		p.rec.Apply = append(p.rec.Apply, "cdk deploy --app "+app+" "+stacks[i].Name)
	}
	p.rec.Stacks = stacks
	snap, _ := json.MarshalIndent(stacks, "", "  ")
	name := plans.PlanName(p.rec.ID, "cdk.json")
	if err := p.store(name, append(snap, '\n')); err != nil {
		return err
	}
	if err := p.hashStore(name); err != nil {
		return err
	}
	p.rec.Kind = string(model.KindCFN)
	p.rec.Summary = model.Summarize(all)
	return nil
}

func (p *planRun) changeSet() error {
	iv := p.iv
	argv := append([]string(nil), p.argv...)
	if !hasFlag(argv, "--output") {
		argv = append(argv, "--output", "json")
	}
	stack, _ := iv.Flag("stack-name")
	if stack == "" {
		return errors.New("create-change-set needs --stack-name")
	}
	p.rec.Stack, p.rec.Subject = stack, plans.Subject(stack)
	p.rec.Dir = gate.WorkDir(iv)
	if body, ok := iv.Flag("template-body"); ok && strings.HasPrefix(body, "file://") {
		t := relJoin(iv.Dir, strings.TrimPrefix(body, "file://"))
		h, err := gate.HashLocal(p.host, t)
		if err != nil {
			return err
		}
		p.rec.Template, p.rec.TemplateSH = t, h
		p.rec.Dir = path.Dir(t)
	}
	if p.rec.Dir == "?" {
		return errors.New("the command's folder is unknown; run it from the repository")
	}
	p.inputs = []string{p.rec.Dir}
	res, err := p.run(argv, iv.ToolAt, p.timeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("create-change-set failed (exit %d):\n%s", res.ExitCode, p.tail(res))
	}
	var created struct {
		ID string `json:"Id"`
	}
	if json.Unmarshal(res.Stdout, &created) != nil || !strings.Contains(created.ID, ":changeSet/") {
		return errors.New("create-change-set printed no change-set ARN")
	}
	p.rec.ChangeSet = created.ID
	common := []string{}
	for _, f := range []string{"profile", "region"} {
		if v, ok := iv.Flag(f); ok && v != "" {
			common = append(common, "--"+f, v)
		}
	}
	pre := append([]string(nil), p.argv[:iv.ToolAt+1]...)
	wait := append(append(append([]string(nil), pre...), "cloudformation", "wait", "change-set-create-complete", "--change-set-name", created.ID), common...)
	if w, err := p.run(wait, iv.ToolAt, p.timeout); err != nil {
		return err
	} else if w.ExitCode != 0 {
		return fmt.Errorf("the change set did not become ready (exit %d):\n%s", w.ExitCode, p.tail(w))
	}
	desc := append(append(append([]string(nil), pre...), "cloudformation", "describe-change-set", "--change-set-name", created.ID, "--include-property-values", "--output", "json"), common...)
	d, err := p.run(desc, iv.ToolAt, p.timeout)
	if err != nil {
		return err
	}
	if d.ExitCode != 0 || d.StdoutTruncated {
		return fmt.Errorf("describe-change-set failed or printed over %d bytes:\n%s", toolrun.MaxOutput, p.tail(d))
	}
	name := plans.PlanName(p.rec.ID, "changeset.json")
	if err := p.store(name, d.Stdout); err != nil {
		return err
	}
	if err := p.hashStore(name); err != nil {
		return err
	}
	p.raw, p.kind = d.Stdout, model.KindChangeSet
	ex := append(append([]string(nil), pre...), "cloudformation", "execute-change-set", "--change-set-name", created.ID)
	p.rec.Apply = []string{strings.Join(append(ex, common...), " ")}
	return nil
}

func (p *planRun) pulumi() error {
	iv := p.iv
	if iv.Has("show-secrets") {
		return errors.New("--show-secrets writes secrets in clear; tq never records it")
	}
	stack, _ := iv.Flag("stack", "s")
	if stack == "" {
		return errors.New("name the stack with --stack (tq does not read Pulumi's selected stack)")
	}
	dir := gate.WorkDir(iv)
	if dir == "?" {
		return errors.New("the preview's folder is unknown; run it from the repository (with --cwd)")
	}
	p.rec.Stack, p.rec.Dir, p.rec.Subject = stack, dir, plans.Subject(stack)
	p.inputs = []string{dir}
	argv := append([]string(nil), p.argv...)
	if !hasFlag(argv, "--json", "-j") {
		argv = append(argv, "--json")
	}
	planName := ""
	for i := iv.ToolAt + 1; i < len(argv); i++ {
		a := argv[i]
		if a == "--save-plan" && i+1 < len(argv) {
			planName = plans.PlanName(p.rec.ID, "pulumi-plan.json")
			argv[i+1] = p.abs(planName)
		} else if strings.HasPrefix(a, "--save-plan=") {
			planName = plans.PlanName(p.rec.ID, "pulumi-plan.json")
			argv[i] = "--save-plan=" + p.abs(planName)
		}
	}
	if planName != "" {
		p.files = append(p.files, planName)
	}
	res, err := p.run(argv, iv.ToolAt, p.timeout)
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("pulumi preview failed (exit %d):\n%s", res.ExitCode, p.tail(res))
	}
	if res.StdoutTruncated {
		return fmt.Errorf("the preview JSON is over %d bytes; split the stack", toolrun.MaxOutput)
	}
	cwd := ""
	if dir != "." {
		cwd = " --cwd " + dir
	}
	if planName != "" {
		if err := p.hashStore(planName); err != nil {
			return err
		}
		p.rec.Apply = []string{"pulumi up" + cwd + " --stack " + stack + " --plan " + p.abs(planName)}
	} else {
		name := plans.PlanName(p.rec.ID, "preview.json")
		if err := p.store(name, res.Stdout); err != nil {
			return err
		}
		if err := p.hashStore(name); err != nil {
			return err
		}
		p.rec.Weak = true
		proj, cfg, err := gate.PulumiBinding(p.host, dir, stack)
		if err != nil {
			return err
		}
		if proj == plans.None {
			return fmt.Errorf("no Pulumi.yaml in %s", dir)
		}
		p.rec.Project, p.rec.StackCfg = proj, cfg
		// §9.5 "an unchanged tree": the program files, re-hashed by the guard
		tree, err := plans.TreeFiles(p.ctx, p.in.Root, dir)
		if err != nil {
			return err
		}
		ts, err := gate.TreeHash(p.host, tree)
		if err != nil {
			return err
		}
		ls, err := gate.ListingHash(p.host, dir, tree)
		if err != nil {
			return err
		}
		p.rec.Tree, p.rec.TreeSHA, p.rec.ListingSHA = tree, ts, ls
		p.rec.Apply = []string{"pulumi up" + cwd + " --stack " + stack + " --yes"}
	}
	p.raw, p.kind = res.Stdout, model.KindPulumi
	return nil
}

// finish reads the summary, writes the redacted copy, hashes the inputs,
// runs the built-in cloud checks over the plan's folder and saves the
// record.
func (p *planRun) finish() error {
	if p.raw != nil {
		rs, err := readers.Read(p.kind, p.rec.PlanFile, p.raw, readers.Options{Env: p.env})
		if err != nil {
			return fmt.Errorf("the plan output cannot be read (%v); it is not recorded", err)
		}
		p.rec.Summary = model.Summarize(rs)
		p.rec.Kind = string(p.kind)
		red, err := readers.Redact(p.kind, p.raw)
		if err != nil {
			return fmt.Errorf("the plan cannot be redacted (%v); it is not recorded", err)
		}
		rn := plans.RedactedName(p.rec.ID)
		if err := p.store(rn, red); err != nil {
			return err
		}
		p.rec.Redacted = []string{rn}
	}
	root := p.in.Root
	h, err := plans.InputsHash(p.ctx, root, p.inputs)
	if err != nil {
		return err
	}
	p.rec.InputsHash, p.rec.Inputs = h, p.inputs
	p.rec.Commit = plans.Head(p.ctx, root)
	p.rec.Clean, _ = plans.Clean(p.ctx, root, p.inputs)
	if err := plans.Save(p.sd, p.rec); err != nil {
		return err
	}
	p.files = append(p.files, plans.RecordName(p.rec.ID))
	p.rec.Checks = p.checks()
	if err := plans.Save(p.sd, p.rec); err != nil {
		return err
	}
	return plans.Append(p.sd, p.rec)
}

// checks runs the built-in cloud checks over the plan's folder.
func (p *planRun) checks() []plans.Check {
	out := []plans.Check{}
	paths := []string{p.rec.Dir}
	changed, base, err := packs.Select(p.ctx, p.in.Root, packs.Selection{Paths: paths})
	if err != nil {
		return out
	}
	in := *p.in
	in.Changed, in.Base = changed, base
	in.Budget = time.Now().Add(2 * time.Minute)
	rep := packs.RunChecks(p.ctx, &in, packs.RunOptions{Packs: []string{"cloud"}, BuiltinOnly: true})
	for _, r := range rep.Results {
		if r.ID == planFreshID {
			continue // a plan is being recorded right now
		}
		e, w := r.Counts()
		out = append(out, plans.Check{ID: r.ID, Status: string(r.Status), Required: r.Required, Errors: e, Warns: w})
	}
	return out
}

// ---- show --------------------------------------------------------------

func printRecord(w io.Writer, r plans.Record, asJSON bool, sd string) error {
	if asJSON {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	}
	fmt.Fprintf(w, "plan %s  env %s  %s  %s\n", r.ID, r.Env, r.Tool, r.Subject)
	fmt.Fprintf(w, "  %s\n", redact.Redact(r.Summary.Text(), []string{"all"}))
	if r.Destroys() {
		fmt.Fprintln(w, "  this plan destroys: approve it with --destroy")
	}
	if r.Weak {
		fmt.Fprintf(w, "  binding: the weaker Pulumi binding (preview hash, Pulumi.yaml, the stack config and the %d recorded program files and the names in their folders; dependencies, and files in sub-folders that hold no recorded file, are not re-checked at apply)\n", len(r.Tree))
	}
	fmt.Fprintf(w, "  identity %s", r.Identity)
	if r.Account != "" {
		fmt.Fprintf(w, ", account %s", r.Account)
	}
	fmt.Fprintln(w)
	if len(r.Checks) > 0 {
		var parts []string
		for _, c := range r.Checks {
			parts = append(parts, fmt.Sprintf("%s %s", c.Status, c.ID))
		}
		fmt.Fprintf(w, "  checks: %s\n", strings.Join(parts, ", "))
	}
	for _, s := range r.Stacks {
		fmt.Fprintf(w, "  stack %s\n", s.Name)
	}
	if r.ChangeSet != "" {
		fmt.Fprintf(w, "  change set %s\n", r.ChangeSet)
	}
	fmt.Fprintf(w, "next: tq pack cloud approve %s (environments that need approval)\n", r.ID)
	for _, a := range r.Apply {
		fmt.Fprintf(w, "apply with: %s\n", a)
	}
	return nil
}

func newShowCmd() *cobra.Command {
	var asJSON bool
	c := &cobra.Command{
		Use:   "show <plan-id> [--json]",
		Short: "Show a recorded plan's redacted summary and check results (never the plan)",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			_, sd, err := cmdContext(c)
			if err != nil {
				return err
			}
			r, err := plans.Load(sd, args[0])
			if err != nil {
				return err
			}
			return printRecord(c.OutOrStdout(), r, asJSON, sd)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "print the record as JSON")
	return c
}

// ---- approve -----------------------------------------------------------

func newApproveCmd() *cobra.Command {
	var destroy, allowFailing bool
	var stack string
	c := &cobra.Command{
		Use:   "approve <plan-id> [--destroy] [--stack S]",
		Short: "Ask a human to approve a recorded plan (agents may ask; only a human answers)",
		Args:  cobra.ExactArgs(1),
		// It only asks (approval.Ask); the human answers in their own terminal.
		Annotations: map[string]string{packs.AnnotationHumanOnly: packs.ApprovalAsk},
		RunE: func(c *cobra.Command, args []string) error {
			in, sd, err := cmdContext(c)
			if err != nil {
				return err
			}
			if allowFailing {
				if err := packs.HumanOnly(); err != nil {
					return fmt.Errorf("--allow-failing: %w", err)
				}
			}
			return askApproval(c.Context(), c.OutOrStdout(), in, sd, args[0], destroy, allowFailing, stack)
		},
	}
	c.Flags().BoolVar(&destroy, "destroy", false, "the plan destroys (a destroy plan, or a stateful delete or replace)")
	c.Flags().StringVar(&stack, "stack", "", "the CDK stack (one approval per stack)")
	c.Flags().BoolVar(&allowFailing, "allow-failing", false, "ask although a required check failed (human only)")
	return c
}

func askApproval(ctx context.Context, w io.Writer, in *packs.Input, sd, id string, destroy, allowFailing bool, stack string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r, err := plans.Load(sd, id)
	if err != nil {
		return err
	}
	gt := envgate.FromManifest(envgate.Cloud, in.Manifest, in.Config)
	if !gt.Declared(r.Env) {
		return fmt.Errorf("environment %s is no longer declared", r.Env)
	}
	action := envgate.Apply
	pol := gt.Policy(r.Env, envgate.Apply)
	switch {
	case r.Destroys() && !destroy:
		return fmt.Errorf("plan %s destroys (%s); pass --destroy to ask for a destroy approval", id, r.Summary.Text())
	case destroy && !r.Destroys():
		return fmt.Errorf("plan %s does not destroy; ask without --destroy", id)
	case destroy:
		action = envgate.Destroy
		pol = envgate.Stricter(pol, gt.Policy(r.Env, envgate.Destroy))
	}
	switch pol {
	case envgate.Approve:
	case envgate.Deny:
		return fmt.Errorf("environment %s has %s: deny; no approval can allow it, a human runs it in their own terminal", r.Env, action)
	default:
		return fmt.Errorf("environment %s has %s: %s; it needs no approval", r.Env, action, pol)
	}
	if r.Tool == "cdk" || r.Tool == "cdktf" {
		if stack == "" && len(r.Stacks) == 1 {
			stack = r.Stacks[0].Name
		}
		if _, ok := r.StackFor(stack); !ok {
			var names []string
			for _, s := range r.Stacks {
				names = append(names, s.Name)
			}
			return fmt.Errorf("name the stack with --stack (one approval per stack): %s", strings.Join(names, ", "))
		}
	} else if stack != "" {
		return errors.New("--stack is for CDK plans")
	}
	b, err := os.ReadFile(filepath.Join(sd, filepath.FromSlash(r.PlanFile)))
	if err != nil || packs.HashBytes(b) != r.PlanSHA256 {
		return fmt.Errorf("the stored plan %s changed or was pruned; record a new plan", id)
	}
	if gt.RequireMerged(r.Env) {
		ref := plans.DefaultRemoteRef(ctx, in.Root)
		switch {
		case !r.Clean:
			return fmt.Errorf("environment %s requires a merged plan, and plan %s was made with uncommitted IaC changes; commit, merge and re-plan", r.Env, id)
		case ref == "":
			return fmt.Errorf("environment %s requires a merged plan, and the default branch's remote ref cannot be found (git fetch)", r.Env)
		case !plans.Merged(ctx, in.Root, r.Commit, ref):
			return fmt.Errorf("environment %s requires a merged plan: commit %.12s is not on %s yet", r.Env, r.Commit, strings.TrimPrefix(ref, "refs/remotes/"))
		}
	}
	if failed := r.FailedChecks(); len(failed) > 0 && !allowFailing {
		return fmt.Errorf("required checks failed on this plan: %s. Fix them and re-plan (a human may pass --allow-failing from their own terminal)", strings.Join(failed, ", "))
	}
	var sum strings.Builder
	if failed := r.FailedChecks(); len(failed) > 0 {
		fmt.Fprintf(&sum, "FAILING CHECKS (asked with --allow-failing): %s\n", strings.Join(failed, ", "))
	}
	fmt.Fprintf(&sum, "%s %s plan %s for environment %s (%s)\n", r.Tool, action, r.ID, r.Env, r.SubjectFor(stack))
	fmt.Fprintf(&sum, "%s\nidentity: %s", r.Summary.Text(), r.Identity)
	if r.Account != "" {
		fmt.Fprintf(&sum, ", account %s", r.Account)
	}
	if r.Weak {
		sum.WriteString("\nbinding: the weaker Pulumi binding (the recorded program files and the names in their folders are re-checked at apply; dependencies outside those folders are not)")
	}
	dec, card, err := approval.Ask(in.Workspace(), approval.Request{Pack: "cloud", Action: action, Subject: r.SubjectFor(stack), Target: r.Env,
		Evidence: r.Evidence(stack), Summary: sum.String(), Options: []string{"apply", "reject"}, ValidFor: gt.TTL(r.Env), ProjectDir: projectDir(in)})
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "asked: decision %s on card %s (%s %s -> %s)\n", dec, card, action, r.SubjectFor(stack), r.Env)
	fmt.Fprintf(w, "a human answers in their own terminal (never an agent, never on the board): tq hub answer %s %s --choice apply\n", card, dec)
	for _, a := range r.Apply {
		if (r.Tool != "cdk" && r.Tool != "cdktf") || strings.HasSuffix(a, " "+stackName(r, stack)) {
			fmt.Fprintf(w, "then apply with: %s\n", a)
		}
	}
	return nil
}

// projectDir is the repo's folder relative to the workspace root ("" when
// they are the same or the repo is outside it).
func projectDir(in *packs.Input) string {
	r, err := filepath.Rel(in.WorkspaceRoot(), in.Root)
	if err != nil || r == "." || strings.HasPrefix(r, "..") || filepath.IsAbs(r) {
		return ""
	}
	return filepath.ToSlash(r)
}

func stackName(r plans.Record, s string) string {
	st, _ := r.StackFor(s)
	return st.Name
}

// ---- prune -------------------------------------------------------------

func newPruneCmd() *cobra.Command {
	var older string
	c := &cobra.Command{
		Use:   "prune [--older 7d]",
		Short: "Delete stored plans older than an age (records and approvals are kept)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			_, sd, err := cmdContext(c)
			if err != nil {
				return err
			}
			d, err := manifest.ParseDuration(older)
			if err != nil || d < 0 {
				return fmt.Errorf("--older %q is not a duration (7d, 12h)", older)
			}
			ids, err := plans.Prune(sd, d, now())
			if err != nil {
				return err
			}
			sort.Strings(ids)
			fmt.Fprintf(c.OutOrStdout(), "pruned %d stored plan(s) older than %s\n", len(ids), older)
			return nil
		},
	}
	c.Flags().StringVar(&older, "older", "7d", "the age past which stored plans are deleted")
	return c
}
