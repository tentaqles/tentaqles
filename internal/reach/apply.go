package reach

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
)

// The categories `tentaqles add` and `tentaqles remove` take: the parts of reach's form, one at a time.
const (
	CatEmail    = "email"    // the identity's git email (one value)
	CatLanguage = "language" // the language the client works in (one value)
	CatGit      = "git"      // the git host (one value)
	CatCloud    = "cloud"    // the clouds (a list; the first is the primary)
	CatTeam     = "team"     // team and meeting platforms (a list)
	CatData     = "data"     // data platforms (a list)
	CatAgent    = "agent"    // AI coding agents besides Claude Code (a list)
	CatTask     = "task"     // the task management provider (one value)
	CatMail     = "mail"     // the email provider (one value)
	CatBrowser  = "browser"  // browsers, each with a profile (a list)
	CatOther    = "other"    // providers tentaqles does not manage (a list of names)
)

// Categories are the category names, in the form's order.
var Categories = []string{CatEmail, CatLanguage, CatGit, CatCloud, CatTeam, CatData, CatAgent, CatTask, CatMail, CatBrowser, CatOther}

// AgentOptions are the AI coding agents a client can use besides Claude Code: the catalog's agent
// providers, by id, except claude (every client has Claude Code).
func AgentOptions() []Option {
	var out []Option
	for _, p := range providers.MustLoad().ByCategory(CatAgent) {
		if p.ID != "claude" {
			out = append(out, Option{p.Name, p.ID})
		}
	}
	return out
}

// aliases spell a value the way people say it: the CLI's name for a git host or a cloud.
var aliases = map[string]map[string]string{
	CatGit:   {"gh": "github", "glab": "gitlab", "ado": "azure-devops", "azdo": "azure-devops"},
	CatCloud: {"az": "azure", "gcloud": "gcp", "google": "gcp", "amazon": "aws"},
	CatMail:  {"google": "gmail", "microsoft": "outlook"},
}

// OptionsOf are the values a category offers; nil for email, browser and other, which take free
// values (an address, a browser and its profile, a name).
func OptionsOf(cat string) []Option {
	switch cat {
	case CatGit:
		return GitOptions
	case CatCloud:
		return CloudOptions
	case CatTeam:
		return TeamOptions
	case CatData:
		return DataOptions
	case CatAgent:
		return AgentOptions()
	case CatTask:
		return TaskOptions
	case CatMail:
		return EmailOptions
	case CatBrowser:
		return BrowserOptions
	}
	return nil
}

// Single reports whether a category holds one value: adding replaces it.
func Single(cat string) bool {
	return cat == CatEmail || cat == CatLanguage || cat == CatGit || cat == CatTask || cat == CatMail
}

// value is v in cat's spelling (an alias resolved), or an error naming the values cat offers.
func value(cat, v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if a, ok := aliases[cat][v]; ok {
		v = a
	}
	opts := OptionsOf(cat)
	if isOption(opts, v) {
		return v, nil
	}
	var names []string
	for _, o := range opts {
		names = append(names, o.Value)
	}
	return "", fmt.Errorf("%s: %q is not one of %s", cat, v, strings.Join(names, ", "))
}

// Locate finds the category of a bare value (`tentaqles remove gh`, `tentaqles add aws`): the first
// category that offers it, aliases included. ok is false when none does.
func Locate(v string) (cat, val string, ok bool) {
	for _, c := range []string{CatGit, CatCloud, CatTeam, CatData, CatAgent, CatTask, CatMail, CatBrowser} {
		if got, err := value(c, v); err == nil {
			return c, got, true
		}
	}
	return "", "", false
}

