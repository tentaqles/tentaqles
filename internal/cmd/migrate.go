package cmd

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/gitcfg"
	"github.com/tentaqles/tentaqles/internal/migrate"
	"github.com/tentaqles/tentaqles/internal/registry"
)

// journalTSLayout names a journal directory. Sorting these strings sorts them
// chronologically, which is what migrate.Load("latest") relies on.
const journalTSLayout = "20060102T150405Z"

// defaultSteps is what `tq migrate` runs when the user does not say.
//
// cmd is deliberately absent: clearing the cmd.exe AutoRun hook changes every
// console the user opens, and it is the one step whose effect is invisible
// until the next shell starts. Ask for it explicitly with --steps.
const defaultSteps = "identity,git,shell"

// Seams. Production values reach the real machine; tests replace all four.
var (
	// migrateProcesses lists running processes for the identity step's in-use
	// check. It must never be nil: a nil Deps.Processes turns that check into
	// a no-op and lets tq move a directory out from under a live process.
	migrateProcesses = listProcesses
	// migrateReg runs reg.exe. A nil Deps.Reg makes the cmd step skip with
	// "no registry runner configured" and silently do nothing.
	migrateReg = migrate.DefaultRunner().Reg
	// migrateGit runs the git binary.
	migrateGit gitcfg.Run = gitcfg.RunGit
	// migrateNow returns the journal timestamp for this invocation.
	migrateNow = func() string { return time.Now().UTC().Format(journalTSLayout) }
)

func newMigrateCmd() *cobra.Command {
	var (
		apply    bool
		force    bool
		asJSON   bool
		stepsCSV string
	)
	c := &cobra.Command{
		Use:   "migrate",
		Short: "Move a hand-rolled tentaqles setup under tq's management",
		Long: "Plans (and with --apply performs) the migration from the hand-installed\n" +
			"tentaqles setup to the one tq maintains: identity directories moved inside\n" +
			"tq with a junction left behind, the global git identity replaced by tq's\n" +
			"include file, hand-pasted shell hooks adopted, and (with --steps cmd) the\n" +
			"legacy cmd.exe AutoRun hook cleared.\n\n" +
			"Every mutation is journalled before it happens; `tq uninstall --restore`\n" +
			"replays that journal backwards.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			out := c.OutOrStdout()

			names, err := parseSteps(stepsCSV)
			if err != nil {
				return err
			}
			// Steps() returns them in tq's own order (identity, git, shell,
			// cmd) whatever order they were listed in, and rejects unknown
			// names. Never re-sort what it hands back.
			steps, err := migrate.Steps(names)
			if err != nil {
				return err
			}

			cfg, err := registry.Load()
			if err != nil {
				return err
			}

			d := migrate.Deps{
				Cfg: cfg,
				Git: migrateGit,
				Reg: migrateReg,
				Env: os.LookupEnv,
				// Memoised: the identity step asks once while planning and
				// again while applying, and enumerating processes costs
				// seconds on Windows. One list also means the refusal the user
				// reads in the plan is the refusal that fires on apply.
				Processes: onceProcesses(migrateProcesses),
				Force:     force,
			}

			// The journal is opened, and its path printed, before anything is
			// touched: a user who interrupts the run mid-way still knows where
			// the undo lives.
			ts := ""
			var j *migrate.Journal
			if apply {
				ts = migrateNow()
				j, err = migrate.Open(ts)
				if err != nil {
					return err
				}
				if !asJSON {
					fmt.Fprintf(out, "journal: %s\n", j.Dir)
				}
			}

			// One Run call for the whole set. Reconstructing a Plan from JSON
			// and handing it to Apply would be rejected: each step cross-checks
			// the plan against one it derives in the same call.
			plans, runErr := migrate.Run(d, steps, apply, j)

			if asJSON {
				if err := writeMigrateJSON(out, ts, plans, apply && runErr == nil); err != nil {
					return err
				}
			} else {
				renderPlans(out, plans)
			}

			if runErr != nil {
				step, msg := splitStepErr(runErr, names)
				fmt.Fprintf(c.ErrOrStderr(), "stopped at %s: %s; restore with: tq uninstall --restore %s\n", step, msg, ts)
				exitFunc(1)
				return nil
			}
			if !asJSON {
				if apply {
					fmt.Fprintf(out, "applied. undo with: tq uninstall --restore %s\n", ts)
				} else {
					fmt.Fprintln(out, "dry run — nothing changed. Re-run with --apply.")
				}
			}
			return nil
		},
	}
	c.Flags().BoolVar(&apply, "apply", false, "perform the migration (without this it only shows what it would do)")
	c.Flags().StringVar(&stepsCSV, "steps", defaultSteps, "comma-separated steps to run: "+strings.Join(migrate.KnownSteps(), ", "))
	c.Flags().BoolVar(&force, "force", false, "proceed past advisory refusals (a directory in use, a changed AutoRun value)")
	c.Flags().BoolVar(&asJSON, "json", false, "JSON output")
	return c
}

