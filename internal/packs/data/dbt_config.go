package data

import (
	"fmt"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// dbtConfig is packs.data.dbt (spec C §3.1).
type dbtConfig struct {
	Project       string   // dir holding dbt_project.yml ("" = auto-detect)
	ContractPaths []string // globs, relative to the dbt project or the repo
	NewModelTests []string // tests required on a new model's primary key
	Build         string   // off | dev
	DevTarget     string
	ProdTargets   []string
	State         string // dir with the production manifest.json
}

// Defaults of packs.data.dbt.
var (
	defaultNewModelTests = []string{"unique", "not_null"}
	defaultProdTargets   = []string{"prod", "production"}
)

const defaultDevTarget = "dev"

var dbtKeys = map[string]string{
	"project":         "scalar",
	"contract_paths":  "list",
	"new_model_tests": "list",
	"build":           "scalar",
	"dev_target":      "scalar",
	"prod_targets":    "list",
	"state":           "scalar",
}

// validateDBT is the validator of packs.data.dbt: a mapping of the known
// keys, with build off|dev and new_model_tests drawn from unique, not_null.
func validateDBT(n yaml.Node) error {
	if n.Kind == yaml.ScalarNode && (n.Tag == "!!null" || n.Value == "") {
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping", n.Line)
	}
	for _, kv := range scan.Entries(&n) {
		kind, ok := dbtKeys[kv.Key]
		if !ok {
			return fmt.Errorf("line %d: unknown key %q (known: build, contract_paths, dev_target, new_model_tests, prod_targets, project, state)", kv.Line, kv.Key)
		}
		v := kv.Value
		switch kind {
		case "scalar":
			if v.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: %s must be a string", kv.Line, kv.Key)
			}
		case "list":
			if v.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: %s must be a list", kv.Line, kv.Key)
			}
			for _, it := range v.Content {
				if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
					return fmt.Errorf("line %d: %s items must be non-empty strings", it.Line, kv.Key)
				}
			}
		}
		switch kv.Key {
		case "build":
			switch strings.ToLower(v.Value) {
			case "off", "false", "dev":
			default:
				return fmt.Errorf("line %d: build must be off or dev, got %q", kv.Line, v.Value)
			}
		case "new_model_tests":
			for _, it := range v.Content {
				if it.Value != "unique" && it.Value != "not_null" {
					return fmt.Errorf("line %d: new_model_tests items are unique or not_null, got %q", it.Line, it.Value)
				}
			}
		case "project", "state":
			if strings.Contains(strings.ReplaceAll(v.Value, `\`, "/"), "..") {
				return fmt.Errorf("line %d: %s must stay inside the repository (no ..)", kv.Line, kv.Key)
			}
		case "dev_target":
			if !targetRe.MatchString(v.Value) {
				return fmt.Errorf("line %d: dev_target %q is not a target name", kv.Line, v.Value)
			}
		case "prod_targets":
			for _, it := range v.Content {
				if !targetRe.MatchString(it.Value) {
					return fmt.Errorf("line %d: prod target %q is not a target name", it.Line, it.Value)
				}
			}
		}
	}
	return nil
}

// dbtSettings reads packs.data.dbt from the merged settings, with defaults.
func dbtSettings(s packs.Settings) dbtConfig {
	c := dbtConfig{
		Project:       strings.Trim(strings.ReplaceAll(s.String("dbt.project"), `\`, "/"), "/"),
		ContractPaths: s.Strings("dbt.contract_paths"),
		NewModelTests: s.Strings("dbt.new_model_tests"),
		Build:         strings.ToLower(s.String("dbt.build")),
		DevTarget:     s.String("dbt.dev_target"),
		ProdTargets:   s.Strings("dbt.prod_targets"),
		State:         strings.Trim(strings.ReplaceAll(s.String("dbt.state"), `\`, "/"), "/"),
	}
	if c.Project == "." {
		c.Project = ""
	}
	if len(c.NewModelTests) == 0 {
		c.NewModelTests = defaultNewModelTests
	}
	if c.DevTarget == "" {
		c.DevTarget = defaultDevTarget
	}
	if len(c.ProdTargets) == 0 {
		c.ProdTargets = defaultProdTargets
	}
	if c.Build == "false" || c.Build == "" {
		c.Build = "off"
	}
	return c
}

// isProdTarget reports whether target is one of the production targets
// (case-insensitive).
func (c dbtConfig) isProdTarget(target string) bool {
	for _, p := range c.ProdTargets {
		if strings.EqualFold(strings.TrimSpace(p), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}
