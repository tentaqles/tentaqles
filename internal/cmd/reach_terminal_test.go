package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/hooks"
	"github.com/tentaqles/tentaqles/internal/humanonly"
	"github.com/tentaqles/tentaqles/internal/reach"
	"github.com/tentaqles/tentaqles/internal/termopen"
	"github.com/tentaqles/tentaqles/internal/trust"
)

// handover is what reach did to the terminal seams and to its output, in order. The launcher, the
// killer and the pause (a read of stdin) record into it, and so does every line reach prints, so a
// test can assert the order of the whole handover (ADR-0019).
type handover struct {
	events     []string
	launched   bool
	openErr    error     // what the launcher returns
	killErr    error     // what the killer returns
	parentGone bool      // the parent cannot be read once the new terminal has started
	input      io.Reader // what the developer types at the pause; nil reads as a closed input
}

func (h *handover) add(e string) { h.events = append(h.events, e) }

func (h *handover) reset() {
	h.events = nil
	h.launched = false
}

// at is the position of the first event with prefix, or -1.
func (h *handover) at(prefix string) int {
	for i, e := range h.events {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// outAt is the position of the first line reach printed that contains sub, or -1.
func (h *handover) outAt(sub string) int {
	for i, e := range h.events {
		if strings.HasPrefix(e, "out: ") && strings.Contains(e, sub) {
			return i
		}
	}
	return -1
}

// seamCalls counts what reached the launcher, the killer or the pause, not the lines printed.
func (h *handover) seamCalls() int {
	n := 0
	for _, e := range h.events {
		if !strings.HasPrefix(e, "out: ") {
			n++
		}
	}
	return n
}

func (h *handover) count(prefix string) int {
	n := 0
	for _, e := range h.events {
		if strings.HasPrefix(e, prefix) {
			n++
		}
	}
	return n
}

type eventWriter struct{ h *handover }

func (w eventWriter) Write(p []byte) (int, error) {
	w.h.add("out: " + string(p))
	return len(p), nil
}

type eventReader struct{ h *handover }

func (r eventReader) Read(p []byte) (int, error) {
	r.h.add("read")
	if r.h.input == nil {
		return 0, io.EOF
	}
	return r.h.input.Read(p)
}

// isolateHandover swaps the terminal seams for fakes that record into h, and the profiles for temp
// files under home, and restores them at the end of the test. No real terminal opens or closes and
// no real profile is touched. A test that wants the real hook check puts its own ProfilesFn back.
func isolateHandover(t *testing.T, home string) *handover {
	t.Helper()
	h := &handover{}
	profiles := hooks.Profiles{}
	for _, sh := range hooks.Shells {
		profiles[sh] = filepath.Join(home, "profiles", "profile-"+string(sh))
	}
	prevOpen, prevKill := reachOpenTerminal, reachKill
	prevProfiles, prevDetect := hooks.ProfilesFn, reachDetectShells
	reachOpenTerminal = func(dir string) (termopen.Launch, error) {
		h.add("launch " + dir)
		h.launched = true
		if h.openErr != nil {
			return termopen.Launch{}, h.openErr
		}
		return termopen.Launch{Name: "fake-terminal", Dir: dir}, nil
	}
	reachKill = func(pid int) error {
		h.add(fmt.Sprintf("kill %d", pid))
		return h.killErr
	}
	hooks.ProfilesFn = func() hooks.Profiles { return profiles }
	// Only the shell reach runs in gets a profile: the detection of this machine stays out.
	reachDetectShells = func(hooks.Profiles) []hooks.Shell { return nil }
	t.Cleanup(func() {
		reachOpenTerminal, reachKill = prevOpen, prevKill
		hooks.ProfilesFn, reachDetectShells = prevProfiles, prevDetect
	})
	return h
}

// reachChain makes the processes above this test process: chain[0] is reach's direct parent, and each
// one's parent is the next. Above the chain sit a WindowsTerminal and explorer, so the human check of
// the process tree still passes. Once the new terminal has started, h.parentGone makes the parent
// unreadable.
func reachChain(t *testing.T, h *handover, chain ...humanonly.Parent) {
	t.Helper()
	prev := humanonly.ParentOf
	t.Cleanup(func() { humanonly.ParentOf = prev })
	terminal := humanonly.Parent{PID: 80, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}
	explorer := humanonly.Parent{PID: 70, Name: "explorer.exe", Cmdline: "explorer", CmdlineKnown: true}
	above := map[int]humanonly.Parent{terminal.PID: explorer}
	for i, p := range chain {
		if i+1 < len(chain) {
			above[p.PID] = chain[i+1]
		} else {
			above[p.PID] = terminal
		}
	}
	humanonly.ParentOf = func(pid int) (humanonly.Parent, bool) {
		if pid == os.Getpid() {
			if len(chain) == 0 || (h.parentGone && h.launched) {
				return humanonly.Parent{}, false
			}
			return chain[0], true
		}
		p, ok := above[pid]
		return p, ok
	}
}

// runReachH runs reach with its output and its input going through h, so the events show the order.
// A run that waits for input fails rather than hanging the suite.
func runReachH(t *testing.T, h *handover, args ...string) (string, error) {
	t.Helper()
	root := NewRoot()
	var out bytes.Buffer
	root.SetOut(io.MultiWriter(&out, eventWriter{h}))
	root.SetErr(&out)
	root.SetIn(eventReader{h})
	root.SetArgs(append([]string{"reach"}, args...))
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err := <-done:
		return out.String(), err
	case <-time.After(30 * time.Second):
		t.Fatalf("reach waited for input it was never given:\n%s", out.String())
		return "", nil
	}
}

var (
	pwshParent = humanonly.Parent{PID: 4321, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true}
)

// TestReach_Handover is U-01: the launcher gets the client's folder, then the output is printed, then
// the Enter prompt, then the killer gets the parent's pid, in that order.
func TestReach_Handover(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	launch := h.at("launch " + dir)
	ready := h.outAt("is ready")
	enter := h.outAt("Press Enter to close this terminal.")
	read := h.at("read")
	kill := h.at("kill 4321")
	if launch < 0 || ready <= launch || enter <= ready || read <= enter || kill <= read {
		t.Fatalf("order of the handover: %v", h.events)
	}
	if n := h.count("kill "); n != 1 {
		t.Fatalf("%d processes ended, want only the parent: %v", n, h.events)
	}
}

// TestReach_InstallsProfileHook is U-02: with a profile and no block, the block is written after the
// yes, the confirmation says so, and a second reach writes nothing more.
func TestReach_InstallsProfileHook(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	profile := hooks.ProfilesFn()["pwsh"]
	var atPrompt []byte
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) {
		atPrompt, _ = os.ReadFile(profile)
		h.add("confirm")
		return true, nil
	}
	t.Cleanup(func() { humanonly.Confirm = prev })

	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(atPrompt) != 0 {
		t.Fatalf("the profile was written before the yes: %q", atPrompt)
	}
	if !strings.Contains(out, "write the tq activation block into "+profile) {
		t.Fatalf("the confirmation does not say the profile is written:\n%s", out)
	}
	got, _ := os.ReadFile(profile)
	if string(got) != hooks.Block("pwsh") {
		t.Fatalf("profile = %q, want the tq block", got)
	}

	h.reset()
	out, err = runReachH(t, h, filepath.Join(clients, "beta"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(out, "activation block") {
		t.Fatalf("a second reach asks about the profile again:\n%s", out)
	}
	again, _ := os.ReadFile(profile)
	if !bytes.Equal(got, again) {
		t.Fatalf("a second reach changed the profile: %q", again)
	}
	if _, err := os.Stat(profile + ".tq-backup"); err == nil {
		t.Fatal("a second reach backed the profile up")
	}
}

// TestReach_NoTerminalFlag is U-03: --no-terminal opens nothing and closes nothing, and the next step
// prints.
func TestReach_NoTerminalFlag(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner", "--no-terminal")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if h.count("launch") != 0 || h.count("kill") != 0 || h.count("read") != 0 {
		t.Fatalf("--no-terminal reached a terminal seam: %v", h.events)
	}
	want := "Open a new terminal in " + dir + ", then start claude and log in with this client's account."
	if !strings.Contains(out, want) {
		t.Fatalf("the next step does not print:\n%s", out)
	}
}

// TestReach_TerminalWontOpen is F-01: the new terminal can't start, so the old one stays open with the
// exact line, the killer is not called, reach exits 0, and the client is trusted.
func TestReach_TerminalWontOpen(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.openErr = errors.New("no terminal emulator found")
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner")
	if err != nil {
		t.Fatalf("exit must be 0: %v\n%s", err, out)
	}
	line := "Could not open a terminal (no terminal emulator found). Open one in " + dir + " to start working.\n"
	if !strings.Contains(out, line) {
		t.Fatalf("the line does not print:\n%s", out)
	}
	if h.count("kill") != 0 || h.count("read") != 0 {
		t.Fatalf("the old terminal was ended or paused for: %v", h.events)
	}
	if !trust.IsTrusted("acme", manifestHash(t, dir)) {
		t.Fatal("the client is not trusted")
	}
}

// TestReach_OldTerminalNotEnded is F-02: a parent that is not a shell reach knows, an unreadable
// parent, and a kill that fails each leave the terminal open with the line, and reach exits 0.
func TestReach_OldTerminalNotEnded(t *testing.T) {
	cases := []struct {
		name    string
		parent  humanonly.Parent
		gone    bool  // the parent cannot be read once the new terminal has started
		killErr error // what the killer returns
		kills   int   // the killer is called for the parent, when the parent is a shell
	}{
		{"explorer", humanonly.Parent{PID: 4321, Name: "explorer.exe", Cmdline: "explorer", CmdlineKnown: true}, false, nil, 0},
		{"node", humanonly.Parent{PID: 4321, Name: "node.exe", Cmdline: "node C:/tools/cli.js", CmdlineKnown: true}, false, nil, 0},
		{"python", humanonly.Parent{PID: 4321, Name: "python.exe", Cmdline: "python reach.py", CmdlineKnown: true}, false, nil, 0},
		{"unreadable", pwshParent, true, nil, 0},
		{"kill fails", pwshParent, false, errors.New("access denied"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
			h := isolateHandover(t, filepath.Dir(clients))
			h.killErr = tc.killErr
			h.parentGone = tc.gone
			reachChain(t, h, tc.parent)
			out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
			if err != nil {
				t.Fatalf("exit must be 0: %v\n%s", err, out)
			}
			if !strings.Contains(out, "Close this terminal when you are ready.") {
				t.Fatalf("the line does not print:\n%s", out)
			}
			if n := h.count("kill "); n != tc.kills {
				t.Fatalf("kills = %d, want %d: %v", n, tc.kills, h.events)
			}
		})
	}
}

// TestReach_PauseClosedInput is F-03: a closed input at the pause counts as Enter. reach proceeds and
// ends the parent, and does not wait.
func TestReach_PauseClosedInput(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.input = strings.NewReader("")
	reachChain(t, h, pwshParent)
	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if h.at("kill 4321") < 0 {
		t.Fatalf("the parent was not ended after a closed input: %v", h.events)
	}
}

// TestReach_NoHandoverWithoutANewClient is R-01: an existing client, a declined yes and a bad folder
// call neither seam.
func TestReach_NoHandoverWithoutANewClient(t *testing.T) {
	clients := isolateReach(t, fullAnswers())
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	if out, err := runReachH(t, h, dir, "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	h.reset()

	// An existing client: shown, and nothing else.
	if out, err := runReachH(t, h, dir, "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if n := h.seamCalls(); n != 0 {
		t.Fatalf("an existing client reached a terminal seam: %v", h.events)
	}

	// A declined yes: nothing is written, so nothing is handed over.
	h.reset()
	prev := humanonly.Confirm
	humanonly.Confirm = func(string) (bool, error) { return false, nil }
	t.Cleanup(func() { humanonly.Confirm = prev })
	if _, err := runReachH(t, h, filepath.Join(clients, "gamma"), "--no-banner"); !errors.Is(err, humanonly.ErrAborted) {
		t.Fatalf("a declined yes: err = %v", err)
	}
	if n := h.seamCalls(); n != 0 {
		t.Fatalf("a declined yes reached a terminal seam: %v", h.events)
	}
	humanonly.Confirm = prev

	// Bad folders: a name that cannot name a client, and a file.
	h.reset()
	if _, err := runReachH(t, h, filepath.Join(clients, "Has Spaces"), "--no-banner"); err == nil {
		t.Fatal("a bad name was accepted")
	}
	file := filepath.Join(clients, "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runReachH(t, h, file, "--no-banner"); err == nil {
		t.Fatal("a file was accepted")
	}
	if n := h.seamCalls(); n != 0 {
		t.Fatalf("a bad folder reached a terminal seam: %v", h.events)
	}
}

// TestReach_KillsOnlyADirectShellParent is S-01: under python under pwsh nothing is ended; under pwsh,
// only that pid is.
func TestReach_KillsOnlyADirectShellParent(t *testing.T) {
	t.Run("python under pwsh", func(t *testing.T) {
		clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
		h := isolateHandover(t, filepath.Dir(clients))
		reachChain(t, h,
			humanonly.Parent{PID: 4321, Name: "python.exe", Cmdline: "python reach.py", CmdlineKnown: true},
			humanonly.Parent{PID: 4322, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true})
		out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if n := h.count("kill"); n != 0 {
			t.Fatalf("%d processes ended under python: %v", n, h.events)
		}
		if !strings.Contains(out, "Close this terminal when you are ready.") {
			t.Fatalf("the line does not print:\n%s", out)
		}
	})
	t.Run("pwsh under pwsh", func(t *testing.T) {
		clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
		h := isolateHandover(t, filepath.Dir(clients))
		reachChain(t, h, pwshParent,
			humanonly.Parent{PID: 4322, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true})
		out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if n := h.count("kill "); n != 1 || h.at("kill 4321") < 0 {
			t.Fatalf("want only pid 4321 ended: %v", h.events)
		}
	})
}

// TestReach_ProfileBlockIsTqHooksInstall is S-04: the bytes reach writes equal what tq hooks install
// writes, so tq hooks remove takes them back.
func TestReach_ProfileBlockIsTqHooksInstall(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	profile := hooks.ProfilesFn()["pwsh"]
	if out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := os.ReadFile(profile)

	other := filepath.Join(t.TempDir(), "profile.ps1")
	if _, err := hooks.Install("pwsh", hooks.Profiles{"pwsh": other}); err != nil {
		t.Fatal(err)
	}
	installed, _ := os.ReadFile(other)
	if !bytes.Equal(got, installed) {
		t.Fatalf("reach wrote %q, tq hooks install writes %q", got, installed)
	}

	if _, err := hooks.Remove("pwsh", hooks.Profiles{"pwsh": profile}); err != nil {
		t.Fatal(err)
	}
	if st := hooks.StatusOf("pwsh", hooks.Profiles{"pwsh": profile}); st.State != "missing" {
		t.Fatalf("after tq hooks remove: %s", st.State)
	}
	if after, _ := os.ReadFile(profile); len(after) != 0 {
		t.Fatalf("tq hooks remove left %q", after)
	}
}

// A profile that cannot take the block keeps the old terminal open, and no new one opens: a terminal
// that would not route into the client runs under no identity. The client is still made and trusted.
func TestReach_HookFailureOpensNothing(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	profiles := hooks.Profiles{"pwsh": filepath.Join(blocker, "profile.ps1")}
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return profiles }
	t.Cleanup(func() { hooks.ProfilesFn = prev })

	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner")
	if err != nil {
		t.Fatalf("reach must exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "The tq activation block could not be written") {
		t.Fatalf("the failure does not print:\n%s", out)
	}
	if h.count("launch") != 0 || h.count("kill") != 0 || h.count("read") != 0 {
		t.Fatalf("a terminal was opened or ended: %v", h.events)
	}
	if !trust.IsTrusted("acme", manifestHash(t, dir)) {
		t.Fatal("the client is not trusted")
	}
}
