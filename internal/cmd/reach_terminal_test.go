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

// handover is what reach did to the terminal seams and to its output, in order. The launcher, the killer,
// the probe and the pause (a read of stdin) record into it, and so does every line reach prints, so a test
// can assert the order of the whole handover (ADR-0019).
type handover struct {
	events   []string
	launched bool
	openErr  error         // what the launcher returns
	killErr  error         // what the killer returns
	probeErr error         // what the PowerShell probe returns
	shell    string        // the Shell of the window the launcher opens ("powershell" for the fallback)
	detect   []hooks.Shell // the shells tq hooks besides the one reach runs in
	env      []string      // the environment the launcher was given
	after    *afterLaunch  // what the parent reads as once the window has opened; nil leaves it unchanged
	onPause  func()        // runs when the pause reads
	input    io.Reader     // what the developer types at the pause; nil reads as a closed input
}

// afterLaunch is what reach's parent reads as once the new window has opened.
type afterLaunch struct {
	p  humanonly.Parent
	ok bool
}

func (h *handover) add(e string) { h.events = append(h.events, e) }

func (h *handover) reset() {
	h.events = nil
	h.launched = false
	h.env = nil
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
	if r.h.onPause != nil {
		r.h.onPause()
	}
	if r.h.input == nil {
		return 0, io.EOF
	}
	return r.h.input.Read(p)
}

// isolateHandover swaps the terminal seams for fakes that record into h, and the profiles for temp files
// under home, and restores them at the end of the test. No real terminal opens or closes, no real profile
// is touched and no PowerShell runs. A test that wants the real hook check puts its own ProfilesFn back.
func isolateHandover(t *testing.T, home string) *handover {
	t.Helper()
	h := &handover{}
	profiles := hooks.Profiles{}
	for _, sh := range hooks.Shells {
		profiles[sh] = filepath.Join(home, "profiles", "profile-"+string(sh))
	}
	prevOpen, prevKill, prevProbe := reachOpenTerminal, reachKill, reachProbe
	prevProfiles, prevDetect := hooks.ProfilesFn, reachDetectShells
	reachOpenTerminal = func(dir string, env []string) (termopen.Launch, error) {
		h.add("launch " + dir)
		h.launched = true
		h.env = env
		if h.openErr != nil {
			return termopen.Launch{}, h.openErr
		}
		return termopen.Launch{Name: "fake-terminal", Dir: dir, Shell: h.shell}, nil
	}
	reachKill = func(pid int) error {
		h.add(fmt.Sprintf("kill %d", pid))
		return h.killErr
	}
	reachProbe = func(string, string) error { return h.probeErr }
	hooks.ProfilesFn = func() hooks.Profiles { return profiles }
	reachDetectShells = func(hooks.Profiles) []hooks.Shell { return h.detect }
	t.Cleanup(func() {
		reachOpenTerminal, reachKill, reachProbe = prevOpen, prevKill, prevProbe
		hooks.ProfilesFn, reachDetectShells = prevProfiles, prevDetect
	})
	return h
}

// reachChain makes the processes above this test process: chain[0] is reach's direct parent, and each
// one's parent is the next. Above the chain sit a WindowsTerminal and explorer, so the human check of the
// process tree still passes. Once the new window has opened, h.after is what the parent reads as.
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
			if h.launched && h.after != nil {
				return h.after.p, h.after.ok
			}
			if len(chain) == 0 {
				return humanonly.Parent{}, false
			}
			return chain[0], true
		}
		p, ok := above[pid]
		return p, ok
	}
}

// runReachH runs reach with its output and its input going through h, so the events show the order. A run
// that waits for input fails rather than hanging the suite.
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

var pwshParent = humanonly.Parent{PID: 4321, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true}

// TestReach_Handover is U-01: the launcher gets the client's folder, then the output is printed, then the
// Enter prompt, then the killer gets the parent's pid, in that order.
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

