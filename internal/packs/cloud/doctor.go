package cloud

// The cloud pack's doctor probes and detection hints (spec D §19,
// workstream D-OI). tq doctor and tq pack doctor call them through
// Pack.Doctor; F's packs-suggest evaluates the hints of a disabled pack.
//
// Codes:
//   cloud-tool-old              a declared opa is older than 1.0 (Rego v1);
//                               conftest's minimum is the framework's
//                               packs-tool-old (Check.MinVersion)
//   cloud-tool-version-unknown  a declared opa whose version cannot be read
//   cloud-env-unresolvable      an environment no signal can reach, or a
//                               match path that does not exist
//   cloud-env-overlap           two environments match the same value
//   cloud-env-default-unknown   default_environment names no environment
//   cloud-plan-store-in-repo    the plan store lies inside the repository
//   cloud-plan-store-mode       the plan store is not 0700 / its files 0600
//   cloud-state-tracked         git tracks a *.tfstate, *.tfplan or .terraform/
//   cloud-state-not-ignored     the repository's .gitignore misses one of them
//
// Probes never execute a repo-local binary, never read a .env file and
// never touch the network: opa is run (`opa version`) only when it resolves
// outside the repository, and git runs locally with a timeout.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
	"github.com/tentaqles/tentaqles/internal/packs/envgate"
	"github.com/tentaqles/tentaqles/internal/packs/toolrun"
)

// OPAMin is the first OPA release that evaluates Rego v1 by default.
const OPAMin = "1.0.0"

// cfnContains marks a CloudFormation or SAM template (YAML or JSON).
const cfnContains = `AWSTemplateFormatVersion|["']?Type["']?\s*:\s*["']?AWS::[A-Za-z0-9]+::`

// Hints are the cloud pack's detection hints (§19, §22 E).
var Hints = []packs.Hint{
	{Glob: "*.tf", Why: "Terraform files"},
	{Glob: "*.tf.json", Why: "Terraform files"},
	{Glob: "*.tofu", Why: "OpenTofu files"},
	{Glob: "*.tofu.json", Why: "OpenTofu files"},
	{Glob: "*.bicep", Why: "Bicep files"},
	{Glob: "cdk.json", Why: "an AWS CDK app"},
	{Glob: "Pulumi.yaml", Why: "a Pulumi project"},
	{Glob: "Pulumi.yml", Why: "a Pulumi project"},
	{Glob: "*.yaml", Contains: cfnContains, Why: "CloudFormation templates"},
	{Glob: "*.yml", Contains: cfnContains, Why: "CloudFormation templates"},
	{Glob: "*.json", Contains: cfnContains, Why: "CloudFormation templates"},
	{Glob: "*.template", Contains: cfnContains, Why: "CloudFormation templates"},
}

func init() {
	packs.RegisterPack(packs.Pack{Name: "cloud", Hints: Hints, Doctor: Doctor})
}

// Doctor is the cloud pack's Pack.Doctor.
func Doctor(in *packs.Input) []doctor.Finding {
	d := &probe{ws: in.Workspace()}
	d.opa(in)
	d.envs(in)
	d.planStore(in)
	d.stateFiles(in)
	return d.out
}

type probe struct {
	ws  string
	out []doctor.Finding
}

func (d *probe) add(level, code, msg, fix string) {
	d.out = append(d.out, doctor.Finding{Level: level, Code: code, Workspace: d.ws, Msg: msg, Fix: fix})
}

// opa: a declared opa must be 1.x. toolrun's probe reads `--version`, which
// opa does not take, so the version comes from `opa version`, run only when
// the binary resolves outside the repository.
func (d *probe) opa(in *packs.Input) {
	if !in.Settings().Declared("opa") || in.Tools == nil {
		return
	}
	p := in.Tools.Probe("cloud", "opa")
	if !p.Found {
		return // packs-tool-missing
	}
	v := p.Version
	if v == "" {
		if (&toolrun.Runner{Root: in.Root}).RepoLocal(p) {
			d.add("info", "cloud-tool-version-unknown", "opa resolves inside the repository; doctor never runs a repo-local binary, so its version is not checked",
				"install opa outside the repository, or set packs.cloud.tool_paths.opa in the manifest")
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), toolrun.VersionTimeout)
		out, err := in.Tools.Run(ctx, toolrun.Spec{Pack: "cloud", Tool: "opa", Args: []string{"version"}, Timeout: toolrun.VersionTimeout})
		cancel()
		if err == nil && !out.TimedOut {
			v = toolrun.ParseVersion(string(out.Stdout))
		}
	}
	switch {
	case v == "":
		d.add("info", "cloud-tool-version-unknown", "the opa version cannot be read; the cloud pack needs OPA "+OPAMin+" or later (Rego v1)", "check `opa version`")
	case toolrun.CompareVersions(v, OPAMin) < 0:
		d.add("warn", "cloud-tool-old", fmt.Sprintf("opa %s is below the cloud pack minimum %s: the shipped Rego bundle is Rego v1", v, OPAMin), "upgrade opa to 1.x")
	}
}

