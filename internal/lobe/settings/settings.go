// Package settings reads and edits the lobe's nine settings, kept under the lobe key of a client's manifest
// (core page 5.6, F-06). The values, defaults and wrong ("safe") values are F-06's table. A lobe package may
// import only internal/resolve, internal/paths, internal/registry, internal/manifest and other lobe packages
// (test/integration/lobe_imports_test.go), which is why the edit helpers are local copies of reach's.
package settings

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"gopkg.in/yaml.v3"
)

// The nine keys, in the order `tentaqles lobe settings` prints them.
const (
	Capture       = "capture"
	Recall        = "recall"
	Lessons       = "lessons"
	Graph         = "graph"
	Enforce       = "enforce"
	Share         = "share"
	Model         = "model"
	Documents     = "documents"
	RetentionDays = "retention_days"
)

// Source says where a value came from.
const (
	SourceDefault  = "default"  // the key is absent, or the lobe key is absent or empty
	SourceManifest = "manifest" // the key holds a value from the list
	SourceInvalid  = "invalid"  // the key holds something outside the list; the value is the safe value
)

// RetentionMin and RetentionMax bound retention_days. The range is proposed in F-06 and not yet agreed.
const (
	RetentionMin = 1
	RetentionMax = 365
)

var order = []string{Capture, Recall, Lessons, Graph, Enforce, Share, Model, Documents, RetentionDays}

type spec struct {
	onOff   bool     // a bare on/off key: true, yes, false and no are read too
	allowed []string // the list of values; nil for retention_days
	def     string
	safe    string // the value used for a wrong value (F-06 step 1, "Wrong value")
}

var specs = map[string]spec{
	Capture:       {onOff: true, def: "on", safe: "off"},
	Recall:        {onOff: true, def: "on", safe: "off"},
	Lessons:       {onOff: true, def: "on", safe: "off"},
	Graph:         {onOff: true, def: "on", safe: "off"},
	Enforce:       {allowed: []string{"off", "advice", "block"}, def: "advice", safe: "off"},
	Share:         {allowed: []string{"private", "shared"}, def: "private", safe: "private"},
	Model:         {onOff: true, def: "on", safe: "off"},
	Documents:     {onOff: true, def: "off", safe: "off"},
	RetentionDays: {def: "14", safe: "14"},
}

// needs lists the keys that must be on for a key to act (F-06: documents needs model and graph).
var needs = map[string][]string{
	Documents: {Model, Graph},
}

// isDigits reports whether s is a non-empty run of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// Keys returns the nine keys in print order.
func Keys() []string { return slices.Clone(order) }

// Known reports whether key is one of the nine.
func Known(key string) bool { _, ok := specs[key]; return ok }

// Allowed returns the values of an on/off or list key, in the order F-06 gives them. It returns nil for
// retention_days, which takes a whole number (see Hint) and for an unknown key.
func Allowed(key string) []string {
	if !Known(key) || key == RetentionDays {
		return nil
	}
	if specs[key].onOff {
		return []string{"on", "off"}
	}
	return slices.Clone(specs[key].allowed)
}

// Hint describes the values of a key in words, for a message.
func Hint(key string) string {
	if key == RetentionDays {
		return fmt.Sprintf("a whole number from %d to %d", RetentionMin, RetentionMax)
	}
	return strings.Join(Allowed(key), ", ")
}

// Default is the value of a key when the manifest does not set it.
func Default(key string) string { return specs[key].def }

// Safe is the value a key takes when the manifest holds a wrong value.
func Safe(key string) string { return specs[key].safe }

