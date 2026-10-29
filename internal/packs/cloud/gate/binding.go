package gate

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/cloud/plans"
)

// The per-tool bindings of §9.5 that both tq pack cloud plan (at record
// time) and the guard (at apply time) compute, so the two agree byte for
// byte.

func rel(dir, p string) string {
	p = strings.ReplaceAll(strings.Trim(p, `"'`), `\`, "/")
	if strings.HasPrefix(p, "/") || (len(p) > 1 && p[1] == ':') || dir == "" || dir == "." {
		return p
	}
	return path.Join(dir, p)
}

// AzScope is the deployment scope of an az deployment command:
// group:<rg>, sub, mg:<id> or tenant.
func AzScope(iv Invocation) string {
	if len(iv.Verb) < 2 {
		return ""
	}
	switch iv.Verb[1] {
	case "group":
		rg, _ := iv.Flag("resource-group", "g")
		return "group:" + strings.ToLower(rg)
	case "sub":
		return "sub"
	case "mg":
		m, _ := iv.Flag("management-group-id", "m")
		return "mg:" + strings.ToLower(m)
	case "tenant":
		return "tenant"
	}
	return ""
}

// AzBinding hashes an az deployment's template and parameters (§9.5): the
// --template-file content, and each --parameters value (a file's content
// for @file or a .json / .bicepparam path, the text otherwise). A remote
// template (--template-uri, --template-spec) cannot be bound.
func AzBinding(iv Invocation, h Host) (template, params string, err error) {
	if iv.Has("template-uri", "u", "template-spec", "s") {
		return "", "", errors.New("a remote template (--template-uri / --template-spec) cannot be bound to the plan; deploy a local --template-file")
	}
	tf, ok := iv.Flag("template-file", "f")
	if !ok || tf == "" {
		return "", "", errors.New("the deployment names no --template-file")
	}
	dir := iv.Dir
	if dir == "?" {
		return "", "", errors.New("the command's folder is unknown (cd to an absolute path); run it from the repository")
	}
	template, err = HashLocal(h, rel(dir, tf))
	if err != nil {
		return "", "", err
	}
	if template == plans.None {
		return "", "", fmt.Errorf("the template %s cannot be read", tf)
	}
	var parts []string
	for _, p := range iv.FlagAll("parameters", "p") {
		file := strings.TrimPrefix(p, "@")
		if strings.HasPrefix(p, "@") || strings.HasSuffix(strings.ToLower(file), ".json") || strings.HasSuffix(strings.ToLower(file), ".bicepparam") {
			hp, err := HashLocal(h, rel(dir, file))
			if err != nil {
				return "", "", err
			}
			parts = append(parts, "file:"+hp)
		} else {
			parts = append(parts, "text:"+packs.HashBytes([]byte(p)))
		}
	}
	return template, HashParts(parts), nil
}

// CDKManifest reads a cloud assembly's stacks from manifest.json.
func CDKManifest(raw []byte) ([]plans.Stack, error) {
	var m struct {
		Artifacts map[string]struct {
			Type        string `json:"type"`
			DisplayName string `json:"displayName"`
			Properties  struct {
				TemplateFile string `json:"templateFile"`
				StackName    string `json:"stackName"`
				File         string `json:"file"`
			} `json:"properties"`
			Dependencies []string `json:"dependencies"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("cdk manifest.json: %w", err)
	}
	var out []plans.Stack
	for id, a := range m.Artifacts {
		if a.Type != "aws:cloudformation:stack" || a.Properties.TemplateFile == "" {
			continue
		}
		s := plans.Stack{Name: id, Template: a.Properties.TemplateFile}
		for _, alias := range []string{a.Properties.StackName, a.DisplayName} {
			if alias != "" && alias != id {
				s.Aliases = append(s.Aliases, alias)
			}
		}
		for _, d := range a.Dependencies {
			if dep, ok := m.Artifacts[d]; ok && dep.Type == "cdk:asset-manifest" {
				s.Assets = dep.Properties.File
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func safeName(n string) bool {
	return n != "" && !strings.Contains(n, "..") && !strings.HasPrefix(n, "/") && !strings.Contains(n, `\`) && !strings.Contains(n, ":")
}

// CDKStackHash hashes one stack of an assembly in app (repo-relative): the
// manifest, the stack's template and its asset manifest.
func CDKStackHash(h Host, app string, s plans.Stack) (string, error) {
	files := []string{"manifest.json", s.Template}
	if s.Assets != "" {
		files = append(files, s.Assets)
	}
	var parts []string
	for _, f := range files {
		if !safeName(f) {
			return "", fmt.Errorf("the assembly names an unsafe file %q", f)
		}
		hf, err := HashLocal(h, path.Join(app, f))
		if err != nil {
			return "", err
		}
		if hf == plans.None {
			return "", fmt.Errorf("%s is missing from the assembly %s", f, app)
		}
		parts = append(parts, f+"\x00"+hf)
	}
	return HashParts(parts), nil
}

// PulumiBinding hashes the Pulumi project file and the stack's config file
// in dir (the weaker binding of §9.5).
func PulumiBinding(h Host, dir, stack string) (project, stackCfg string, err error) {
	project, err = HashLocal(h, path.Join(dir, "Pulumi.yaml"))
	if err == nil && project == plans.None {
		project, err = HashLocal(h, path.Join(dir, "Pulumi.yml"))
	}
	if err != nil {
		return "", "", err
	}
	short := stack
	if i := strings.LastIndex(short, "/"); i >= 0 {
		short = short[i+1:]
	}
	if !safeName(short) {
		return "", "", fmt.Errorf("stack %q is not a plain name", stack)
	}
	stackCfg, err = HashLocal(h, path.Join(dir, "Pulumi."+short+".yaml"))
	return project, stackCfg, err
}

// TreeHash is the combined hash of Pulumi's weak-binding program files
// (repo-relative paths): sha256 over the sorted "path sha256:<hex>" lines.
// A file that is missing, unreadable or over the 1 MiB read cap is an
// error, so the guard denies.
func TreeHash(h Host, files []string) (string, error) {
	if len(files) == 0 || len(files) > plans.MaxTreeFiles {
		return "", fmt.Errorf("the recorded program tree has %d files (1 to %d are re-checked)", len(files), plans.MaxTreeFiles)
	}
	fs := append([]string(nil), files...)
	sort.Strings(fs)
	parts := make([]string, 0, len(fs))
	for _, f := range fs {
		if strings.Contains("/"+f+"/", "/../") || path.IsAbs(f) {
			return "", fmt.Errorf("the program file %s is outside the repository", f)
		}
		b, err := h.ReadLocal(f, 1<<20)
		if err != nil {
			return "", fmt.Errorf("the program file %s cannot be re-read (%v)", f, err)
		}
		parts = append(parts, f+" "+packs.HashBytes(b))
	}
	return HashParts(parts), nil
}

// MaxListingDirs bounds the folders ListingHash lists (each is one
// GuardCtx.ListLocal call inside the guard's budget).
const MaxListingDirs = 16

// ListingDirs are the folders whose names Pulumi's weak binding fixes: the
// program folder dir and every folder that holds a tree file (repo-relative).
func ListingDirs(dir string, tree []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(d string) {
		d = path.Clean(strings.ReplaceAll(d, `\`, "/"))
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	add(dir)
	for _, f := range tree {
		add(path.Dir(f))
	}
	sort.Strings(out)
	return out
}

// ListingHash hashes the names (and kinds: a trailing / for a folder) in
// each folder of ListingDirs, as GuardCtx.ListLocal returns them. A file
// added to the program folder, or next to a program file, after the plan
// changes it (§9.5 "an unchanged tree", closing the r0 residual). Sizes are
// left out: the tree files' contents are hashed by TreeHash already.
func ListingHash(h Host, dir string, tree []string) (string, error) {
	if h.ListLocal == nil {
		return "", errors.New("the program folder cannot be listed")
	}
	dirs := ListingDirs(dir, tree)
	if len(dirs) > MaxListingDirs {
		return "", fmt.Errorf("the Pulumi program spans %d folders, over the %d the weak binding lists; record an update plan with --save-plan", len(dirs), MaxListingDirs)
	}
	var parts []string
	for _, d := range dirs {
		if strings.Contains("/"+d+"/", "/../") || path.IsAbs(d) {
			return "", fmt.Errorf("the program folder %s is outside the repository", d)
		}
		entries, err := h.ListLocal(d, packs.MaxListEntries)
		if err != nil {
			return "", fmt.Errorf("the program folder %s cannot be listed (%v)", d, err)
		}
		for _, e := range entries {
			n := e.Name
			if e.Dir {
				n += "/"
			}
			parts = append(parts, d+"\x00"+n)
		}
	}
	return HashParts(parts), nil
}
