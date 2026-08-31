// Package setupapi is the public facade tq exposes to the Wails desktop
// app. It re-declares every type it needs as a plain, JSON-friendly struct
// so callers outside this module (which cannot import internal/...) never
// see an internal type in a signature. Conversion to/from internal types
// happens in unexported helpers in this package.
package setupapi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/detect"
	"github.com/tentaqles/tentaqles/internal/doctor"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/setup"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// ---- facade types -----------------------------------------------------

// Provider is a plain, JSON-friendly view of a catalog provider.
type Provider struct {
	ID          string
	Name        string
	Category    string
	HasCLI      bool
	HasIdentity bool
	Docs        string
}

// Company mirrors internal/setup.Company field-for-field (a type alias to
// an internal type is not allowed across the internal/ boundary).
type Company struct {
	Name           string
	DisplayName    string
	Color          string
	GitName        string
	GitEmail       string
	GitUser        string
	GitProvider    string
	Identities     []string
	PermissionMode string
}

// Plan mirrors internal/setup.SetupPlan.
type Plan struct {
	Base      string
	Companies []Company
	Hooks     []string
	Trust     bool
}

// Change describes one action Apply would take (or skip).
type Change struct {
	Kind, Target, Detail string
}

// ToolResult is the outcome of probing one provider's CLI.
type ToolResult struct {
	ID        string
	Command   string
	Path      string
	Version   string
	Err       string
	Installed bool
	Hints     []string
}

// Report summarizes what Apply did.
type Report struct {
	Changes  []Change
	Logins   []string
	Warnings []string
}

// HookStatus describes the hook state for a shell.
type HookStatus struct {
	Shell, Profile, State string
}

// Workspace is a plain view of an existing tq workspace.
type Workspace struct {
	Name       string
	Root       string
	Email      string
	Trusted    bool
	Identities []string
}

// Finding is a plain view of a doctor check result.
type Finding struct {
	Level     string
	Code      string
	Workspace string
	Msg       string
	Fix       string
}

// ---- seams --------------------------------------------------------------

// RunGit is the seam Apply uses to run git; tests override it with a fake.
var RunGit = gitcfg.RunGit

// SetUserPath persists dir onto the current user's PATH. The Windows
// implementation edits HKCU\Environment via PowerShell; other platforms are
// a no-op (callers should print a PATH hint themselves). Tests override this
// with a recorder.
var SetUserPath = setUserPathOS

// profilesFn is the seam used to look up shell profile paths; hooks.Profiles
// is an internal type, so this stays unexported. Tests use SetTestProfiles.
var profilesFn = hooks.DefaultProfiles

// SetTestProfiles overrides the shell-profile lookup for tests. Passing nil
// restores the real, per-OS default.
func SetTestProfiles(profiles map[string]string) {
	if profiles == nil {
		profilesFn = hooks.DefaultProfiles
		return
	}
	p := make(hooks.Profiles, len(profiles))
	for k, v := range profiles {
		p[hooks.Shell(k)] = v
	}
	profilesFn = func() hooks.Profiles { return p }
}

// ---- conversions ----------------------------------------------------------

func fromInternalProvider(p providers.Provider) Provider {
	return Provider{
		ID:          p.ID,
		Name:        p.Name,
		Category:    p.Category,
		HasCLI:      p.CLI != nil,
		HasIdentity: p.HasIdentity(),
		Docs:        p.Docs,
	}
}

func (c Company) toInternal() setup.Company {
	return setup.Company{
		Name:           c.Name,
		DisplayName:    c.DisplayName,
		Color:          c.Color,
		GitName:        c.GitName,
		GitEmail:       c.GitEmail,
		GitUser:        c.GitUser,
		GitProvider:    c.GitProvider,
		Identities:     c.Identities,
		PermissionMode: c.PermissionMode,
	}
}

func (p Plan) toInternal() *setup.SetupPlan {
	companies := make([]setup.Company, len(p.Companies))
	for i, c := range p.Companies {
		companies[i] = c.toInternal()
	}
	return &setup.SetupPlan{
		Base:      setup.ExpandHome(p.Base),
		Companies: companies,
		Hooks:     p.Hooks,
		Trust:     p.Trust,
	}
}

