package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/tentaqles/tentaqles/internal/paths"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// Report summarizes what Sync changed.
type Report struct {
	Settings bool
	Skills   []string
	MCP      []string
	Warnings []string
}

// Options controls Sync's behavior.
type Options struct {
	Force bool
}

// Sync materializes ws's claude.bundle into its Claude identity dir:
// settings.json (enabledPlugins/extraKnownMarketplaces), skills, and
// .claude.json mcpServers, in that order, then stamps and saves state.
func Sync(ws *resolve.Workspace, cat *Catalog, o Options) (Report, error) {
	var rep Report

	if ws.Manifest == nil || ws.Manifest.Claude.Bundle == nil {
		return rep, fmt.Errorf("no claude.bundle in manifest")
	}

	d, errs := Compute(ws.Manifest.Claude.Bundle, cat)
	if len(errs) > 0 {
		msg := "bundle compute errors:"
		for _, e := range errs {
			msg += "\n  " + e.Error()
		}
		return rep, fmt.Errorf("%s", msg)
	}

	dir := paths.IdentityDir(ws.Name, "claude")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return rep, err
	}

	// Collected up front so they're reported even when a later step fails.
	rep.Warnings = cat.Validate()

	if _, err := os.Stat(filepath.Join(dir, "sessions")); err != nil {
		rep.Warnings = append(rep.Warnings,
			fmt.Sprintf("no sessions/ dir under %s; in-use check skipped", dir))
	}

	if !o.Force && InUse(dir) {
		return rep, fmt.Errorf("claude appears to be running with this config dir; close it or use --force")
	}

	st := LoadState(dir)

	changedSettings, err := SyncSettings(dir, d)
	if err != nil {
		return rep, err
	}
	rep.Settings = changedSettings

	skillsChanged, err := SyncSkills(dir, d, &st)
	if err != nil {
		return rep, err
	}
	rep.Skills = skillsChanged

	// Persist skill ownership before touching MCP: if SyncMCP fails, the
	// skills tq just installed are still recorded as ours.
	st.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	if err := st.Save(dir); err != nil {
		return rep, err
	}

	mcpChanged, err := SyncMCP(dir, d, &st)
	if err != nil {
		return rep, err
	}
	rep.MCP = mcpChanged

	st.SyncedAt = time.Now().UTC().Format(time.RFC3339)
	if err := st.Save(dir); err != nil {
		return rep, err
	}

	return rep, nil
}

// Drift describes one difference between a workspace's desired bundle
// state and what's actually on disk.
type Drift struct {
	Kind, Name, Detail string
}

// Diff reports drift between ws's desired bundle state and its Claude
// identity dir, without modifying anything.
func Diff(ws *resolve.Workspace, cat *Catalog) ([]Drift, error) {
	if ws.Manifest == nil || ws.Manifest.Claude.Bundle == nil {
		return nil, fmt.Errorf("no claude.bundle in manifest")
	}

	d, errs := Compute(ws.Manifest.Claude.Bundle, cat)
	if len(errs) > 0 {
		msg := "bundle compute errors:"
		for _, e := range errs {
			msg += "\n  " + e.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}

	dir := paths.IdentityDir(ws.Name, "claude")
	st := LoadState(dir)

	var drifts []Drift

	// settings.json: enabledPlugins + extraKnownMarketplaces
	settingsPath := filepath.Join(dir, "settings.json")
	current, err := ReadJSONMap(settingsPath)
	if err != nil {
		return nil, err
	}

	currentEnabled := map[string]bool{}
	if raw, ok := current["enabledPlugins"].(map[string]any); ok {
		for k, v := range raw {
			if b, ok := v.(bool); ok && b {
				currentEnabled[k] = true
			}
		}
	}
	for name := range d.EnabledPlugins {
		if !currentEnabled[name] {
			drifts = append(drifts, Drift{Kind: "plugin-missing", Name: name})
		}
	}
	for name := range currentEnabled {
		if !d.EnabledPlugins[name] {
			drifts = append(drifts, Drift{Kind: "plugin-extra", Name: name})
		}
	}

	currentMkts := map[string]bool{}
	if raw, ok := current["extraKnownMarketplaces"].(map[string]any); ok {
		for k := range raw {
			currentMkts[k] = true
		}
	}
	for name := range d.Marketplaces {
		if !currentMkts[name] {
			drifts = append(drifts, Drift{Kind: "marketplace-missing", Name: name})
		}
	}

	// skills
	for name, src := range d.Skills {
		dst := filepath.Join(dir, "skills", name)
		markerRaw, err := os.ReadFile(filepath.Join(dst, skillMarkerName))
		if err != nil {
			drifts = append(drifts, Drift{Kind: "skill-missing", Name: name, Detail: "not present"})
			continue
		}
		wantSum, err := fingerprintDir(src)
		if err != nil {
			return nil, err
		}
		_, gotSha, ok := parseSkillMarker(markerRaw)
		if !ok || gotSha != wantSum {
			drifts = append(drifts, Drift{Kind: "skill-missing", Name: name, Detail: "content differs"})
		}
	}
	for _, name := range st.Skills {
		if _, ok := d.Skills[name]; ok {
			continue
		}
		dst := filepath.Join(dir, "skills", name)
		if _, err := os.Stat(filepath.Join(dst, skillMarkerName)); err == nil {
			drifts = append(drifts, Drift{Kind: "skill-extra", Name: name})
		}
	}

	// mcp servers in .claude.json
	claudeJSONPath := filepath.Join(dir, ".claude.json")
	root, err := ReadJSONMap(claudeJSONPath)
	if err != nil {
		return nil, err
	}
	servers := map[string]any{}
	if raw, ok := root["mcpServers"].(map[string]any); ok {
		servers = raw
	}
	for name, spec := range d.MCP {
		want := deepCopy(spec)
		got, exists := servers[name]
		if !exists {
			drifts = append(drifts, Drift{Kind: "mcp-missing", Name: name, Detail: "not present"})
			continue
		}
		if !reflect.DeepEqual(normalizeJSON(got), normalizeJSON(want)) {
			drifts = append(drifts, Drift{Kind: "mcp-missing", Name: name, Detail: "content differs"})
		}
	}
	for _, name := range st.MCP {
		if _, ok := d.MCP[name]; ok {
			continue
		}
		if _, exists := servers[name]; exists {
			drifts = append(drifts, Drift{Kind: "mcp-extra", Name: name})
		}
	}

	return drifts, nil
}
