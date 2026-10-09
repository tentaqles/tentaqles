package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/decide"
	"github.com/tentaqles/tentaqles/internal/registry"
	"github.com/tentaqles/tentaqles/internal/resolve"
	"github.com/tentaqles/tentaqles/internal/secrets"
)

// Values in a `tq decide log` line are short plain tokens (ids, actions,
// counts): the judgment log must never carry content.
var (
	logKeyRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	logValueRe = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{0,40}$`)
)

func newDecideLogCmd() *cobra.Command {
	var kind, mode, errMsg string
	var ps, woulds, extras []string
	c := &cobra.Command{
		Use:   "log",
		Short: "Append a judgment line from a caller (the plugin's memory gates)",
		Long: `Writes one line to $TQ_HOME/decide/judgments.jsonl, next to the guard's
and router's own lines, so shadow decisions made outside tq are measured in
one place:

  tq decide log --kind memory-capture --p worth=0.82 --would worth=keep --extra tool=Bash

--p is id=probability, --would and --extra are key=value. Keys and values
must be short plain tokens (ids, actions, counts): the log never holds
content, and a value that looks like text is refused.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if !labelRe.MatchString(kind) {
				return fmt.Errorf("--kind %q: want a short lowercase label like memory-capture", kind)
			}
			if mode != "shadow" && mode != "enforce" {
				return fmt.Errorf("--mode must be shadow or enforce")
			}
			j := decide.Judgment{Kind: kind, Mode: mode}
			for _, s := range ps {
				k, v, ok := strings.Cut(s, "=")
				f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
				if !ok || !logKeyRe.MatchString(k) || err != nil || f < 0 || f > 1 {
					return fmt.Errorf("--p %q: want id=probability in [0,1]", s)
				}
				if j.P == nil {
					j.P = map[string]float64{}
				}
				j.P[k] = f
			}
			kv := func(flag string, in []string) (map[string]string, error) {
				var m map[string]string
				for _, s := range in {
					k, v, ok := strings.Cut(s, "=")
					if !ok || !logKeyRe.MatchString(k) || !logValueRe.MatchString(v) {
						return nil, fmt.Errorf("--%s %q: want key=value with short plain tokens", flag, s)
					}
					if m == nil {
						m = map[string]string{}
					}
					m[k] = v
				}
				return m, nil
			}
			var err error
			if j.Would, err = kv("would", woulds); err != nil {
				return err
			}
			if j.Extra, err = kv("extra", extras); err != nil {
				return err
			}
			if errMsg != "" {
				j.Error = truncate(secrets.Redact(errMsg), 120)
			}
			if cwd, err := os.Getwd(); err == nil {
				if cfg, err := registry.Load(); err == nil {
					if ws := resolve.Resolve(cwd, cfg).Workspace; ws != nil {
						j.Workspace = ws.Name
					}
				}
			}
			judgmentLog().Write(j)
			return nil
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "what was judged (e.g. memory-capture, memory-recall)")
	c.Flags().StringVar(&mode, "mode", "shadow", "shadow or enforce")
	c.Flags().StringArrayVar(&ps, "p", nil, "id=probability (repeatable)")
	c.Flags().StringArrayVar(&woulds, "would", nil, "id=action the answer maps to (repeatable)")
	c.Flags().StringArrayVar(&extras, "extra", nil, "key=value context, plain tokens only (repeatable)")
	c.Flags().StringVar(&errMsg, "error", "", "error that kept the gate from answering")
	_ = c.MarkFlagRequired("kind")
	return c
}