func fromInternalChanges(cs []setup.Change) []Change {
	out := make([]Change, len(cs))
	for i, c := range cs {
		out[i] = Change{Kind: c.Kind, Target: c.Target, Detail: c.Detail}
	}
	return out
}

func fromInternalReport(r setup.Report) Report {
	return Report{
		Changes:  fromInternalChanges(r.Changes),
		Logins:   append([]string(nil), r.Logins...),
		Warnings: append([]string(nil), r.Warnings...),
	}
}

func fromInternalFinding(f doctor.Finding) Finding {
	return Finding{Level: f.Level, Code: f.Code, Workspace: f.Workspace, Msg: f.Msg, Fix: f.Fix}
}

// ---- API --------------------------------------------------------------

// Providers returns the merged embedded+user provider catalog.
func Providers() ([]Provider, error) {
	cat, err := providers.Load()
	if err != nil {
		return nil, err
	}
	all := cat.All()
	out := make([]Provider, len(all))
	for i, p := range all {
		out[i] = fromInternalProvider(p)
	}
	return out, nil
}

// DetectShells returns the shells tq can offer to install hooks into.
func DetectShells() ([]string, error) {
	shells := hooks.Detect(profilesFn(), hooks.LookPath)
	out := make([]string, len(shells))
	for i, sh := range shells {
		out[i] = string(sh)
	}
	return out, nil
}

// HooksStatus reports the install state for every known shell.
func HooksStatus() ([]HookStatus, error) {
	p := profilesFn()
	out := make([]HookStatus, 0, len(hooks.Shells))
	for _, sh := range hooks.Shells {
		st := hooks.StatusOf(sh, p)
		out = append(out, HookStatus{Shell: string(st.Shell), Profile: st.Profile, State: st.State})
	}
	return out, nil
}

// DefaultBase returns ~/work, or the first registered base if one exists.
func DefaultBase() string {
	if cfg, err := registry.Load(); err == nil && len(cfg.Bases) > 0 {
		return cfg.Bases[0]
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "work"
	}
	return filepath.Join(home, "work")
}

// ExistingWorkspaces lists every workspace under every registered base.
func ExistingWorkspaces() ([]Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	wss, errs := resolve.ListWorkspaces(cfg)
	if len(errs) > 0 && len(wss) == 0 {
		return nil, errs[0]
	}
	out := make([]Workspace, len(wss))
	for i, w := range wss {
		out[i] = Workspace{
			Name:       w.Name,
			Root:       w.Root,
			Email:      w.Manifest.Git.Email,
			Trusted:    trust.IsTrusted(w.Hash),
			Identities: w.Manifest.IdentityNames(),
		}
	}
	return out, nil
}

// ValidatePlan checks structural rules on the plan.
func ValidatePlan(p Plan) error {
	cat, err := providers.Load()
	if err != nil {
		return err
	}
	return p.toInternal().Validate(cat)
}

// Preview is read-only: it reports what Apply would do without writing
// anything.
func Preview(p Plan) ([]Change, error) {
	changes, err := setup.Preview(p.toInternal(), profilesFn())
	if err != nil {
		return nil, err
	}
	return fromInternalChanges(changes), nil
}

// ToolCheck probes each company's identities' CLI tools and fills in
// install hints for the current OS.
func ToolCheck(p Plan) (map[string][]ToolResult, error) {
	cat, err := providers.Load()
	if err != nil {
		return nil, err
	}
	d := detect.DefaultDeps()
	raw := setup.ToolCheck(p.toInternal(), cat, d)
	out := make(map[string][]ToolResult, len(raw))
	for name, results := range raw {
		trs := make([]ToolResult, len(results))
		for i, r := range results {
			var hints []string
			if !r.Installed {
				if prov, ok := cat.Get(r.ID); ok {
					hints = detect.Hints(prov, runtime.GOOS)
				}
			}
			trs[i] = ToolResult{
				ID:        r.ID,
				Command:   r.Command,
				Path:      r.Path,
				Version:   r.Version,
				Err:       r.Err,
				Installed: r.Installed,
				Hints:     hints,
			}
		}
		out[name] = trs
	}
	return out, nil
}