// TestReach_InstallsProfileHook is U-02: with a profile and no block, the block is written after the yes,
// the confirmation says so, and a second reach writes nothing more.
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
	if string(again) != string(got) {
		t.Fatalf("a second reach changed the profile: %q", again)
	}
	if _, err := os.Stat(profile + ".tq-backup"); err == nil {
		t.Fatal("a second reach backed the profile up")
	}
}

// TestReach_NoTerminalFlag is U-03: --no-terminal opens nothing and closes nothing, and the next step prints.
func TestReach_NoTerminalFlag(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner", "--no-terminal")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if h.seamCalls() != 0 {
		t.Fatalf("--no-terminal reached a terminal seam: %v", h.events)
	}
	want := "Open a new terminal in " + dir + ", then start claude and log in with this client's account."
	if !strings.Contains(out, want) {
		t.Fatalf("the next step does not print:\n%s", out)
	}
}

// TestReach_TerminalWontOpen is F-01: the new terminal can't start, so the old one stays open with the exact
// line, the killer is not called, reach exits 0, and the client is trusted.
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

// TestReach_OldTerminalNotEnded is F-02, and the guards of the kill (L-07): a parent that is not a shell
// reach may end, an unreadable, reused or unknown-cmdline parent once the window has opened, a shell that
// runs a command or a script, and a kill that fails each leave the terminal open with the line, and reach
// exits 0.
func TestReach_OldTerminalNotEnded(t *testing.T) {
	pwshCmd := humanonly.Parent{PID: 4321, Name: "pwsh.exe", Cmdline: `pwsh.exe -Command "tq reach"`, CmdlineKnown: true}
	bashCmd := humanonly.Parent{PID: 4321, Name: "bash", Cmdline: `bash -c "tq reach"`, CmdlineKnown: true}
	cmdRun := humanonly.Parent{PID: 4321, Name: "cmd.exe", Cmdline: "cmd /c tq reach", CmdlineKnown: true}
	cases := []struct {
		name    string
		parent  humanonly.Parent
		after   *afterLaunch // what the parent reads as once the window has opened
		killErr error
		kills   int // the killer is called for the parent, when the parent may be ended
	}{
		{"explorer", humanonly.Parent{PID: 4321, Name: "explorer.exe", Cmdline: "explorer", CmdlineKnown: true}, nil, nil, 0},
		{"node", humanonly.Parent{PID: 4321, Name: "node.exe", Cmdline: "node C:/tools/cli.js", CmdlineKnown: true}, nil, nil, 0},
		{"python", humanonly.Parent{PID: 4321, Name: "python.exe", Cmdline: "python reach.py", CmdlineKnown: true}, nil, nil, 0},
		{"pwsh runs a command", pwshCmd, nil, nil, 0},
		{"bash runs a command", bashCmd, nil, nil, 0},
		{"cmd runs a command", cmdRun, nil, nil, 0},
		{"unreadable after the window opened", pwshParent, &afterLaunch{ok: false}, nil, 0},
		{"reused after the window opened", pwshParent, &afterLaunch{p: humanonly.Parent{PID: 4321, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true, Reused: true}, ok: true}, nil, 0},
		{"cmdline unknown after the window opened", pwshParent, &afterLaunch{p: humanonly.Parent{PID: 4321, Name: "pwsh.exe", CmdlineKnown: false}, ok: true}, nil, 0},
		{"kill fails", pwshParent, nil, errors.New("access denied"), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
			h := isolateHandover(t, filepath.Dir(clients))
			h.killErr = tc.killErr
			reachChain(t, h, tc.parent)
			if tc.after != nil {
				// The replacement is set once the window has opened, so the check before it sees the parent.
				prevOpen := reachOpenTerminal
				reachOpenTerminal = func(dir string, env []string) (termopen.Launch, error) {
					h.after = tc.after
					return prevOpen(dir, env)
				}
				t.Cleanup(func() { reachOpenTerminal = prevOpen })
			}
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

// TestReach_PauseClosedInput is F-03: a closed input at the pause counts as Enter. reach proceeds and ends
// the parent, and does not wait.
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

// TestReach_ParentChangedAtPauseNotEnded: the pid reach identified is not the parent any more when the
// developer presses Enter (the shell ended, and its pid may belong to another process). Nothing is ended.
func TestReach_ParentChangedAtPauseNotEnded(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.input = strings.NewReader("\n")
	h.onPause = func() {
		h.after = &afterLaunch{p: humanonly.Parent{PID: 4400, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true}, ok: true}
	}
	reachChain(t, h, pwshParent)
	// The replacement shell is a real one under a terminal, so only the changed pid can stop the kill.
	chained := humanonly.ParentOf
	humanonly.ParentOf = func(pid int) (humanonly.Parent, bool) {
		if pid == 4400 {
			return humanonly.Parent{PID: 80, Name: "WindowsTerminal.exe", Cmdline: "wt", CmdlineKnown: true}, true
		}
		return chained(pid)
	}
	t.Cleanup(func() { humanonly.ParentOf = chained })
	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if h.count("kill") != 0 {
		t.Fatalf("a parent that changed during the pause was ended: %v", h.events)
	}
	if !strings.Contains(out, "Close this terminal when you are ready.") {
		t.Fatalf("the line does not print:\n%s", out)
	}
}

// TestReach_NoHandoverWithoutANewClient is R-01: an existing client, a declined yes and a bad folder call
// neither seam.
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

// TestReach_KillsOnlyADirectShellParent is S-01: under python under pwsh nothing is ended; under a terminal,
// only the pwsh pid is; and a shell started by a shell is not ended either (it is a Git Bash stub, or a
// nested shell, and ending it leaves the terminal open with no message).
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
	t.Run("pwsh under a terminal", func(t *testing.T) {
		clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
		h := isolateHandover(t, filepath.Dir(clients))
		reachChain(t, h, pwshParent)
		out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if n := h.count("kill "); n != 1 || h.at("kill 4321") < 0 {
			t.Fatalf("want only pid 4321 ended: %v", h.events)
		}
	})
	t.Run("shell under a shell", func(t *testing.T) {
		clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
		h := isolateHandover(t, filepath.Dir(clients))
		reachChain(t, h, pwshParent,
			humanonly.Parent{PID: 4322, Name: "pwsh.exe", Cmdline: "pwsh.exe -NoLogo", CmdlineKnown: true})
		out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if n := h.count("kill"); n != 0 {
			t.Fatalf("a shell under a shell was ended: %v", h.events)
		}
		if !strings.Contains(out, "Close this terminal when you are ready.") {
			t.Fatalf("the line does not print:\n%s", out)
		}
	})
}

// TestReach_GitBashStubNotEnded: a native program run from Git Bash has a bash.exe stub as its direct
// parent, and the stub's parent is the interactive bash. Ending the stub leaves that bash open with no
// message, so reach ends nothing and says so (REQ-5, ADR-0019).
func TestReach_GitBashStubNotEnded(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h,
		humanonly.Parent{PID: 4321, Name: "bash.exe", Cmdline: "/usr/bin/bash", CmdlineKnown: true},
		humanonly.Parent{PID: 4322, Name: "bash.exe", Cmdline: "bash --login -i", CmdlineKnown: true})
	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if h.count("kill") != 0 || h.count("read") != 0 {
		t.Fatalf("the Git Bash stub was ended or paused for: %v", h.events)
	}
	if !strings.Contains(out, "Close this terminal when you are ready.") {
		t.Fatalf("the Git Bash case prints no line:\n%s", out)
	}
}

// TestReach_ProfileBlockIsTqHooksInstall is S-04: the bytes reach writes equal what tq hooks install writes,
// so tq hooks remove takes them back.
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

// A profile that cannot take the block keeps the old terminal open, and no new one opens: a terminal that
// would not route into the client runs under no identity. The client is still made and trusted.
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

// PowerShell that will not load the profile tq wrote (a Restricted policy, or a redirected Documents folder)
// is the same case as a failed write for the shell reach runs in: nothing opens, nothing ends (Q2).
func TestReach_PowerShellProbeFailureOpensNothing(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.probeErr = errors.New("pwsh's execution policy is Restricted, which does not run its profile")
	reachChain(t, h, pwshParent)
	dir := filepath.Join(clients, "acme")
	out, err := runReachH(t, h, dir, "--no-banner")
	if err != nil {
		t.Fatalf("reach must exit 0: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Restricted") {
		t.Fatalf("the policy does not print:\n%s", out)
	}
	if h.count("launch") != 0 || h.count("kill") != 0 {
		t.Fatalf("a terminal was opened or ended: %v", h.events)
	}
	if !trust.IsTrusted("acme", manifestHash(t, dir)) {
		t.Fatal("the client is not trusted")
	}
}

// A failure on another detected shell is a warning naming that shell, and the handover goes on (Q2).
func TestReach_OtherShellFailureWarnsAndHandsOver(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.detect = []hooks.Shell{"bash"}
	reachChain(t, h, pwshParent)
	blocker := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	profiles := hooks.ProfilesFn()
	profiles["bash"] = filepath.Join(blocker, ".bashrc")
	prev := hooks.ProfilesFn
	hooks.ProfilesFn = func() hooks.Profiles { return profiles }
	t.Cleanup(func() { hooks.ProfilesFn = prev })

	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "Warning: bash: the tq activation block could not be written") {
		t.Fatalf("the bash failure does not warn:\n%s", out)
	}
	if h.at("launch ") < 0 || h.at("kill 4321") < 0 {
		t.Fatalf("the handover did not go on: %v", h.events)
	}
}

// The Windows fallback is PowerShell. When its profile is not confirmed to load, the window opens with no
// login line, the old terminal stays open, and the next step is tq run (Q1, ADR-0019).
func TestReach_FallbackWithoutTheHookNamesTqRun(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.shell = "powershell"
	h.detect = []hooks.Shell{"powershell"}
	h.probeErr = errors.New("powershell's execution policy is Restricted")
	reachChain(t, h, humanonly.Parent{PID: 4321, Name: "cmd.exe", Cmdline: "cmd /k", CmdlineKnown: true})
	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "tq run acme -- claude") {
		t.Fatalf("the next step is not tq run:\n%s", out)
	}
	if strings.Contains(out, "log in with this client's account") {
		t.Fatalf("an unconfirmed window gets the login line:\n%s", out)
	}
	if h.count("kill") != 0 {
		t.Fatalf("the old terminal was ended: %v", h.events)
	}
	if !strings.Contains(out, "Close this terminal when you are ready.") {
		t.Fatalf("the line does not print:\n%s", out)
	}
}