// canonical reads a raw manifest value as the lobe reads it: on/off bare or quoted, true/false/yes/no for
// on/off keys, case and surrounding spaces ignored. ok is false when the value is outside the key's list.
func canonical(key, raw string) (string, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	sp := specs[key]
	switch {
	case sp.onOff:
		switch s {
		case "on", "true", "yes":
			return "on", true
		case "off", "false", "no":
			return "off", true
		}
		return "", false
	case key == RetentionDays:
		if !isDigits(s) {
			return "", false
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < RetentionMin || n > RetentionMax {
			return "", false
		}
		return strconv.Itoa(n), true
	}
	return s, slices.Contains(sp.allowed, s)
}

// ValidValue reports whether value is written exactly as the lobe writes it: one of the key's values, with
// no alias and no case change. Edit accepts only these.
func ValidValue(key, value string) bool {
	if !Known(key) {
		return false
	}
	c, ok := canonical(key, value)
	return ok && c == value
}

// Setting is one key with its effective value and where the value came from.
type Setting struct {
	Key    string
	Value  string
	Source string // SourceDefault, SourceManifest or SourceInvalid
}

// Settings are the nine settings in print order.
type Settings []Setting

// From reads the lobe settings of the workspace's manifest. A nil workspace or manifest gives the defaults.
func From(ws *resolve.Workspace) Settings {
	if ws == nil {
		return FromManifest(nil)
	}
	return FromManifest(ws.Manifest)
}

// FromManifest reads the lobe settings of m (nil gives the defaults).
func FromManifest(m *manifest.Manifest) Settings {
	if m == nil || m.Lobe.Kind == 0 {
		return decode(nil)
	}
	return decode(&m.Lobe)
}

// NotAMapping reports whether the lobe key of m holds something other than a mapping, such as `lobe: 7`.
// Then all nine settings read as invalid. An absent or empty lobe key is not one.
func NotAMapping(m *manifest.Manifest) bool {
	if m == nil || m.Lobe.Kind == 0 || isNull(&m.Lobe) {
		return false
	}
	return m.Lobe.Kind != yaml.MappingNode
}

// FromNode reads the lobe settings from a lobe node as it is in the manifest: the value under the lobe
// key, or nil when there is none.
func FromNode(n *yaml.Node) Settings { return decode(n) }

func decode(n *yaml.Node) Settings {
	absent := n == nil || isNull(n)
	notMapping := !absent && n.Kind != yaml.MappingNode
	out := make(Settings, 0, len(order))
	for _, key := range order {
		s := Setting{Key: key, Value: Default(key), Source: SourceDefault}
		switch {
		case notMapping:
			s = Setting{Key: key, Value: Safe(key), Source: SourceInvalid}
		case absent:
		default:
			if v := lookup(n, key); v != nil {
				c, ok := canonical(key, scalarText(v))
				if ok && v.Kind == yaml.ScalarNode && !isNull(v) {
					s = Setting{Key: key, Value: c, Source: SourceManifest}
				} else {
					s = Setting{Key: key, Value: Safe(key), Source: SourceInvalid}
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// Get returns the setting for key.
func (s Settings) Get(key string) (Setting, bool) {
	for _, x := range s {
		if x.Key == key {
			return x, true
		}
	}
	return Setting{}, false
}

// Invalid reports whether any key holds a wrong value (the doctor reports it once per client).
func (s Settings) Invalid() bool {
	for _, x := range s {
		if x.Source == SourceInvalid {
			return true
		}
	}
	return false
}

// Lines returns the text form of each setting, in print order: `key=value (source)`. A key that is on but
// cannot act because a key it needs is off says so: `documents=on (manifest, needs model)`.
func (s Settings) Lines() []string {
	out := make([]string, 0, len(s))
	for _, x := range s {
		note := x.Source
		if x.Value == "on" {
			var missing []string
			for _, dep := range needs[x.Key] {
				if d, ok := s.Get(dep); ok && d.Value != "on" {
					missing = append(missing, dep)
				}
			}
			if len(missing) > 0 {
				note += ", needs " + strings.Join(missing, ", ")
			}
		}
		out = append(out, fmt.Sprintf("%s=%s (%s)", x.Key, x.Value, note))
	}
	return out
}

// Reminders returns the lines of F-06 step 6 for a pair that surprises, from the settings as they are
// after an edit: nothing for an unremarkable set of values.
func Reminders(s Settings) []string {
	var out []string
	recall, _ := s.Get(Recall)
	enforce, _ := s.Get(Enforce)
	if recall.Value == "off" && enforce.Value == "block" {
		out = append(out, "recall is off, but enforce is block: an approved block rule still denies.")
	}
	if doc, _ := s.Get(Documents); doc.Value == "on" {
		var waiting []string
		for _, dep := range needs[Documents] {
			if d, _ := s.Get(dep); d.Value != "on" {
				waiting = append(waiting, dep)
			}
		}
		if len(waiting) > 0 {
			out = append(out, "documents is on, but no document is read while "+strings.Join(waiting, " or ")+" is off.")
		}
	}
	if enforce.Value == "block" {
		out = append(out, "enforce is block, which acts as advice until blocking ships.")
	}
	return out
}

// isNull reports whether n is an empty or null scalar (`lobe:`, `lobe: ~`, `lobe: null`).
func isNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null"
}

// scalarText is the text of a scalar node, or "" for anything else.
func scalarText(n *yaml.Node) string {
	if n.Kind != yaml.ScalarNode {
		return ""
	}
	return n.Value
}

// lookup is m's value for key, or nil.
func lookup(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