// Apply executes the plan: registers the base, ensures git's global
// include, scaffolds each company as a workspace, installs shell hooks, and
// collects login commands.
func Apply(p Plan) (Report, error) {
	cat, err := providers.Load()
	if err != nil {
		return Report{}, err
	}
	r, err := setup.Apply(p.toInternal(), cat, setup.ApplyOptions{
		RunGit:   RunGit,
		Profiles: profilesFn(),
	})
	return fromInternalReport(r), err
}

// Doctor runs the read-only identity health checks.
func Doctor() ([]Finding, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	findings := doctor.Run(cfg, doctor.Deps{
		Env:      os.LookupEnv,
		Cwd:      cwd,
		RunGit:   RunGit,
		LookPath: exec.LookPath,
	})
	out := make([]Finding, len(findings))
	for i, f := range findings {
		out[i] = fromInternalFinding(f)
	}
	return out, nil
}

// LoginCommand returns the "tq login <ws> <id>" invocation string.
func LoginCommand(ws, id string) string {
	return fmt.Sprintf("tq login %s %s", ws, id)
}

// TQVersion runs "tq version" if tq is on PATH, returning its trimmed
// output. It returns "" (no error) when tq is not found.
// TQPath finds the tq binary: PATH first, then the directory this app installs
// into.
//
// That second lookup matters more than it sounds. InstallTQ writes the binary
// and updates the PERSISTENT user PATH, which does not reach an already
// running process -- so a PATH-only check reports "tq is not installed"
// immediately after installing it successfully, which is the first thing a new
// user would do.
func TQPath() string {
	if p, err := exec.LookPath("tq"); err == nil {
		return p
	}
	dir, err := installDestDir()
	if err != nil {
		return ""
	}
	name := "tq"
	if runtime.GOOS == "windows" {
		name = "tq.exe"
	}
	p := filepath.Join(dir, name)
	if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
		return p
	}
	return ""
}

// TQVersion returns the installed tq's version string.
//
// An empty string with a nil error means tq is genuinely absent. A non-nil
// error means it was found and would not run, which is a different problem
// with a different fix -- reporting both as "not installed" sends someone off
// to install something they already have.
func TQVersion() (string, error) {
	path := TQPath()
	if path == "" {
		return "", nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if i := strings.IndexAny(detail, "\r\n"); i >= 0 {
			detail = detail[:i]
		}
		if detail != "" {
			detail = ": " + detail
		}
		return "", fmt.Errorf("found tq at %s but could not run it: %w%s", path, err, detail)
	}
	return strings.TrimSpace(string(out)), nil
}

