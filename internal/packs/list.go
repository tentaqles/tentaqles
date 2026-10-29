package packs

import (
	"encoding/json"
	"io"
)

// ListCheck is one check row of tq pack list.
type ListCheck struct {
	ID       string `json:"id"`
	Kind     Kind   `json:"kind"`
	Tool     string `json:"tool,omitempty"`
	Required bool   `json:"required"`
}

// ListPack is one pack of tq pack list.
type ListPack struct {
	Name    string         `json:"name"`
	Enabled bool           `json:"enabled"`
	Tools   []string       `json:"tools"`
	Config  map[string]any `json:"config"`
	Checks  []ListCheck    `json:"checks"`
}

// ListReport is the --json output of tq pack list (schema tq.packs/v1). Its
// config_hash is the same hash tq pack check reports; the build gate
// freezes it at plan approval.
type ListReport struct {
	Schema     string     `json:"schema"`
	Workspace  string     `json:"workspace"`
	ConfigHash string     `json:"config_hash"`
	Packs      []ListPack `json:"packs"`
	Problems   []string   `json:"problems,omitempty"`
}

// List builds the report; enabledOnly keeps the enabled packs.
func List(cfg Config, workspace string, enabledOnly bool) ListReport {
	rep := ListReport{Schema: Schema, Workspace: workspace, ConfigHash: cfg.Hash(), Packs: []ListPack{}}
	canon := cfg.Canonical()
	for _, n := range Names() {
		s := cfg.Pack(n)
		if enabledOnly && !s.Enabled {
			continue
		}
		p := ListPack{Name: n, Enabled: s.Enabled, Tools: append([]string{}, s.Tools...), Checks: []ListCheck{}}
		if c, ok := canon[n].(map[string]any); ok {
			p.Config = c
		} else {
			p.Config = map[string]any{}
		}
		for _, c := range Checks(n) {
			req := c.Kind == Builtin || s.Declared(c.Tool)
			p.Checks = append(p.Checks, ListCheck{ID: c.ID, Kind: c.Kind, Tool: c.Tool, Required: req})
		}
		rep.Packs = append(rep.Packs, p)
	}
	for _, pr := range cfg.Problems {
		rep.Problems = append(rep.Problems, pr.String())
	}
	return rep
}

// EncodeListJSON writes the list report with a two-space indent.
func EncodeListJSON(w io.Writer, r ListReport) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}
