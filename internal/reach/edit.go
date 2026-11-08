package reach

import (
	"bytes"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"gopkg.in/yaml.v3"
)

// AnswersFrom is the form's view of an existing client's manifest: what reach would have asked to
// write it. Scope ids the form does not offer (a provider added by hand) are not answers; Edit leaves
// them where they are.
func AnswersFrom(m *manifest.Manifest) *Answers {
	a := &Answers{Email: m.Git.Email, Git: strings.ToLower(strings.TrimSpace(m.Git.Provider))}
	if !slices.ContainsFunc(GitOptions, func(o Option) bool { return o.Value == a.Git }) {
		a.Git = ""
	}
	if p, _ := m.Cloud["provider"].(string); p != "" && p != "none" {
		a.Clouds = append(a.Clouds, manifest.CloudProviderName(p))
	}
	if also, ok := m.Cloud["also"].([]any); ok {
		for _, v := range also {
			if s, ok := v.(string); ok {
				a.Clouds = append(a.Clouds, manifest.CloudProviderName(s))
			}
		}
	}
	for _, id := range m.ExtraScope {
		switch {
		case isOption(TeamOptions, id):
			a.Team = append(a.Team, id)
		case isOption(DataOptions, id):
			a.Data = append(a.Data, id)
		case isOption(TaskOptions, id) && a.Task == "":
			a.Task = id
		}
	}
	if m.Email.Enabled() {
		a.EmailProvider = m.Email.Provider
	}
	a.Browsers = slices.Clone(m.Browsers)
	a.Other = slices.Clone(m.OtherProviders)
	return a
}

func isOption(opts []Option, v string) bool {
	return slices.ContainsFunc(opts, func(o Option) bool { return o.Value == v })
}

// formScopeIDs are the scope: ids the form owns: the team, data and task choices.
func formScopeIDs() []string {
	var out []string
	for _, o := range slices.Concat(TeamOptions, DataOptions, TaskOptions) {
		out = append(out, o.Value)
	}
	return out
}

// providerIDs are the catalog ids the answers choose: the git host's CLI, each cloud's CLI and the
// scope ids. claude is not among them.
func (a *Answers) providerIDs() []string {
	var out []string
	add := func(id string) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	if a.Git != "" {
		add(gitCLI[a.Git])
	}
	for _, c := range a.Clouds {
		add(cloudCLI[c])
	}
	for _, id := range a.scopeIDs() {
		add(id)
	}
	sort.Strings(out)
	return out
}

