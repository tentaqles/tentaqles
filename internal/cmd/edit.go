package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

// reachCategoryForm asks one category of reach's form, starting from cur. A seam for tests.
var reachCategoryForm = runCategoryForm

func newAddCmd() *cobra.Command    { return newChangeCmd(true) }
func newRemoveCmd() *cobra.Command { return newChangeCmd(false) }

// newChangeCmd is tentaqles add (add true) or tentaqles remove: one change to a client's providers,
// browser or email, through the same edit as reach's menu.
func newChangeCmd(add bool) *cobra.Command {
	var client string
	var yes bool
	use, short, verb := "add <category> [value...]", "Add a provider, browser or email to a client", "add"
	if !add {
		use, short, verb = "remove <category> [value...]", "Remove a provider, browser or other provider from a client", "remove"
	}
	c := &cobra.Command{
		Use:   use,
		Short: short,
		Long: "tentaqles " + verb + " changes one part of a client's setup, as tentaqles reach's menu does, without the menu.\n\n" +
			"Categories: " + strings.Join(reach.Categories, ", ") + ".\n" +
			"  git, task and mail hold one value: add replaces it, remove clears it.\n" +
			"  cloud, team, data, browser and other are lists: add and remove change the values named.\n" +
			"  email is the client's git email: add sets it; it cannot be removed.\n" +
			"A value alone works too: tentaqles add aws, tentaqles remove gh. With no value, the category's\n" +
			"part of the form opens with the client's current answers.\n\n" +
			"Values: git " + strings.Join(reach.Values(reach.CatGit), ", ") + " (also gh, glab, ado);\n" +
			"cloud " + strings.Join(reach.Values(reach.CatCloud), ", ") + " (also az, gcloud); team " + strings.Join(reach.Values(reach.CatTeam), ", ") + ";\n" +
			"data " + strings.Join(reach.Values(reach.CatData), ", ") + "; task " + strings.Join(reach.Values(reach.CatTask), ", ") + ";\n" +
			"mail " + strings.Join(reach.Values(reach.CatMail), ", ") + "; browser chrome or edge, then a profile.\n\n" +
			"It works on the client of the current folder, or --client (a name or a folder). It lists every change\n" +
			"and waits for a typed yes (--yes skips that), then rewrites the manifest and renders the client's files.\n" +
			"It refuses to run from inside a Claude Code session.",
		Example: "  tentaqles add git azure-devops\n  tentaqles add cloud aws\n  tentaqles remove gh --client yduqs\n" +
			"  tentaqles add browser chrome \"Profile 2\"\n  tentaqles add email dev@acme.example\n  tentaqles add task",
		Args: cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if insideAgent() {
				return errors.New("tentaqles " + verb + ": run it in your own terminal, not from a Claude Code session")
			}
			ws, err := changeTarget(client)
			if err != nil {
				return err
			}
			cur := reach.AnswersFrom(ws.Manifest)
			cat, values := strings.ToLower(args[0]), args[1:]
			var next *reach.Answers
			var drop []string
			if len(values) == 0 && slices.Contains(reach.Categories, cat) && cat != reach.CatEmail &&
				(add || !reach.Single(cat)) {
				next, err = reachCategoryForm(cat, cur)
			} else {
				next, drop, err = reach.Change(cur, add, cat, values)
			}
			if err != nil {
				return err
			}
			return applyEdit(c, "tentaqles "+verb, ws, cur, next, drop, yes)
		},
	}
	if add {
		c.Aliases = []string{"set"}
	}
	c.Flags().StringVar(&client, "client", "", "the client to change: its name, or its folder (default: the current folder's client)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for the typed yes")
	return c
}

// changeTarget is the client tentaqles add and remove change: --client as a folder or a name, else the
// current folder's.
func changeTarget(client string) (*resolve.Workspace, error) {
	if client == "" {
		ws, err := cwdWorkspace()
		if err != nil {
			return nil, err
		}
		if ws == nil {
			return nil, errors.New("this folder belongs to no client: cd into one, or name it with --client")
		}
		return ws, nil
	}
	if fi, err := os.Stat(client); err == nil && fi.IsDir() {
		cfg, err := registry.Load()
		if err != nil {
			return nil, err
		}
		abs, _ := filepath.Abs(client)
		r := resolve.Resolve(abs, cfg)
		if r.Workspace == nil {
			return nil, fmt.Errorf("%s belongs to no client (%s)", abs, r.Reason)
		}
		return r.Workspace, nil
	}
	return findWorkspace(client)
}

// runCategoryForm asks one category of the form, starting from cur's answers, and returns the
// answers with that category replaced.
func runCategoryForm(cat string, cur *reach.Answers) (*reach.Answers, error) {
	if !interactive() {
		return nil, errNoTerminal
	}
	a := *cur
	a.Clouds, a.Team, a.Data = slices.Clone(cur.Clouds), slices.Clone(cur.Team), slices.Clone(cur.Data)
	a.Browsers, a.Other = slices.Clone(cur.Browsers), slices.Clone(cur.Other)
	run := func(f huh.Field) error { return huh.NewForm(huh.NewGroup(f)).Run() }
	titles := map[string]string{reach.CatGit: "Git provider", reach.CatCloud: "Cloud providers",
		reach.CatTeam: "Team and meeting platforms", reach.CatData: "Data platforms",
		reach.CatTask: "Task management provider", reach.CatMail: "Email provider"}
	options := func() []huh.Option[string] {
		out := []huh.Option[string]{}
		for _, o := range reach.OptionsOf(cat) {
			out = append(out, huh.NewOption(o.Label, o.Value))
		}
		return out
	}
	switch cat {
	case reach.CatGit, reach.CatTask, reach.CatMail:
		field := map[string]*string{reach.CatGit: &a.Git, reach.CatTask: &a.Task, reach.CatMail: &a.EmailProvider}[cat]
		v := *field
		if v == "" {
			v = reachSkip
		}
		opts := append(options(), huh.NewOption("None", reachSkip))
		if err := run(huh.NewSelect[string]().Title(titles[cat]).Options(opts...).Value(&v)); err != nil {
			return nil, err
		}
		*field = ""
		if v != reachSkip {
			*field = v
		}
	case reach.CatCloud, reach.CatTeam, reach.CatData:
		field := map[string]*[]string{reach.CatCloud: &a.Clouds, reach.CatTeam: &a.Team, reach.CatData: &a.Data}[cat]
		vs := slices.Clone(*field)
		if err := run(huh.NewMultiSelect[string]().Title(titles[cat]).Options(options()...).Value(&vs)); err != nil {
			return nil, err
		}
		*field = vs
	case reach.CatBrowser:
		if err := askBrowsers(run, &a); err != nil {
			return nil, err
		}
	case reach.CatOther:
		keep := slices.Clone(a.Other)
		if len(a.Other) > 0 {
			var opts []huh.Option[string]
			for _, n := range a.Other {
				opts = append(opts, huh.NewOption(n, n).Selected(true))
			}
			if err := run(huh.NewMultiSelect[string]().Title("Providers tentaqles does not manage: keep").Options(opts...).Value(&keep)); err != nil {
				return nil, err
			}
		}
		name := ""
		if err := run(huh.NewInput().Title("Another one (empty for none)").Value(&name).Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return nil
			}
			return reach.ValidateName(s)
		})); err != nil {
			return nil, err
		}
		if n := strings.TrimSpace(name); n != "" && !slices.Contains(keep, n) {
			keep = append(keep, n)
		}
		a.Other = keep
	default:
		return nil, fmt.Errorf("no form for %q", cat)
	}
	return &a, nil
}