// The fallback with a confirmed hook is a normal handover: the login line, the pause, then the old terminal
// ends.
func TestReach_FallbackWithTheHookEndsTheOldTerminal(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	h.shell = "powershell"
	h.detect = []hooks.Shell{"powershell"}
	reachChain(t, h, humanonly.Parent{PID: 4321, Name: "cmd.exe", Cmdline: "cmd /k", CmdlineKnown: true})
	out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(out, "log in with this client's account") {
		t.Fatalf("a confirmed window gets no login line:\n%s", out)
	}
	if h.at("kill 4321") < 0 {
		t.Fatalf("the old terminal was not ended: %v", h.events)
	}
}

// The new terminal starts without tq's identity: the variables a client's hook sets, and the workspace
// markers, are not inherited from the terminal reach was run in (L-01, REQ-3).
func TestReach_NewTerminalInheritsNoIdentity(t *testing.T) {
	clients := isolateReach(t, &reach.Answers{Email: "me@acme.test"})
	h := isolateHandover(t, filepath.Dir(clients))
	reachChain(t, h, pwshParent)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(clients, "other", "claude"))
	t.Setenv("TQ_WS", "other")
	if out, err := runReachH(t, h, filepath.Join(clients, "acme"), "--no-banner"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if len(h.env) == 0 {
		t.Fatal("the new terminal got no environment at all")
	}
	for _, kv := range h.env {
		if strings.HasPrefix(strings.ToUpper(kv), "CLAUDE_CONFIG_DIR=") || strings.HasPrefix(strings.ToUpper(kv), "TQ_WS=") {
			t.Fatalf("the new terminal inherited %s", kv)
		}
	}
}
