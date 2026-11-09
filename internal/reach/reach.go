// Package reach is the data side of `tentaqles reach`: the form's options, what its answers mean,
// and the workspace they become. The interactive form lives in internal/cmd.
//
// Answers hold names only. A token is never asked for here.
package reach

import (
	"fmt"
	"net/mail"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/providers"
	"github.com/tentaqles/tentaqles/internal/workspace"
)

// Option is one choice of the form.
type Option struct{ Label, Value string }

// The form's choices. A value is what the manifest stores: a git host, a cloud name, or a catalog id.
var (
	GitOptions     = []Option{{"GitHub", "github"}, {"GitLab", "gitlab"}, {"Bitbucket", "bitbucket"}, {"Azure DevOps", "azure-devops"}}
	CloudOptions   = []Option{{"Azure", "azure"}, {"AWS", "aws"}, {"Google Cloud Platform", "gcp"}}
	TeamOptions    = []Option{{"Microsoft Teams", "teams"}, {"Slack", "slack"}, {"Discord", "discord"}, {"Zoom", "zoom"}, {"Google Meet", "google-meet"}}
	DataOptions    = []Option{{"Databricks", "databricks"}, {"Snowflake", "snowflake"}, {"Google Colab", "colab"}}
	TaskOptions    = []Option{{"Asana", "asana"}, {"Linear", "linear"}, {"GitHub Projects", "gh"}, {"Azure DevOps", "azure-devops"}, {"Trello", "trello"}, {"Jira", "jira"}}
	EmailOptions   = []Option{{"Google", "gmail"}, {"Microsoft", "outlook"}}
	BrowserOptions = []Option{{"Google Chrome", "chrome"}, {"Microsoft Edge", "edge"}}
)

// Answers is one run of the form. An empty field or list is a skipped question.
type Answers struct {
	Email string
	// Language is the client's language tag (manifest.Languages).
	Language string
	Git      string
	Clouds   []string
	Team     []string
	Data     []string
	// Agent holds the AI coding agents besides Claude Code (AgentOptions).
	Agent         []string
	Task          string
	EmailProvider string
	Browsers      []manifest.Browser
	// Other holds the names typed under "Other". tq has no catalog entry for them, so it can
	// neither isolate nor guard them.
	Other []string
}

// ValidateEmail checks the identity email's format.
func ValidateEmail(s string) error {
	s = strings.TrimSpace(s)
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || !strings.Contains(s[strings.LastIndex(s, "@")+1:], ".") {
		return fmt.Errorf("enter a valid email, like you@company.com")
	}
	return gitcfg.ValidateValue(s)
}

var freeTextRe = regexp.MustCompile(`^[\pL\pN][\pL\pN ._+-]{0,63}$`)

// ValidateName checks a name typed into the form, an Other provider or a browser profile.
func ValidateName(s string) error {
	s = strings.TrimSpace(s)
	if manifest.LooksLikeSecret(s) {
		return fmt.Errorf("that looks like a secret; the manifest holds names only")
	}
	if !freeTextRe.MatchString(s) {
		return fmt.Errorf("use letters, digits, spaces and . _ + - (64 characters at most)")
	}
	return nil
}

// Validate checks the whole answer set before anything is written.
func (a *Answers) Validate() error {
	if err := ValidateEmail(a.Email); err != nil {
		return err
	}
	if _, ok := manifest.LanguageOf(a.Language); a.Language != "" && !ok {
		return fmt.Errorf("unknown language %q", a.Language)
	}
	for _, s := range a.Other {
		if err := ValidateName(s); err != nil {
			return fmt.Errorf("other provider %q: %w", s, err)
		}
	}
	m := manifest.Manifest{Browsers: a.Browsers}
	if err := m.ValidateBrowsers(); err != nil {
		return err
	}
	for _, b := range a.Browsers {
		if err := ValidateName(b.Profile); err != nil {
			return fmt.Errorf("browser profile %q: %w", b.Profile, err)
		}
	}
	return nil
}

// gitCLI and cloudCLI are the catalog ids of a git host's and a cloud's CLI.
var (
	gitCLI   = map[string]string{"github": "gh", "gitlab": "glab", "azure-devops": "azure-devops", "bitbucket": "bitbucket"}
	cloudCLI = map[string]string{"azure": "az", "aws": "aws", "gcp": "gcloud"}
)

// AddOptions is the client these answers describe, in folder name under base. reach creates it
// after the developer's typed yes.
func (a *Answers) AddOptions(base, name, gitName string) (workspace.AddOptions, error) {
	if err := a.Validate(); err != nil {
		return workspace.AddOptions{}, err
	}
	cat := providers.MustLoad()
	// Claude Code is always set up. Everything else with a login of its own gets its own folder.
	idents := map[string]bool{"claude": true}
	addIdent := func(id string) {
		if p, ok := cat.Get(id); ok && p.HasIdentity() {
			idents[id] = true
		}
	}
	if a.Git != "" {
		addIdent(gitCLI[a.Git])
	}
	var cloud map[string]any
	for i, c := range a.Clouds {
		addIdent(cloudCLI[c])
		if i == 0 {
			cloud = map[string]any{"provider": c}
		} else {
			also, _ := cloud["also"].([]string)
			cloud["also"] = append(also, c)
		}
	}
	// The rest of the scope, whether or not it has a CLI login: chat and meeting platforms, data
	// platforms, AI agents and the task provider. The scope is what the guard allows.
	var scope []string
	for _, id := range slices.Concat(a.Team, a.Data, a.Agent, []string{a.Task}) {
		if id != "" && !slices.Contains(scope, id) {
			scope = append(scope, id)
			addIdent(id)
		}
	}
	sort.Strings(scope)
	ids := make([]string, 0, len(idents))
	for id := range idents {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	o := workspace.AddOptions{
		Base: base, Name: name, GitName: gitName, GitEmail: strings.TrimSpace(a.Email), GitProvider: a.Git, Language: a.Language,
		Identities: ids, Cloud: cloud, ExtraScope: scope, Browsers: a.Browsers, OtherProviders: a.Other,
	}
	if a.EmailProvider != "" {
		o.Email = &manifest.Service{Provider: a.EmailProvider}
	}
	return o, nil
}

// GitNameFor is the commit name for a client: the developer's global git name, or the part of the
// email before the @.
func GitNameFor(global, email string) string {
	if g := strings.TrimSpace(global); g != "" {
		return g
	}
	local, _, _ := strings.Cut(strings.TrimSpace(email), "@")
	return local
}
