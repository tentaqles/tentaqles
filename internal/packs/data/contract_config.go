package data

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/packs/scan"
	"gopkg.in/yaml.v3"
)

// contractDefaultPaths is packs.data.contracts.paths when unset. "**" also
// matches no directory, so contracts/ and a top-level file both match.
var contractDefaultPaths = []string{"contracts/**/*.odcs.yaml", "**/*.odcs.yaml", "**/*.odcs.yml"}

// contractMaxFiles bounds how many contract files one run reads.
const contractMaxFiles = 2000

// contractMaxBytes caps one contract read.
const contractMaxBytes = 4 << 20

// validateContractsKey validates packs.data.contracts.
func validateContractsKey(n yaml.Node) error {
	if n.Kind == yaml.ScalarNode && n.Tag == "!!null" {
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a mapping (paths, enum_added, consumers)", n.Line)
	}
	for _, kv := range scan.Entries(&n) {
		switch kv.Key {
		case "paths":
			if kv.Value.Kind != yaml.SequenceNode {
				return fmt.Errorf("line %d: paths must be a list of globs", kv.Line)
			}
			for _, it := range kv.Value.Content {
				if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
					return fmt.Errorf("line %d: paths entries must be non-empty globs", it.Line)
				}
			}
		case "enum_added":
			if v := scan.Scalar(kv.Value); v != "warn" && v != "breaking" {
				return fmt.Errorf("line %d: enum_added must be warn or breaking", kv.Line)
			}
		case "consumers":
			if kv.Value.Kind != yaml.MappingNode {
				return fmt.Errorf("line %d: consumers must map a contract id to a list of consumer names", kv.Line)
			}
			for _, c := range scan.Entries(kv.Value) {
				if c.Value.Kind != yaml.SequenceNode {
					return fmt.Errorf("line %d: consumers.%s must be a list of names", c.Line, c.Key)
				}
				for _, it := range c.Value.Content {
					if it.Kind != yaml.ScalarNode || strings.TrimSpace(it.Value) == "" {
						return fmt.Errorf("line %d: consumers.%s entries must be names", it.Line, c.Key)
					}
				}
			}
		default:
			return fmt.Errorf("line %d: unknown key %q (known: paths, enum_added, consumers)", kv.Line, kv.Key)
		}
	}
	return nil
}

// contractPaths is the configured contract globs (the default when unset).
func contractPaths(s packs.Settings) []string {
	if ps := s.Strings("contracts.paths"); len(ps) > 0 {
		return ps
	}
	return contractDefaultPaths
}

// contractEnumAddedBreaking reports contracts.enum_added: breaking.
func contractEnumAddedBreaking(s packs.Settings) bool {
	return s.String("contracts.enum_added") == "breaking"
}

// contractConsumers returns the consumers listed for a contract, by its id,
// then its name, then the file's base name without .odcs.yaml.
func contractConsumers(s packs.Settings, c *odcsContract, file string) []string {
	keys := []string{c.ID, c.Name, contractBase(file)}
	for _, k := range keys {
		if k == "" {
			continue
		}
		if v := s.Strings("contracts.consumers." + k); len(v) > 0 {
			return v
		}
	}
	// A key with a dot can't be reached by a dotted path: walk the mapping.
	n := s.Node("contracts.consumers")
	for _, kv := range scan.Entries(n) {
		for _, k := range keys {
			if k != "" && kv.Key == k {
				return scan.Strings(kv.Value)
			}
		}
	}
	return nil
}

func contractBase(file string) string {
	b := path.Base(file)
	for _, suf := range []string{".odcs.yaml", ".odcs.yml", ".yaml", ".yml"} {
		if strings.HasSuffix(b, suf) {
			return strings.TrimSuffix(b, suf)
		}
	}
	return b
}

func isContractPath(s packs.Settings, p string) bool {
	for _, g := range contractPaths(s) {
		if packs.MatchGlob(g, p) {
			return true
		}
	}
	return false
}

// contractChanges is the changed set restricted to contract files: added,
// modified, renamed (either side a contract path) and deleted.
func contractChanges(in *packs.Input) []packs.ChangedFile {
	s := in.Settings()
	var out []packs.ChangedFile
	for _, c := range in.Changed {
		if isContractPath(s, c.Path) || (c.OldPath != "" && isContractPath(s, c.OldPath)) {
			out = append(out, c)
		}
	}
	return out
}

// anyContractChange is the cheap Applies of the contract checks.
func anyContractChange(in *packs.Input) bool { return len(contractChanges(in)) > 0 }

// allContractFiles lists every contract file on the new side: the tracked
// files plus the changed ones, minus deletions, bounded at contractMaxFiles.
func allContractFiles(in *packs.Input) []string {
	s := in.Settings()
	set := map[string]bool{}
	if in.Git != nil {
		if files, err := in.Git.ListFiles(); err == nil {
			for _, f := range files {
				if isContractPath(s, f) {
					set[f] = true
				}
			}
		}
	}
	for _, c := range in.Changed {
		if c.Status == 'D' {
			delete(set, c.Path)
			continue
		}
		if c.Status == 'R' && c.OldPath != "" {
			delete(set, c.OldPath)
		}
		if isContractPath(s, c.Path) {
			set[c.Path] = true
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	if len(out) > contractMaxFiles {
		out = out[:contractMaxFiles]
	}
	return out
}

// readContract reads and parses the new side of a contract file.
func readContract(in *packs.Input, p string) (*odcsContract, error) {
	b, err := in.ReadNew(p, contractMaxBytes)
	if err != nil {
		return nil, err
	}
	return parseContract(p, b)
}

// readOldContract reads and parses the old side (Base) of a contract file.
// A failure to read it (no GitReader, a git error or timeout) wraps
// errContractOldRead; a YAML error on a read file does not.
func readOldContract(in *packs.Input, p string) (*odcsContract, []byte, error) {
	if in.Base == "" || in.Git == nil {
		return nil, nil, fmt.Errorf("%w: no old side", errContractOldRead)
	}
	b, err := in.Git.Show(in.Base, p)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", errContractOldRead, err)
	}
	c, err := parseContract(p, b)
	return c, b, err
}

var errContractOldRead = errors.New("cannot read the old side")

func contractPlural(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	if strings.HasSuffix(one, "y") {
		return fmt.Sprintf("%d %sies", n, strings.TrimSuffix(one, "y"))
	}
	return fmt.Sprintf("%d %ss", n, one)
}
