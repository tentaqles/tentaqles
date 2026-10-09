package decide

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Skill picker for the UserPromptSubmit hook: Jev picks the installed skill
// (or "none") that best fits a prompt. It is a hint only: shadow mode logs
// the pick, enforce mode adds one line of context. It never blocks.

// Skill is one installed skill.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"` // project | user | plugin
}

const (
	// MaxSkills caps the options of the choice question.
	MaxSkills = 60
	// maxSkillDescription truncates each option's description.
	maxSkillDescription = 200
	// maxSkillFrontmatter bounds how much of a SKILL.md is read.
	maxSkillFrontmatter = 16 << 10
	// NoSkill is the option meaning no listed skill fits.
	NoSkill        = "none"
	maxSkillPrompt = 4000
)

type skillDir struct {
	Dir    string // a skills/ directory holding <name>/SKILL.md
	Source string
	Prefix string // "plugin:" for plugin skills
}

// skillDirs lists the skills directories, project first, then user, then
// installed plugins (sorted by plugin key). It reads installed_plugins.json,
// which is small; everything else is stat-only until a rescan.
func skillDirs(configDir, cwd string) ([]skillDir, string) {
	var dirs []skillDir
	if cwd != "" {
		dirs = append(dirs, skillDir{Dir: filepath.Join(cwd, ".claude", "skills"), Source: "project"})
	}
	if configDir == "" {
		return dirs, ""
	}
	dirs = append(dirs, skillDir{Dir: filepath.Join(configDir, "skills"), Source: "user"})
	pluginsFile := filepath.Join(configDir, "plugins", "installed_plugins.json")
	raw, err := os.ReadFile(pluginsFile)
	if err != nil {
		return dirs, pluginsFile
	}
	var f struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(raw, &f) != nil {
		return dirs, pluginsFile
	}
	keys := make([]string, 0, len(f.Plugins))
	for k := range f.Plugins {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		name, _, _ := strings.Cut(k, "@")
		for _, inst := range f.Plugins[k] {
			if inst.InstallPath == "" {
				continue
			}
			dirs = append(dirs, skillDir{Dir: filepath.Join(inst.InstallPath, "skills"), Source: "plugin", Prefix: name + ":"})
			break // the first install of a plugin is enough
		}
	}
	return dirs, pluginsFile
}

// skillStamp fingerprints every input of the index by path, size and
// mtime, so the cache is rebuilt only when a skill is added, removed or
// edited.
func skillStamp(dirs []skillDir, pluginsFile string) string {
	h := sha256.New()
	stat := func(p string) {
		if fi, err := os.Stat(p); err == nil {
			fmt.Fprintf(h, "%s|%d|%d\n", p, fi.Size(), fi.ModTime().UnixNano())
		} else {
			fmt.Fprintf(h, "%s|-\n", p)
		}
	}
	if pluginsFile != "" {
		stat(pluginsFile)
	}
	for _, d := range dirs {
		stat(d.Dir)
		ents, err := os.ReadDir(d.Dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				stat(filepath.Join(d.Dir, e.Name(), "SKILL.md"))
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

type skillCache struct {
	Stamp  string  `json:"stamp"`
	Skills []Skill `json:"skills"`
}

// SkillIndex returns the installed skills (at most MaxSkills), from a cache
// under cacheDir keyed on configDir+cwd and invalidated by mtimes.
func SkillIndex(cacheDir, configDir, cwd string) []Skill {
	dirs, pluginsFile := skillDirs(configDir, cwd)
	stamp := skillStamp(dirs, pluginsFile)
	key := sha256.Sum256([]byte(configDir + "\n" + cwd))
	cachePath := filepath.Join(cacheDir, "skills-"+hex.EncodeToString(key[:8])+".json")
	if raw, err := os.ReadFile(cachePath); err == nil {
		var c skillCache
		if json.Unmarshal(raw, &c) == nil && c.Stamp == stamp {
			return c.Skills
		}
	}
	skills := scanSkills(dirs)
	if raw, err := json.Marshal(skillCache{Stamp: stamp, Skills: skills}); err == nil {
		_ = os.MkdirAll(cacheDir, 0o700)
		tmp := cachePath + ".tmp"
		if os.WriteFile(tmp, raw, 0o600) == nil {
			_ = os.Rename(tmp, cachePath)
		}
	}
	return skills
}

func scanSkills(dirs []skillDir) []Skill {
	var out []Skill
	seen := map[string]bool{NoSkill: true}
	for _, d := range dirs {
		ents, err := os.ReadDir(d.Dir)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if len(out) >= MaxSkills {
				return out
			}
			if !e.IsDir() {
				continue
			}
			name, desc, ok := readSkillFrontmatter(filepath.Join(d.Dir, e.Name(), "SKILL.md"))
			if !ok {
				continue
			}
			if name == "" {
				name = e.Name()
			}
			name = d.Prefix + name
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, Skill{Name: name, Description: clipHead(desc, maxSkillDescription), Source: d.Source})
		}
	}
	return out
}

// readSkillFrontmatter reads name and description from a SKILL.md's YAML
// frontmatter. ok is false when the file is missing.
func readSkillFrontmatter(path string) (name, desc string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	raw, _ := io.ReadAll(io.LimitReader(f, maxSkillFrontmatter))
	raw = bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", true
	}
	end := strings.Index(text[4:], "\n---")
	if end < 0 {
		return "", "", true
	}
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	_ = yaml.Unmarshal([]byte(text[4:4+end]), &fm)
	return strings.TrimSpace(fm.Name), strings.Join(strings.Fields(fm.Description), " "), true
}

// SkillQuestion is the choice question over skills plus NoSkill.
func SkillQuestion(skills []Skill) Question {
	crit := make(map[string]string, len(skills)+1)
	for _, s := range skills {
		d := s.Description
		if d == "" {
			d = s.Name
		}
		crit[s.Name] = d
	}
	crit[NoSkill] = "No listed skill clearly fits this request; it is ordinary work or a question."
	return Question{
		Type:         "choice",
		Instructions: "Which installed skill, if any, should be used for this user request? Pick none unless one clearly fits.",
		Criteria:     crit,
	}
}

// SkillPick is the picker's outcome for one prompt.
type SkillPick struct {
	Pick       string
	Confidence float64
	// Apply is set in enforce mode when the pick is a real skill and the
	// confidence is at least the block threshold.
	Apply bool
	Err   error
}

// PickSkill asks Jev which skill fits prompt. It never returns an error: a
// failed call is a pick with Err set that applies nothing.
func PickSkill(ctx context.Context, c *Client, p Policy, prompt string, skills []Skill) SkillPick {
	var out SkillPick
	resp, err := c.Ask(ctx, map[string]string{"user_request": clipHead(prompt, maxSkillPrompt)},
		map[string]Question{"skill": SkillQuestion(skills)})
	if err != nil {
		out.Err = err
		return out
	}
	a, ok := resp.Answers["skill"]
	valid := a.Choice == NoSkill
	for _, s := range skills {
		if s.Name == a.Choice {
			valid = true
		}
	}
	if !ok || !valid {
		out.Err = errMissingSkill
		return out
	}
	out.Pick, out.Confidence = a.Choice, a.Confidence
	block, _ := p.Thresholds()
	out.Apply = p.Enforcing() && a.Choice != NoSkill && a.Confidence >= block
	return out
}

var errMissingSkill = &routeErr{"jev: no usable skill answer"}
