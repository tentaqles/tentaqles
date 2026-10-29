package cmd

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
	"github.com/tentaqles/tentaqles/internal/packs/ledger"
)

// A pack approval (spec C §2.11) is answered by a human present at a real
// terminal: tq hub answer refuses a pipe, shows the binding, and requires the
// binding to be typed back (pack, action and a 12-hex code of the subject).
// --choice alone never answers one. The answer goes straight to the store
// (never through the running hub's API, which refuses pack approvals).

// packApprovalDecision finds the decision on the card and reports whether it
// is a pending pack approval.
func packApprovalDecision(ctx context.Context, h *hubConn, cardID, decisionID string) (store.Decision, bool) {
	detail, err := h.ops.Show(ctx, cardID)
	if err != nil {
		return store.Decision{}, false
	}
	for _, d := range detail.Decisions {
		if d.ID == decisionID && d.PackApproval() != "" {
			return d, true
		}
	}
	return store.Decision{}, false
}

// subjectCode is the first 12 hex digits of sha256(subject): what the human
// types to confirm they read the binding.
func subjectCode(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:])[:12]
}

// errPackAnswerTTY refuses a pack-approval answer without a terminal.
var errPackAnswerTTY = errors.New("a pack approval is answered by a human at a terminal: run tq hub answer in your own terminal (or answer it on the board in your browser); a pipe or an agent's shell cannot answer it")

func answerPackApproval(c *cobra.Command, h *hubConn, cardID string, d store.Decision, choice, note string) error {
	if hubAgent() || !stdinIsTerminal(c.InOrStdin()) {
		return errPackAnswerTTY
	}
	str := func(k string) string { s, _ := d.Data[k].(string); return s }
	pack, action, subject, target := d.PackApproval(), str("action"), str("subject"), str("target")
	out := c.OutOrStdout()
	fmt.Fprintf(out, "Pack approval %s on card %s\n  pack:    %s\n  action:  %s\n  subject: %s\n  target:  %s\n  expires: %s\n", d.ID, cardID, pack, action, subject, target, str("expires_at"))
	if ev, ok := d.Data["evidence"].(map[string]any); ok {
		names := make([]string, 0, len(ev))
		for k := range ev {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			fmt.Fprintf(out, "  evidence %s: %v\n", k, ev[k])
		}
	}
	want := pack + " " + action + " " + subjectCode(subject)
	fmt.Fprintf(out, "\nTo answer %q, type: %s\n> ", choice, want)
	line, _ := bufio.NewReader(c.InOrStdin()).ReadString('\n')
	if strings.Join(strings.Fields(line), " ") != want {
		return errors.New("the confirmation did not match the binding; nothing was answered")
	}
	if h.ws == nil {
		return errors.New("no workspace for this hub")
	}
	if d.CardID != cardID {
		return fmt.Errorf("decision %s belongs to card %s, not %s", d.ID, d.CardID, cardID)
	}
	// The human is present: sign the answer with the workspace's ledger key
	// (created on first use, in the OS keychain) and answer through the
	// service, which refuses a pack approval without this marker.
	key, err := ledger.SigningKey(h.ws.Name)
	if err != nil {
		return fmt.Errorf("the pack approval cannot be signed: %w (pack approvals need an OS keychain)", err)
	}
	svc := h.svc
	if svc == nil {
		s, err := newHubService(h.ws, warnTo(c), false)
		if err != nil {
			return err
		}
		defer s.Store().Close()
		svc = s
	}
	ctx := runner.WithHumanPresence(context.Background(), runner.HumanPresence{Sign: store.PackSigner(ledger.AnswerSigner(h.ws.Name, key))})
	got, err := svc.Answer(ctx, cardID, d.ID, choice, note)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "decision %s: %s by %s\n", got.Decision.ID, got.Decision.Choice, got.Decision.Actor)
	return nil
}