func (d *probe) envs(in *packs.Input) {
	d.out = append(d.out, EnvFindings("cloud", envgate.Cloud, in)...)
}

// pathKeys are the match keys whose entries are repository paths.
var pathKeys = []string{"dirs", "var_files"}

// EnvFindings checks the effective environments of spec (§19): every
// environment can be reached by a signal (it has match entries, and its path
// entries exist), no two environments match the same value, and
// default_environment names a declared one. Codes are <prefix>-env-*.
func EnvFindings(prefix string, spec envgate.Spec, in *packs.Input) []doctor.Finding {
	d := &probe{ws: in.Workspace()}
	gt := envgate.FromManifest(spec, in.Manifest, in.Config)
	names := gt.Envs()
	for _, n := range names {
		m := gt.Match(n)
		total := 0
		for _, vs := range m {
			total += len(vs)
		}
		if total == 0 {
			d.add("warn", prefix+"-env-unresolvable", fmt.Sprintf("environment %s has no match entries: tq resolves a target to it only by name or as the default", n),
				"add match entries (dirs, stacks, subscriptions, accounts, …) under packs."+spec.Pack+".environments."+n+".match")
			continue
		}
		for _, k := range pathKeys {
			for _, p := range m[k] {
				clean := strings.TrimPrefix(path.Clean(strings.ReplaceAll(p, `\`, "/")), "./")
				if clean == "." || in.Root == "" {
					continue
				}
				if _, err := os.Stat(filepath.Join(in.Root, filepath.FromSlash(clean))); err != nil {
					d.add("warn", prefix+"-env-unresolvable", fmt.Sprintf("environments.%s.match.%s: %s does not exist in the repository, so it never matches", n, k, p),
						"fix the path, or remove the entry")
				}
			}
		}
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			a, b := gt.Match(names[i]), gt.Match(names[j])
			keys := make([]string, 0, len(a))
			for k := range a {
				keys = append(keys, k)
			}
			sort.Strings(keys)
		pair:
			for _, k := range keys {
				for _, x := range a[k] {
					for _, y := range b[k] {
						if envgate.Matches(k, x, y) || envgate.Matches(k, y, x) {
							d.add("warn", prefix+"-env-overlap", fmt.Sprintf("environments %s and %s both match %s %q / %q; a target there resolves to the stricter one (%s)",
								names[i], names[j], k, x, y, gt.StricterEnv(names[i], names[j], "")),
								"give each environment its own match values")
							break pair
						}
					}
				}
			}
		}
	}
	if def := in.Settings().String("default_environment"); def != "" && len(names) > 0 && !gt.Declared(def) {
		d.add("warn", prefix+"-env-default-unknown", "default_environment "+def+" is not a declared environment; an unresolved target counts as the strictest one",
			"name a declared environment, or remove default_environment")
	}
	return d.out
}

// planStore: the store lies outside the repository, and on Unix the
// plans folder is 0700 and every file in it 0600 (§9.4). Windows has no
// mode bits to check; the folder sits under the user's profile there.
func (d *probe) planStore(in *packs.Input) {
	sd := in.StateDir()
	if sd == "" {
		return
	}
	for _, root := range []string{in.Root, in.WorkspaceRoot()} {
		if root != "" && inside(sd, root) {
			d.add("error", "cloud-plan-store-in-repo", "the cloud plan store ($TQ_HOME/packs/"+d.ws+"/cloud/plans) is inside the repository: stored plans hold secret values",
				"set TQ_HOME to a folder outside the repository")
			break
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	dir := filepath.Join(sd, plans.Dir)
	fi, err := os.Lstat(dir)
	if err != nil {
		return // no plan recorded yet
	}
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 {
		d.add("error", "cloud-plan-store-mode", fmt.Sprintf("the cloud plan store folder is %s, not a 0700 folder", fi.Mode()), "chmod 700 "+dir)
		return
	}
	f, err := os.Open(dir)
	if err != nil {
		return
	}
	des, _ := f.ReadDir(10000)
	f.Close()
	bad, first := 0, ""
	for _, de := range des {
		info, err := os.Lstat(filepath.Join(dir, de.Name()))
		if err != nil || info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
			if bad++; first == "" {
				first = de.Name()
			}
		}
	}
	if bad > 0 {
		d.add("error", "cloud-plan-store-mode", fmt.Sprintf("%d file(s) in the cloud plan store are not 0600 regular files (first: %s)", bad, first), "chmod 600 "+filepath.Join(dir, "*"))
	}
}

// inside reports p at or under root, by final path when both exist.
func inside(p, root string) bool {
	norm := func(s string) string {
		if fp, err := packs.FinalPath(s); err == nil {
			s = fp
		} else if abs, err := filepath.Abs(s); err == nil {
			s = abs
		}
		s = filepath.Clean(s)
		if runtime.GOOS == "windows" {
			s = strings.ToLower(s)
		}
		return s
	}
	rel, err := filepath.Rel(norm(root), norm(p))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// StateFile reports a Terraform state, plan or .terraform/ path.
func StateFile(p string) bool {
	p = strings.ReplaceAll(p, `\`, "/")
	base := strings.ToLower(path.Base(p))
	if strings.HasSuffix(base, ".tfstate") || strings.HasSuffix(base, ".tfstate.backup") || strings.HasSuffix(base, ".tfplan") {
		return true
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".terraform" {
			return true
		}
	}
	return false
}

func terraformFile(p string) bool {
	for _, g := range []string{"*.tf", "*.tf.json", "*.tofu", "*.tofu.json"} {
		if packs.MatchGlob(g, p) {
			return true
		}
	}
	return false
}

// gitTimeout bounds the check-ignore call.
const gitTimeout = 10 * time.Second

// stateFiles: none of *.tfstate, *.tfplan and .terraform/ is tracked, and
// with Terraform files present, the repository's own ignore files cover
// all three (git ls-files and git check-ignore, both local).
func (d *probe) stateFiles(in *packs.Input) {
	if in.Git == nil {
		return
	}
	files, err := in.Git.ListFiles()
	if err != nil {
		return
	}
	var tracked []string
	tfDir := ""
	for _, f := range files {
		if StateFile(f) {
			tracked = append(tracked, f)
		}
		if tfDir == "" && terraformFile(f) {
			tfDir = path.Dir(f)
		}
	}
	if len(tracked) > 0 {
		show := tracked
		if len(show) > 3 {
			show = show[:3]
		}
		d.add("error", "cloud-state-tracked", fmt.Sprintf("git tracks %d Terraform state, plan or .terraform/ file(s) (%s): they hold secret values in clear text", len(tracked), strings.Join(show, ", ")),
			"git rm --cached them, and add *.tfstate, *.tfplan and .terraform/ to .gitignore")
	}
	if tfDir == "" {
		return
	}
	root := in.Root
	if gr, ok := in.Git.(packs.GitRepo); ok && gr.Root != "" {
		root = gr.Root
	}
	missing, localOnly, err := notIgnored(root, tfDir)
	if err != nil {
		d.add("info", "cloud-state-not-ignored", "git check-ignore did not run ("+err.Error()+"); the .gitignore entries for Terraform state are not checked", "")
		return
	}
	if len(missing) > 0 {
		msg := fmt.Sprintf("the repository's .gitignore does not ignore %s (checked in %s)", strings.Join(missing, ", "), tfDir)
		if len(localOnly) > 0 {
			msg += "; " + strings.Join(localOnly, ", ") + " is ignored only by your own excludes (.git/info/exclude or core.excludesFile), which teammates do not share"
		}
		d.add("warn", "cloud-state-not-ignored", msg, "add "+strings.Join(missing, ", ")+" to .gitignore")
	}
}

// statePatterns maps each required pattern to the probe path checked.
var statePatterns = []struct{ pattern, probe string }{
	{"*.tfstate", "terraform.tfstate"},
	{"*.tfplan", "tq-doctor.tfplan"},
	{".terraform/", ".terraform/providers"},
}

// notIgnored runs `git check-ignore --no-index -v -n -z --stdin` over the
// probe paths in dir. A path counts as ignored only when a pattern of a
// repository ignore file (a relative source outside .git/) ignores it; a
// negation does not ignore.
func notIgnored(root, dir string) (missing, localOnly []string, err error) {
	var in bytes.Buffer
	for _, s := range statePatterns {
		in.WriteString(path.Join(dir, s.probe))
		in.WriteByte(0)
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "check-ignore", "--no-index", "-v", "-n", "-z", "--stdin")
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdin = &in
	cmd.WaitDelay = 2 * time.Second
	out, runErr := cmd.Output()
	if runErr != nil {
		var ee *exec.ExitError
		if ctx.Err() != nil {
			return nil, nil, errors.New("timed out")
		}
		if !errors.As(runErr, &ee) || ee.ExitCode() != 1 { // 1: nothing ignored
			return nil, nil, runErr
		}
	}
	fields := strings.Split(string(out), "\x00")
	for i, s := range statePatterns {
		if 4*i+3 >= len(fields) {
			return nil, nil, errors.New("unexpected check-ignore output")
		}
		src, pat := fields[4*i], fields[4*i+2]
		switch {
		case src == "" || strings.HasPrefix(pat, "!"):
			missing = append(missing, s.pattern)
		case filepath.IsAbs(src) || strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".git/"):
			missing = append(missing, s.pattern)
			localOnly = append(localOnly, s.pattern)
		}
	}
	return missing, localOnly, nil
}
