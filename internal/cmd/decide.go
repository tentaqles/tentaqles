package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
)

func newDecideCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "decide",
		Short: "Ask Jev (TypeSafe) fast yes/no, choice and score questions",
		Long: `The Jev decision layer. Configured per workspace by the manifest's
decision block:

  decision:
    backend: typesafe        # or off (the default)
    mode: shadow             # shadow (log only, the default) or enforce
    block_threshold: 0.8
    warn_threshold: 0.5
    env_file: ../tentaqles/.env   # dotenv holding TYPESAFE_API_KEY

The state is redacted before it is sent and framed as untrusted data. Calls
are cached and logged (cost, latency; never the state) under
$TQ_HOME/decide/.`,
	}
	c.AddCommand(newDecideStatusCmd(), newDecideAskCmd(), newDecideEvalCmd(), newDecideTriageCmd(),
		newDecideExploreCmd(), newDecideLogCmd())
	return c
}

// decideClient builds a client from the current workspace's policy.
func decideClient(purpose string, timeout time.Duration) (*decide.Client, decide.Policy, *resolve.Workspace, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, decide.Policy{}, nil, err
	}
	return decideClientAt(cwd, purpose, timeout)
}

// decideClientAt builds a client from the policy of the workspace holding dir.
func decideClientAt(cwd, purpose string, timeout time.Duration) (*decide.Client, decide.Policy, *resolve.Workspace, error) {
	cfg, err := registry.Load()
	if err != nil {
		return nil, decide.Policy{}, nil, err
	}
	ws := resolve.Resolve(cwd, cfg).Workspace
	if ws == nil {
		return nil, decide.Policy{}, nil, errors.New("not in a trusted workspace (decision policy comes from the manifest)")
	}
	pol := ws.Manifest.Decision
	cl, err := jevClientFor(ws, purpose, timeout)
	if errors.Is(err, decide.ErrDisabled) {
		return nil, pol, ws, fmt.Errorf("decision backend is off for %s (set decision.backend: typesafe in %s)", ws.Name, ws.ManifestPath)
	}
	if err != nil {
		return nil, pol, ws, err
	}
	return cl, pol, ws, nil
}

func newDecideStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show this workspace's decision policy and whether a key is found (never the key)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cwd, _ := os.Getwd()
			cfg, err := registry.Load()
			if err != nil {
				return err
			}
			ws := resolve.Resolve(cwd, cfg).Workspace
			if ws == nil {
				return errors.New("not in a trusted workspace")
			}
			p := ws.Manifest.Decision
			block, warn := p.Thresholds()
			backend, mode := p.Backend, p.Mode
			if backend == "" {
				backend = "off"
			}
			if mode == "" {
				mode = "shadow"
			}
			key := "missing"
			if decide.ResolveKey(p.EnvFilePath(filepath.Dir(ws.ManifestPath))) != "" {
				key = "found"
			}
			model := p.Model
			if model == "" {
				model = decide.DefaultModel
			}
			out := c.OutOrStdout()
			fmt.Fprintf(out, "workspace: %s\nbackend:   %s\nmode:      %s\nmodel:     %s\nblock:     %.2f\nwarn:      %.2f\nkey:       %s (%s)\n",
				ws.Name, backend, mode, model, block, warn, key, decide.KeyName)
			return nil
		},
	}
}

