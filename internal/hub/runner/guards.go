package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// gitProbe bounds the local git call of the preflight (no network: rev-parse reads .git only).
const gitProbe = 10 * time.Second

// ErrNotARepo marks a GitPreflight refusal whose folder is outside every git repository.
var ErrNotARepo = errors.New("not a git repository")

type notRepoError struct{ msg string }

func (e notRepoError) Error() string        { return e.msg }
func (e notRepoError) Is(target error) bool { return target == ErrNotARepo }

// GitPreflight checks, before a round, that git is on PATH and that dir lies inside a git work
// tree. label is the card's project folder as the human set it ("" when the card has none, so
// dir is the workspace root).
func GitPreflight(ctx context.Context, dir, label string) error {
	bin, err := exec.LookPath("git")
	if err != nil {
		return errors.New("git is not on PATH; the build workflow needs it. Install git (or fix PATH for the hub), then resume")
	}
	ctx, cancel := context.WithTimeout(ctx, gitProbe)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-C", dir, "rev-parse", "--is-inside-work-tree")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return fmt.Errorf("git did not answer within %s in %s; resume to try again", gitProbe, dir)
	}
	if err == nil && strings.TrimSpace(string(out)) == "true" {
		return nil
	}
	if label == "" {
		return notRepoError{fmt.Sprintf("The card has no project folder, so the run would start in the workspace root %s, which is not a git repository. Set the card's project folder (relative to the workspace root) to a git repository, then resume", dir)}
	}
	return notRepoError{fmt.Sprintf("The project folder %s (%s) is not inside a git repository. Point the card at a git repository (or git init it), then resume", label, dir)}
}

// preflight runs the zero-cost checks before every round, first and resumed. ok=false means
// the run is held on a hub decision and no round starts.
func (s *Service) preflight(ctx context.Context, r store.Run, card store.Card, dir string) bool {
	if s.cfg.Trusted != nil {
		if err := s.cfg.Trusted(); err != nil {
			s.hold(r, card, "untrusted", fmt.Sprintf("The workspace manifest changed and is no longer trusted. Run `tq allow %s` in a terminal, then resume.", s.cfg.Workspace),
				"No round was started: "+err.Error()+".")
			return false
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		s.hold(r, card, "project folder missing", fmt.Sprintf("The run's folder %s does not exist. Set the card's project folder (relative to the workspace root) or create the folder, then resume.", dir), "No round was started.")
		return false
	}
	if s.cfg.RepoCheck != nil {
		label := r.ProjectDir
		if r.Worktree != "" {
			label = r.Worktree
		}
		if err := s.cfg.RepoCheck(ctx, dir, label); err != nil {
			reason := "git preflight"
			if errors.Is(err, ErrNotARepo) {
				reason = ErrNotARepo.Error()
			}
			msg := err.Error()
			msg = strings.ToUpper(msg[:1]) + msg[1:] + "."
			s.hold(r, card, reason, msg, "No round was started.")
			return false
		}
	}
	return true
}

// hold stops the run for the human without ending it: status waiting (the card stays locked),
// one hub decision (resume / stop) and a card comment. Resume requeues the same run.
func (s *Service) hold(r store.Run, card store.Card, reason, question, note string) {
	r.Status, r.StopReason = store.StatusWaiting, reason
	s.save(r)
	_, _ = s.st.AskHub(card.ID, r.ID, r.Slug, question, []string{"resume", "stop"}, map[string]any{"hold": reason})
	s.comment(card, r, strings.TrimSpace(question+" "+note))
	_ = s.st.Log(activity.Entry{Actor: "hub", Verb: activity.VerbRunWait, CardID: card.ID, RunID: r.ID, Data: map[string]any{"hold": reason}})
	s.notify("tq hub: waiting on you", card.Title+": "+reason)
}

// resumeHeld requeues a run held by hold (the human chose resume on its decision).
func (s *Service) resumeHeld(r store.Run) *store.Run {
	s.enqMu.Lock()
	defer s.enqMu.Unlock()
	if cur, err := s.st.Run(r.ID); err == nil {
		r = cur
	}
	if !r.Active() || r.Status != store.StatusWaiting {
		return nil
	}
	r.Status, r.NotBefore, r.StopReason = store.StatusQueued, s.stamp(), ""
	if r.Slug != "" {
		r.Phase = store.PhaseResume
	}
	s.save(r)
	return &r
}