// parseSteps splits the --steps list, dropping blanks so a trailing comma is
// not an error. It rejects an empty result rather than silently running
// nothing.
func parseSteps(csv string) ([]string, error) {
	var out []string
	for _, s := range strings.Split(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--steps needs at least one step (known: %s)", strings.Join(migrate.KnownSteps(), ", "))
	}
	return out, nil
}

// splitStepErr recovers the step name migrate.Run prefixed onto the error, so
// the message can name it separately. It only strips a prefix that is one of
// the steps actually asked for, and falls back to the whole error otherwise.
func splitStepErr(err error, names []string) (string, string) {
	msg := err.Error()
	for _, n := range names {
		if strings.HasPrefix(msg, n+": ") {
			return n, strings.TrimPrefix(msg, n+": ")
		}
	}
	return "migrate", msg
}

// kindWidth pads the change kind so paths line up. The longest kind any step
// emits is "sync-include-file"; a longer one simply overflows, still separated
// by the two spaces below.
const kindWidth = 17

// renderPlans writes the human-readable plan. Change.Detail is printed
// verbatim: the steps own its wording and the command layer must not parse it.
func renderPlans(w io.Writer, plans map[string]migrate.Plan) {
	danger := false
	for _, name := range migrate.SortedStepNames(plans) {
		p := plans[name]
		fmt.Fprintf(w, "%s: %s\n", name, changeCount(len(p.Changes)))
		for _, ch := range p.Changes {
			bullet := "~"
			if ch.Danger {
				bullet = "!"
				danger = true
			}
			line := fmt.Sprintf("  %s %-*s  %s", bullet, kindWidth, ch.Kind, ch.Path)
			if ch.Detail != "" {
				line += " -> " + ch.Detail
			}
			fmt.Fprintln(w, line)
		}
		for _, warn := range p.Warnings {
			fmt.Fprintf(w, "  warn: %s\n", warn)
		}
		for _, sk := range p.Skipped {
			fmt.Fprintf(w, "  skip: %s\n", sk)
		}
	}
	if danger {
		fmt.Fprintln(w, "! marks changes that move or delete real data.")
	}
}

func changeCount(n int) string {
	switch n {
	case 0:
		return "no changes"
	case 1:
		return "1 change"
	default:
		return fmt.Sprintf("%d changes", n)
	}
}

// ------------------------------------------------------------------ JSON

// The JSON shape is the CLI's own contract, mirrored here rather than
// marshalling migrate.Plan directly: those structs carry no json tags, so
// encoding them would publish Go field names ("Changes", "Kind") that could
// never be renamed afterwards.
type jsonChange struct {
	Step   string `json:"step"`
	Kind   string `json:"kind"`
	Path   string `json:"path"`
	Detail string `json:"detail"`
	Danger bool   `json:"danger"`
}

type jsonPlan struct {
	Changes  []jsonChange `json:"changes"`
	Warnings []string     `json:"warnings"`
	Skipped  []string     `json:"skipped"`
}

type migrateJSON struct {
	// TS names the journal this run wrote, and is empty on a dry run: a
	// timestamp naming no journal would be a trap for a script that feeds it
	// straight to `tq uninstall --restore`. After a failed --apply it is set,
	// with applied false, because that is exactly when it is needed.
	TS      string              `json:"ts"`
	Steps   map[string]jsonPlan `json:"steps"`
	Applied bool                `json:"applied"`
}