func (a *Answers) scopeIDs() []string {
	var out []string
	for _, id := range slices.Concat(a.Team, a.Data, []string{a.Task}) {
		if id != "" && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// Edit is the manifest raw with what the form owns changed from before to after: git.email and
// git.provider, the identities the providers bring, the cloud block's provider and also, the form's ids
// in scope:, email.provider, browsers: and other_providers:. Every other key, value and comment stays.
// drop names providers to take out of the client that no answer owns (an identity or a scope: entry
// listed by hand): their identity entry and their scope: entry go. It returns the new bytes, one line per change, and the identities added and removed. No change
// gives raw back and no lines. The new manifest is checked by manifest.Parse before it is returned.
func Edit(raw []byte, before, after *Answers, drop ...string) (out []byte, changes, added, removed []string, err error) {
	if err := after.Validate(); err != nil {
		return nil, nil, nil, nil, err
	}
	if _, err := manifest.Parse(raw, "the manifest"); err != nil {
		return nil, nil, nil, nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, nil, nil, nil, err
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, nil, nil, nil, fmt.Errorf("the manifest is not a YAML mapping")
	}
	top := doc.Content[0]
	cat := providers.MustLoad()

	if strings.TrimSpace(after.Email) != strings.TrimSpace(before.Email) {
		setScalar(mapping(top, "git"), "email", strings.TrimSpace(after.Email))
		changes = append(changes, fmt.Sprintf("email: %s -> %s", before.Email, strings.TrimSpace(after.Email)))
	}
	if after.Git != before.Git {
		if after.Git == "" {
			deleteKey(mapping(top, "git"), "provider")
		} else {
			setScalar(mapping(top, "git"), "provider", after.Git)
		}
		changes = append(changes, fmt.Sprintf("git provider: %s -> %s", orNone(before.Git), orNone(after.Git)))
	}

	// Identities follow only the providers whose choice changed: a provider no longer chosen loses its
	// identity entry (else the identity alone would keep it in the scope), and a newly chosen one with
	// a login of its own gains one. claude, and every provider the form does not own, stay as they are.
	oldIDs, newIDs := before.providerIDs(), after.providerIDs()
	idents := mapping(top, "identities")
	for _, id := range drop {
		dropped := false
		if lookup(idents, id) != nil {
			deleteKey(idents, id)
			removed = append(removed, id)
			dropped = true
		}
		if s := lookup(top, "scope"); s != nil && s.Kind == yaml.SequenceNode {
			kept := s.Content[:0]
			for _, e := range s.Content {
				if e.Value == id {
					dropped = true
					continue
				}
				kept = append(kept, e)
			}
			s.Content = kept
			if len(s.Content) == 0 {
				deleteKey(top, "scope")
			}
		}
		if !dropped {
			return nil, nil, nil, nil, fmt.Errorf("%s is not among this client's identities or its scope: list", id)
		}
		changes = append(changes, "removed "+id)
	}
	for _, id := range oldIDs {
		if !slices.Contains(newIDs, id) && lookup(idents, id) != nil {
			deleteKey(idents, id)
			removed = append(removed, id)
		}
	}
	for _, id := range newIDs {
		if slices.Contains(oldIDs, id) || lookup(idents, id) != nil {
			continue
		}
		if p, ok := cat.Get(id); ok && p.HasIdentity() {
			added = append(added, id)
			setNode(idents, id, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Style: yaml.FlowStyle})
		}
	}

	if !slices.Equal(before.Clouds, after.Clouds) {
		cloud := mapping(top, "cloud")
		deleteKey(cloud, "provider")
		deleteKey(cloud, "also")
		if len(after.Clouds) > 0 {
			setScalar(cloud, "provider", after.Clouds[0])
			if len(after.Clouds) > 1 {
				setNode(cloud, "also", stringSeq(after.Clouds[1:]))
			}
		}
		if len(cloud.Content) == 0 {
			deleteKey(top, "cloud")
		}
		changes = append(changes, fmt.Sprintf("clouds: %s -> %s", list(before.Clouds), list(after.Clouds)))
	}

	if b, a := before.scopeIDs(), after.scopeIDs(); !slices.Equal(b, a) {
		var keep []string
		if s := lookup(top, "scope"); s != nil {
			for _, n := range s.Content {
				if !slices.Contains(formScopeIDs(), n.Value) {
					keep = append(keep, n.Value)
				}
			}
		}
		scope := slices.Concat(keep, a)
		sort.Strings(scope)
		if len(scope) == 0 {
			deleteKey(top, "scope")
		} else {
			setNode(top, "scope", stringSeq(scope))
		}
		changes = append(changes, fmt.Sprintf("team, data and task providers: %s -> %s", list(b), list(a)))
	}

	if after.EmailProvider != before.EmailProvider {
		if after.EmailProvider == "" {
			deleteKey(top, "email")
		} else {
			setScalar(mapping(top, "email"), "provider", after.EmailProvider)
		}
		changes = append(changes, fmt.Sprintf("email provider: %s -> %s", orNone(before.EmailProvider), orNone(after.EmailProvider)))
	}

	if !slices.Equal(before.Browsers, after.Browsers) {
		if len(after.Browsers) == 0 {
			deleteKey(top, "browsers")
		} else {
			var n yaml.Node
			if err := n.Encode(after.Browsers); err != nil {
				return nil, nil, nil, nil, err
			}
			setNode(top, "browsers", &n)
		}
		changes = append(changes, fmt.Sprintf("browsers: %s -> %s", browsers(before.Browsers), browsers(after.Browsers)))
	}

	if !slices.Equal(before.Other, after.Other) {
		if len(after.Other) == 0 {
			deleteKey(top, "other_providers")
		} else {
			setNode(top, "other_providers", stringSeq(after.Other))
		}
		changes = append(changes, fmt.Sprintf("providers tentaqles does not manage: %s -> %s", list(before.Other), list(after.Other)))
	}

	if len(changes) == 0 {
		return raw, nil, nil, nil, nil
	}
	if len(added) > 0 {
		changes = append(changes, "identities added: "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		changes = append(changes, "identities removed: "+strings.Join(removed, ", "))
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(&doc); err != nil {
		return nil, nil, nil, nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, nil, nil, nil, err
	}
	if _, err := manifest.Parse(buf.Bytes(), "the edited manifest"); err != nil {
		return nil, nil, nil, nil, err
	}
	return buf.Bytes(), changes, added, removed, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

func browsers(bs []manifest.Browser) string {
	if len(bs) == 0 {
		return "none"
	}
	var out []string
	for _, b := range bs {
		out = append(out, b.Name+" ("+b.Profile+")")
	}
	return strings.Join(out, ", ")
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

// mapping is m's mapping value for key, made (block style) when absent or empty.
func mapping(m *yaml.Node, key string) *yaml.Node {
	if v := lookup(m, key); v != nil && v.Kind == yaml.MappingNode {
		return v
	}
	v := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setNode(m, key, v)
	return v
}

func setNode(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v.HeadComment, v.LineComment = m.Content[i+1].HeadComment, m.Content[i+1].LineComment
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, v)
}

func setScalar(m *yaml.Node, key, value string) {
	setNode(m, key, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

func deleteKey(m *yaml.Node, key string) {
	if m == nil {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

func stringSeq(xs []string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, x := range xs {
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: x})
	}
	return n
}