func newDecideAskCmd() *cobra.Command {
	var state, stateFile string
	var qs []string
	var asJSON bool
	var timeout time.Duration
	var purpose string
	c := &cobra.Command{
		Use:   "ask",
		Short: "Ask one batch of questions about a state",
		Long: `Each --q is id=type:instructions, where type is noul (yes/no). For choice
and score questions pass --questions-file with Jev's native question JSON.

  tq decide ask --state-file diff.txt \
    --q destructive="Does this change drop or truncate data?" \
    --q auth="Does it touch authentication or authorization?"

A bare --q id=instructions is a noul question. Output is one line per
question (id, answer), or the raw answers with --json.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st any = state
			if stateFile != "" {
				raw, err := readStateFile(stateFile)
				if err != nil {
					return err
				}
				st = raw
			}
			if st == "" {
				return errors.New("--state or --state-file is required")
			}
			questions, err := parseQuestions(qs)
			if err != nil {
				return err
			}
			if qf, _ := c.Flags().GetString("questions-file"); qf != "" {
				raw, err := os.ReadFile(qf)
				if err != nil {
					return err
				}
				extra := map[string]decide.Question{}
				if err := json.Unmarshal(raw, &extra); err != nil {
					return fmt.Errorf("%s: %w", qf, err)
				}
				for k, v := range extra {
					questions[k] = v
				}
			}
			if len(questions) == 0 {
				return errors.New("at least one --q or --questions-file is required")
			}
			cl, _, _, err := decideClient(purposeLabel(purpose), timeout)
			if err != nil {
				return err
			}
			r, err := cl.Ask(context.Background(), st, questions)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(r.Answers)
			}
			ids := make([]string, 0, len(r.Answers))
			for id := range r.Answers {
				ids = append(ids, id)
			}
			sort.Strings(ids)
			for _, id := range ids {
				a := r.Answers[id]
				switch {
				case a.Noul != nil:
					fmt.Fprintf(out, "%s\t%.3f\n", id, *a.Noul)
				case a.Choice != "":
					fmt.Fprintf(out, "%s\t%s (%.2f)\n", id, a.Choice, a.Confidence)
				case a.Score != nil:
					fmt.Fprintf(out, "%s\t%g (%.2f)\n", id, *a.Score, a.Confidence)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&state, "state", "", "the state, as text")
	c.Flags().StringVar(&stateFile, "state-file", "", "file holding the state (JSON is sent as an object, anything else as text)")
	c.Flags().StringArrayVar(&qs, "q", nil, "question: id=noul:instructions (repeatable)")
	c.Flags().String("questions-file", "", "JSON object of id -> native Jev question")
	c.Flags().BoolVar(&asJSON, "json", false, "print raw answers as JSON")
	c.Flags().DurationVar(&timeout, "timeout", decide.BatchTimeout, "request deadline")
	c.Flags().StringVar(&purpose, "purpose", "cli", "label for the cost log (e.g. memory-capture)")
	return c
}

// purposeLabel keeps a caller-supplied log label short and plain, so the
// cost log never carries free text.
func purposeLabel(s string) string {
	if !labelRe.MatchString(s) {
		return "cli"
	}
	return s
}

var labelRe = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// readStateFile returns the file as a JSON value when it parses, else text.
func readStateFile(path string) (any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v any
	if json.Unmarshal(raw, &v) == nil {
		return v, nil
	}
	return string(raw), nil
}

func parseQuestions(specs []string) (map[string]decide.Question, error) {
	out := map[string]decide.Question{}
	for _, s := range specs {
		id, rest, ok := strings.Cut(s, "=")
		id = strings.TrimSpace(id)
		if !ok || id == "" || strings.TrimSpace(rest) == "" {
			return nil, fmt.Errorf("--q %q: want id=noul:instructions", s)
		}
		if typ, instr, ok := strings.Cut(rest, ":"); ok && typ == "noul" {
			rest = instr
		}
		out[id] = decide.Noul(strings.TrimSpace(rest))
	}
	return out, nil
}

func newDecideEvalCmd() *cobra.Command {
	var workers int
	c := &cobra.Command{
		Use:   "eval CASES.jsonl",
		Short: "Score Jev on labelled cases and pick per-family thresholds",
		Long: `Each line is {"id","family","lang","tags","state","question","expect"}.
For every family it prints precision and recall across thresholds, the
always-no baseline, and the threshold with the best recall at precision >= 0.90. A
family with no such threshold stays in shadow mode. Misclassified cases are
listed by id so the cases file can be corrected or the question reworded.`,
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			f, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer f.Close()
			cases, err := decide.ReadCases(f)
			if err != nil {
				return fmt.Errorf("%s: %w", args[0], err)
			}
			cl, _, _, err := decideClient("eval", decide.BatchTimeout)
			if err != nil {
				return err
			}
			start := time.Now()
			results := decide.Run(context.Background(), cl, cases, workers)
			decide.Format(c.OutOrStdout(), decide.Score(results))
			fmt.Fprintf(c.OutOrStdout(), "\n%d cases in %s\n", len(cases), time.Since(start).Round(time.Millisecond))
			return nil
		},
	}
	c.Flags().IntVar(&workers, "workers", 4, "parallel requests")
	return c
}

func newDecideTriageCmd() *cobra.Command {
	var staged, asJSON bool
	var rng string
	c := &cobra.Command{
		Use:   "triage",
		Short: "Classify a diff as low or high risk to pick the review depth",
		Long: `Asks Jev a fixed set of yes/no risk questions (auth, money, schema, data
loss, migrations, RLS, secrets, public API, shared state, destructive
commands, n8n credentials) about a diff:

  low   every answer below the warn threshold: one light review pass
  high  any flag raised, the diff was truncated, or Jev failed: full review

The diff is the working tree (default), --staged, or --range A..B. Exit
code is 0 for low and 3 for high, so scripts can branch on it.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			args := []string{"diff", "--no-color"}
			switch {
			case rng != "":
				args = append(args, rng)
			case staged:
				args = append(args, "--cached")
			}
			cwd, _ := os.Getwd()
			diff, err := gitcfg.RunGitIn(cwd, args...)
			if err != nil {
				return fmt.Errorf("git diff: %w", err)
			}
			out := c.OutOrStdout()
			if strings.TrimSpace(diff) == "" {
				fmt.Fprintln(out, "low (empty diff)")
				return nil
			}
			cl, pol, ws, err := decideClient("triage", decide.BatchTimeout)
			res := decide.TriageResult{Risk: "high"}
			if err != nil {
				res.Error = err.Error()
			} else {
				res = decide.Triage(context.Background(), cl, pol, diff)
				judgmentLog().Write(decide.Judgment{Workspace: ws.Name, Kind: "triage", Mode: policyMode(pol), P: res.P,
					Error: res.Error, Extra: map[string]string{"risk": res.Risk, "flags": strings.Join(res.Flags, ",")}})
			}
			if asJSON {
				_ = json.NewEncoder(out).Encode(res)
			} else {
				line := res.Risk
				if len(res.Flags) > 0 {
					line += " (" + strings.Join(res.Flags, ", ") + ")"
				}
				if res.Truncated {
					line += " [diff truncated]"
				}
				if res.Error != "" {
					line += " [jev unavailable: full review]"
				}
				fmt.Fprintln(out, line)
			}
			if res.Risk != "low" {
				exitFunc(3)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&staged, "staged", false, "triage the staged diff")
	c.Flags().StringVar(&rng, "range", "", "triage a commit range, e.g. main..HEAD")
	c.Flags().BoolVar(&asJSON, "json", false, "print the result as JSON")
	return c
}