// Change is the answers after `tentaqles add` (add true) or `tentaqles remove` of values in cat,
// starting from cur. For remove of a bare provider id no answer holds (an identity or a scope: entry
// listed by hand), it returns that id in drop, for Edit. It never changes cur.
func Change(cur *Answers, add bool, cat string, values []string) (next *Answers, drop []string, err error) {
	n := *cur
	n.Clouds, n.Team, n.Data, n.Agent = slices.Clone(cur.Clouds), slices.Clone(cur.Team), slices.Clone(cur.Data), slices.Clone(cur.Agent)
	n.Browsers, n.Other = slices.Clone(cur.Browsers), slices.Clone(cur.Other)
	next = &n

	switch cat {
	case CatEmail:
		if !add {
			return nil, nil, fmt.Errorf("a client always has an email: change it with tentaqles add email <address>")
		}
		if len(values) != 1 {
			return nil, nil, fmt.Errorf("email takes one address")
		}
		if err := ValidateEmail(values[0]); err != nil {
			return nil, nil, err
		}
		n.Email = strings.TrimSpace(values[0])
	case CatLanguage:
		if !add {
			return nil, nil, fmt.Errorf("a client always has a language: change it with tentaqles add language <name or tag>")
		}
		if len(values) == 0 {
			return nil, nil, fmt.Errorf("name the language: tentaqles add language pt-BR, or tentaqles add language portuguese")
		}
		l, err := manifest.FindLanguage(strings.Join(values, " "))
		if err != nil {
			return nil, nil, err
		}
		n.Language = l.Tag
	case CatGit, CatTask, CatMail:
		field := map[string]*string{CatGit: &n.Git, CatTask: &n.Task, CatMail: &n.EmailProvider}[cat]
		if add {
			if len(values) != 1 {
				return nil, nil, fmt.Errorf("%s holds one value: name it", cat)
			}
			v, err := value(cat, values[0])
			if err != nil {
				return nil, nil, err
			}
			*field = v
			break
		}
		if len(values) > 1 {
			return nil, nil, fmt.Errorf("%s holds one value", cat)
		}
		if *field == "" {
			return nil, nil, fmt.Errorf("this client has no %s provider", cat)
		}
		if len(values) == 1 {
			v, err := value(cat, values[0])
			if err != nil {
				return nil, nil, err
			}
			if v != *field {
				return nil, nil, fmt.Errorf("%s is %s, not %s", cat, *field, v)
			}
		}
		*field = ""
	case CatCloud, CatTeam, CatData, CatAgent:
		field := map[string]*[]string{CatCloud: &n.Clouds, CatTeam: &n.Team, CatData: &n.Data, CatAgent: &n.Agent}[cat]
		if len(values) == 0 {
			return nil, nil, fmt.Errorf("name the %s providers to %s", cat, verb(add))
		}
		for _, raw := range values {
			v, err := value(cat, raw)
			if err != nil {
				return nil, nil, err
			}
			has := slices.Contains(*field, v)
			switch {
			case add && !has:
				*field = append(*field, v)
			case !add && has:
				*field = slices.DeleteFunc(*field, func(x string) bool { return x == v })
			case !add:
				return nil, nil, fmt.Errorf("%s: this client has no %s", cat, v)
			}
		}
	case CatBrowser:
		if len(values) == 0 {
			return nil, nil, fmt.Errorf("name the browser (chrome or edge)")
		}
		name, err := value(cat, values[0])
		if err != nil {
			return nil, nil, err
		}
		i := slices.IndexFunc(n.Browsers, func(b manifest.Browser) bool { return b.Name == name })
		if add {
			if len(values) > 2 {
				return nil, nil, fmt.Errorf("browser takes a name and a profile: tentaqles add browser chrome \"Profile 2\"")
			}
			profile := "Default"
			if len(values) == 2 {
				profile = strings.TrimSpace(values[1])
			}
			if err := ValidateName(profile); err != nil {
				return nil, nil, fmt.Errorf("browser profile %q: %w", profile, err)
			}
			b := manifest.Browser{Name: name, Profile: profile}
			if i >= 0 {
				n.Browsers[i] = b
			} else {
				n.Browsers = append(n.Browsers, b)
			}
			break
		}
		if i < 0 {
			return nil, nil, fmt.Errorf("browser: this client has no %s", name)
		}
		n.Browsers = slices.Delete(n.Browsers, i, i+1)
	case CatOther:
		if len(values) == 0 {
			return nil, nil, fmt.Errorf("name the provider")
		}
		for _, v := range values {
			v = strings.TrimSpace(v)
			if err := ValidateName(v); err != nil {
				return nil, nil, fmt.Errorf("other provider %q: %w", v, err)
			}
			has := slices.Contains(n.Other, v)
			switch {
			case add && !has:
				n.Other = append(n.Other, v)
			case !add && has:
				n.Other = slices.DeleteFunc(n.Other, func(x string) bool { return x == v })
			case !add:
				return nil, nil, fmt.Errorf("other: this client has no %q", v)
			}
		}
	default:
		// A bare value: a provider named on its own (tentaqles remove gh, tentaqles add aws).
		if len(values) != 0 {
			return nil, nil, fmt.Errorf("unknown category %q (one of %s)", cat, strings.Join(Categories, ", "))
		}
		if c, v, ok := Locate(cat); ok {
			if add || answered(cur, c, v) {
				return Change(cur, add, c, []string{v})
			}
		}
		if add {
			return nil, nil, fmt.Errorf("%q is neither a category (%s) nor a provider reach offers", cat, strings.Join(Categories, ", "))
		}
		// Not an answer: an identity or a scope entry the manifest holds by itself.
		return next, []string{strings.ToLower(strings.TrimSpace(cat))}, nil
	}
	return next, nil, nil
}

// answered reports whether cur's answers hold v in category c.
func answered(cur *Answers, c, v string) bool {
	switch c {
	case CatGit:
		return cur.Git == v
	case CatTask:
		return cur.Task == v
	case CatMail:
		return cur.EmailProvider == v
	case CatCloud:
		return slices.Contains(cur.Clouds, v)
	case CatTeam:
		return slices.Contains(cur.Team, v)
	case CatData:
		return slices.Contains(cur.Data, v)
	case CatAgent:
		return slices.Contains(cur.Agent, v)
	case CatBrowser:
		return slices.ContainsFunc(cur.Browsers, func(b manifest.Browser) bool { return b.Name == v })
	}
	return false
}

func verb(add bool) string {
	if add {
		return "add"
	}
	return "remove"
}

// Values lists cat's offered values, sorted, for help and errors.
func Values(cat string) []string {
	var out []string
	for _, o := range OptionsOf(cat) {
		out = append(out, o.Value)
	}
	sort.Strings(out)
	return out
}
