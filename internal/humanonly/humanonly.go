// Package humanonly decides whether a tq command that writes under the
// Tentaqles config area runs for a human (PRD v2 §8.1). Three detectors refuse
// the command, with no prompt and one line in the denial log: stdin or stdout
// is not a terminal, the process tree holds a harness or cannot be followed
// to a terminal, or the environment carries a harness marker. When all three
// pass, Gate prints what is about to be written and asks the human to type
// yes before any write lands.
package humanonly

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/tentaqles/tentaqles/internal/audit"
)

// ErrHumanOnly is wrapped by every refusal: the command is human-only and this
// process is not a human at a terminal.
var ErrHumanOnly = errors.New("human-only: run it in your own terminal (an agent session is refused)")

// ErrAborted is wrapped when the human does not type yes at the confirmation:
// the command exits 1 and nothing was written.
var ErrAborted = errors.New("aborted: nothing written")

// The three detectors, named in refusals and in the denial log.
const (
	DetectorTTY         = "tty"
	DetectorProcessTree = "process-tree"
	DetectorEnvMarker   = "env-marker"
)

// Markers are the environment variables that mark a harness or an agent
// session: a headless tq run and its children, the Claude Code plugin and
// Claude Code itself, a hub task of an older tq, Codex's sandbox and thread,
// OpenCode and Kimi Code. CLAUDE_CONFIG_DIR and CODEX_HOME are not markers:
// tq sets them in human terminals.
var Markers = []string{
	"TENTAQLES_HEADLESS", "TENTAQLES_HEADLESS_CHILD",
	"CLAUDE_PLUGIN_ROOT", "CLAUDECODE", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_SSE_PORT",
	"TQ_HUB_TASK",
	"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_THREAD_ID",
	"OPENCODE", "KIMI_CODE",
}

// Seams. Tests replace them; production keeps the real terminal, the real
// process tree and a confirmation read from the terminal.
var (
	// IsTerminal reports whether f is an interactive terminal: a tty, a
	// console, or on Windows a Cygwin / MSYS2 pty (mintty's Git Bash).
	IsTerminal = isTerminal
	// ParentOf reads one step of the process tree (per OS; see proctree_*.go).
	ParentOf ParentFunc = parentOf
	// Confirm prints prompt and reports whether the human typed yes. The real
	// implementation reads the terminal itself, never a piped stdin.
	Confirm = confirmOnTerminal
)

// Refusal names the detector that fired and why.
type Refusal struct {
	Detector string
	Reason   string
}

func (r Refusal) String() string { return r.Detector + ": " + r.Reason }

// EnvMarker returns the first harness marker set in the environment.
func EnvMarker() (string, bool) {
	for _, k := range Markers {
		if strings.TrimSpace(os.Getenv(k)) != "" {
			return k, true
		}
	}
	return "", false
}

// Detect runs the three detectors and returns the first refusal, or nil for a
// human at a terminal. It never prompts.
func Detect() *Refusal {
	if !IsTerminal(os.Stdin) {
		return &Refusal{DetectorTTY, "stdin is not a terminal"}
	}
	if !IsTerminal(os.Stdout) {
		return &Refusal{DetectorTTY, "stdout is not a terminal"}
	}
	if m, ok := EnvMarker(); ok {
		return &Refusal{DetectorEnvMarker, m + " is set"}
	}
	if processTreeMethod == "" {
		return &Refusal{DetectorProcessTree, "cannot establish a human ancestry: no process-tree walker on " + runtime.GOOS}
	}
	loadUserAnchorSet()
	switch v, p, why := Walk(selfPID(), ParentOf); v {
	case Harness:
		return &Refusal{DetectorProcessTree, fmt.Sprintf("%s (pid %d) is in the process tree", p.Name, p.PID)}
	case Unverified:
		return &Refusal{DetectorProcessTree, "cannot establish a human ancestry: " + why}
	}
	return nil
}

// Check refuses command unless a human at a terminal runs it. A refusal is
// appended to the denial log (time, command, reason, pid, cwd; no environment
// value) and returned as an error wrapping ErrHumanOnly that names the
// command and the detector. A log that cannot be written never lets the
// command through: the refusal stands and the error says the log failed.
// It never prompts.
func Check(command string) error {
	r := Detect()
	if r == nil {
		return nil
	}
	cwd, _ := os.Getwd()
	if lerr := audit.AppendDenial(audit.Denial{Kind: "human-only", Command: command, Reason: r.String(), PID: os.Getpid(), Cwd: cwd}); lerr != nil {
		return fmt.Errorf("%s: %w (%s); refusal could not be logged: %v", command, ErrHumanOnly, r, lerr)
	}
	return fmt.Errorf("%s: %w (%s)", command, ErrHumanOnly, r)
}

// Gate is Check followed by the mandatory confirmation: it prints about (one
// line saying what is about to be written) on out, asks the human to type
// yes, and returns an error wrapping ErrAborted on anything else. Call it
// before the first write.
func Gate(command, about string, out io.Writer) error {
	if err := Check(command); err != nil {
		return err
	}
	fmt.Fprintln(out, about)
	ok, err := Confirm("Type yes to continue: ")
	if err != nil {
		return fmt.Errorf("%s: %w (%v)", command, ErrAborted, err)
	}
	if !ok {
		return fmt.Errorf("%s: %w", command, ErrAborted)
	}
	return nil
}

// confirmOnTerminal prints prompt on stderr (so a --json stdout stays clean)
// and reads one line from the terminal itself (CONIN$ or the Cygwin pty on
// Windows, /dev/tty elsewhere), so a redirected stdin can never answer for
// the human.
func confirmOnTerminal(prompt string) (bool, error) {
	tty, done, err := openTTY()
	if err != nil {
		return false, fmt.Errorf("no terminal to read the confirmation from: %w", err)
	}
	defer done()
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(tty).ReadString('\n')
	if err != nil && line == "" {
		return false, err
	}
	return answerIsYes(line), nil
}

// answerIsYes accepts the word yes alone, in any case, with surrounding
// whitespace; y, no, an empty line or anything longer aborts.
func answerIsYes(line string) bool {
	return strings.EqualFold(strings.TrimSpace(line), "yes")
}

// Detector describes one detector on this OS, for tq doctor.
type Detector struct {
	Name      string `json:"name"`
	Method    string `json:"method"`
	Available bool   `json:"available"`
}

// Detectors reports which detectors work on this OS and how.
func Detectors() []Detector {
	return []Detector{
		{DetectorTTY, "stdin and stdout are terminals", true},
		{DetectorEnvMarker, fmt.Sprintf("%d harness variables", len(Markers)), true},
		{DetectorProcessTree, processTreeMethod, processTreeMethod != ""},
	}
}

// selfPID is the pid the walk starts from.
func selfPID() int { return os.Getpid() }