// installDestDir returns the directory InstallTQ copies the tq binary into.
func installDestDir() (string, error) {
	if runtime.GOOS == "windows" {
		local := os.Getenv("LOCALAPPDATA")
		if local == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(local, "tentaqles", "bin"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin"), nil
}

// filesIdentical reports whether a and b have the same size and bytes.
func filesIdentical(a, b string) bool {
	fa, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	fb, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return len(fa) == len(fb) && string(fa) == string(fb)
}

// InstallTQ copies the tq binary at fromPath into tq's per-user install
// directory (idempotent: a byte-identical destination is left untouched),
// then adds that directory to the user's PATH, and returns the destination
// path.
func InstallTQ(fromPath string) (string, error) {
	destDir, err := installDestDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(destDir, filepath.Base(fromPath))

	if _, err := os.Stat(dest); err == nil && filesIdentical(fromPath, dest) {
		// Already installed and byte-identical: no-op.
	} else if existing, err := os.Stat(dest); err == nil && !existing.IsDir() {
		// A different tq is already installed. Refuse rather than overwrite.
		//
		// This is not hypothetical caution: a stale bundled copy replaced a
		// working install here and silently removed a subcommand the Claude
		// Code plugin depends on, so every session in every workspace began
		// erroring on each command. Whoever is installing may well have the
		// older binary, and an install button is not the place to find out.
		have, herr := runTQVersion(dest)
		want, werr := runTQVersion(fromPath)
		switch {
		case herr != nil || werr != nil:
			return "", fmt.Errorf("%s already exists and could not be identified; remove it first if you mean to replace it", dest)
		case have == want:
			// Same version, different bytes (a rebuild). Replacing is safe.
			if err := copyExecutable(fromPath, dest); err != nil {
				return "", err
			}
		default:
			return "", fmt.Errorf("%s is already %s and this would install %s; remove it first if you mean to replace it", dest, have, want)
		}
	} else {
		if err := copyExecutable(fromPath, dest); err != nil {
			return "", err
		}
	}

	if err := SetUserPath(destDir); err != nil {
		return dest, err
	}
	return dest, nil
}

// AddCustomProvider validates and writes a minimal custom provider as a
// user override file, mirroring the logic `tq providers add` uses. It
// returns the path the provider was written to.
func AddCustomProvider(id, name, category, command string, env map[string]string) (string, error) {
	p := providers.Provider{
		ID:       id,
		Name:     name,
		Category: category,
	}
	if command != "" {
		p.CLI = &providers.CLI{Command: command}
	}
	if len(env) > 0 {
		for k, v := range env {
			if err := providers.ValidateEnvPair(k, v); err != nil {
				return "", err
			}
		}
		p.Identity = providers.Identity{Env: env}
	}
	if err := p.Validate(); err != nil {
		return "", err
	}
	return providers.WriteUser(p)
}

// FolderCandidate is a first-level folder under the work folder that could
// become a company.
type FolderCandidate struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// Managed is true when the folder is already a tq workspace.
	Managed bool `json:"managed"`
	// Repos counts child directories that look like git repositories, which is
	// what makes a folder recognisable as "the place I keep this client's work".
	Repos int `json:"repos"`
	// GitName and GitEmail are the identity the repositories in this folder
	// actually use today, read from the first one found. Empty when there is
	// nothing to read. Offering these back is the difference between adopting
	// a folder and retyping what it already knew.
	GitName  string `json:"gitName"`
	GitEmail string `json:"gitEmail"`
}

// BaseFolders lists what is already sitting in the work folder.
//
// Someone adopting tq usually has the folders already -- personal, one per
// client, each full of repositories -- and being made to retype their names
// into an empty form is both tedious and a chance to typo a name that has to
// match the directory exactly. This is what lets the UI offer them instead.
func BaseFolders(base string) ([]FolderCandidate, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not yet created is not an error to show anyone
		}
		return nil, err
	}
	var out []FolderCandidate
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		root := filepath.Join(base, e.Name())
		c := FolderCandidate{Name: e.Name(), Path: root}
		if _, err := os.Stat(filepath.Join(root, manifest.FileName)); err == nil {
			c.Managed = true
		}
		c.Repos, c.GitName, c.GitEmail = scanRepos(root)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// scanRepos counts git repositories directly under root and reads the identity
// the first one uses. It reads .git/config rather than shelling out to git:
// this runs for every folder in the work directory while someone waits on a
// screen, and a process per folder is not worth the precision here.
func scanRepos(root string) (count int, name, email string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return 0, "", ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), ".git")); err != nil {
			continue
		}
		count++
		if name == "" && email == "" {
			name, email = userFromGitConfig(filepath.Join(root, e.Name(), ".git", "config"))
		}
	}
	return count, name, email
}

// userFromGitConfig reads name and email from a [user] section. It is a
// deliberately small parser: anything it cannot read simply yields no
// suggestion, which is the same as not offering one.
func userFromGitConfig(path string) (name, email string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	inUser := false
	for i, line := range strings.Split(string(raw), "\n") {
		t := strings.TrimSpace(line)
		if i == 0 {
			// A config written by a Windows tool can carry a UTF-8 BOM, which
			// would make the first section header unrecognisable and silently
			// yield no suggestion at all.
			t = strings.TrimPrefix(t, "")
		}
		if strings.HasPrefix(t, "[") {
			inUser = strings.HasPrefix(strings.ToLower(t), "[user")
			continue
		}
		if !inUser {
			continue
		}
		k, v, ok := strings.Cut(t, "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(k)) {
		case "name":
			name = strings.TrimSpace(v)
		case "email":
			email = strings.TrimSpace(v)
		}
	}
	return name, email
}

// copyExecutable copies src over dst and makes it executable.
func copyExecutable(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return err
	}
	return os.Chmod(dst, 0o755)
}

// runTQVersion asks a tq binary what it is. Used to avoid replacing one
// version with another without saying so.
func runTQVersion(path string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