func writeMigrateJSON(w io.Writer, ts string, plans map[string]migrate.Plan, applied bool) error {
	doc := migrateJSON{TS: ts, Steps: map[string]jsonPlan{}, Applied: applied}
	for name, p := range plans {
		jp := jsonPlan{
			Changes:  make([]jsonChange, 0, len(p.Changes)),
			Warnings: emptyIfNil(p.Warnings),
			Skipped:  emptyIfNil(p.Skipped),
		}
		for _, ch := range p.Changes {
			jp.Changes = append(jp.Changes, jsonChange{
				Step: ch.Step, Kind: ch.Kind, Path: ch.Path, Detail: ch.Detail, Danger: ch.Danger,
			})
		}
		doc.Steps[name] = jp
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(doc)
}

// emptyIfNil keeps a nil slice out of the JSON, so consumers always see [].
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// ------------------------------------------------------- the process list

// procListTimeout bounds the external call. A machine where the process list
// takes longer than this is one tq cannot vouch for, and the timeout surfaces
// as an error, which the identity step treats as a blocker.
const procListTimeout = 60 * time.Second

// onceProcesses memoises a process lister, errors included, so a single
// `tq migrate` enumerates processes once however many times the steps ask.
func onceProcesses(fn func() ([]string, error)) func() ([]string, error) {
	var (
		done  bool
		lines []string
		err   error
	)
	return func() ([]string, error) {
		if !done {
			lines, err = fn()
			done = true
		}
		return lines, err
	}
}

// listProcesses returns one line per running process: the executable name
// followed, where the OS will say, by its full command line.
//
// The identity step matches those lines two ways — a line mentioning the
// directory about to be moved, and a line whose executable is the CLI that
// owns it — so both halves matter, but neither is required: a name-only line
// still blocks a move of that CLI's directory.
//
// It returns an error rather than an empty list whenever it cannot vouch for
// the answer. An empty list reads as "nothing is running", which would let tq
// move a live identity directory; the caller turns the error into a refusal
// the user clears with --force.
func listProcesses() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), procListTimeout)
	defer cancel()
	if runtime.GOOS == "windows" {
		return windowsProcesses(ctx)
	}
	return unixProcesses(ctx)
}

// psScript enumerates processes with their command lines. Win32_Process is the
// only readily available source of command lines (Get-Process has none), and
// the CommandLine of a process owned by another user often comes back empty —
// hence the name is always emitted too.
const psScript = `$ErrorActionPreference='Stop'
[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
Get-CimInstance -ClassName Win32_Process | ForEach-Object { ($_.Name + ' ' + $_.CommandLine).Trim() }`

func windowsProcesses(ctx context.Context) ([]string, error) {
	shell, err := exec.LookPath("powershell")
	if err != nil {
		shell, err = exec.LookPath("pwsh")
	}
	if err != nil {
		return nil, fmt.Errorf("neither powershell nor pwsh is on PATH, so tq cannot list running processes: %w", err)
	}
	// -EncodedCommand takes UTF-16LE base64, which sidesteps every layer of
	// quoting between here and PowerShell's parser.
	cmd := exec.CommandContext(ctx, shell, "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePS(psScript))
	out, err := cmd.Output()
	if err != nil {
		detail := ""
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			detail = ": " + strings.TrimSpace(string(ee.Stderr))
		}
		if ctx.Err() != nil {
			detail = ": timed out after " + procListTimeout.String()
		}
		return nil, fmt.Errorf("Get-CimInstance Win32_Process failed%s: %w", detail, err)
	}
	return splitProcLines(string(out), "Get-CimInstance Win32_Process")
}

// encodePS base64-encodes a script as UTF-16LE for powershell -EncodedCommand.
func encodePS(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[2*i:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// unixProcesses asks ps for every process's full argument vector, falling back
// to bare command names on a ps that will not print args.
func unixProcesses(ctx context.Context) ([]string, error) {
	var firstErr error
	for _, args := range [][]string{{"-A", "-o", "args="}, {"-A", "-o", "comm="}} {
		out, err := exec.CommandContext(ctx, "ps", args...).Output()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("ps %s: %w", strings.Join(args, " "), err)
			}
			continue
		}
		lines, err := splitProcLines(string(out), "ps "+strings.Join(args, " "))
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		return lines, nil
	}
	return nil, fmt.Errorf("tq could not list running processes: %w", firstErr)
}

func splitProcLines(raw, who string) ([]string, error) {
	var out []string
	for _, l := range strings.Split(raw, "\n") {
		if l = strings.TrimSpace(strings.TrimSuffix(l, "\r")); l != "" {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		// Treated as a failure on purpose: "no processes are running" is never
		// true, and believing it would disable the in-use check.
		return nil, fmt.Errorf("%s returned no processes", who)
	}
	return out, nil
}
