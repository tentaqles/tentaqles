package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tentaqles/tentaqles/internal/hub/gate"
	"github.com/tentaqles/tentaqles/internal/hub/runner"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

var ErrNoReview = errors.New("no finished run to review yet")

type Commit struct{ Hash, Subject string }

type ReviewData struct {
	Card        store.Card
	Parent      *store.Card
	Workspace   string
	Run         store.Run
	State       *gate.State
	Records     *gate.Records
	RecordsNote string
	SpecPath    string
	Commits     []Commit
	DiffStat    string
	PRURL       string
	Pending     *store.Decision
	Rounds      []store.RoundRecord
	Standalone  bool
	CSS         template.CSS
	GeneratedAt string
}

// Verdict is the band on top of the review page: what the gate's records say, in one line.
type Verdict struct {
	Class string // ok | wait | bad
	Title string
	Note  string
}

// latestReviews keeps the latest review per (checkpoint, reviewer): latest by timestamp when both carry
// one, else the later in record order. Pairs keep the order of their first appearance.
func latestReviews(all []gate.Review) []gate.Review {
	idx := map[[2]string]int{}
	var out []gate.Review
	for _, rv := range all {
		k := [2]string{string(rv.Checkpoint), rv.Reviewer}
		i, ok := idx[k]
		if !ok {
			idx[k] = len(out)
			out = append(out, rv)
			continue
		}
		if cur := out[i]; cur.TS == "" || rv.TS == "" || rv.TS >= cur.TS {
			out[i] = rv
		}
	}
	return out
}

// Verdict is "ok" only when every checkpoint passed, the DoD is ok, at least one review is recorded and
// no latest review (per checkpoint and reviewer) says anything but PASS; a failing DoD is "bad";
// anything else is "wait", naming what is missing.
func (d ReviewData) Verdict() Verdict {
	passed, total, reviews := 0, 0, 0
	if d.State != nil {
		for _, c := range d.State.Checkpoints {
			total++
			if c.Status == "passed" {
				passed++
			}
		}
	}
	var dod *gate.DoDRecord
	var gaps []string
	if d.Records != nil {
		for _, rv := range latestReviews(d.Records.Reviews) {
			if rv.Result == "PASS" {
				reviews++
			} else {
				gaps = append(gaps, fmt.Sprintf("review %s on checkpoint %s: %s", rv.Reviewer, rv.Checkpoint, rv.Result))
			}
		}
		dod = d.Records.DoD
	}
	if d.Records == nil || len(d.Records.Reviews) == 0 {
		gaps = append([]string{"no review recorded"}, gaps...)
	}
	switch {
	case total == 0:
		gaps = append([]string{"no checkpoint recorded"}, gaps...)
	case passed < total:
		gaps = append([]string{fmt.Sprintf("%d of %d checkpoints passed", passed, total)}, gaps...)
	}
	if dod == nil {
		gaps = append(gaps, "no definition-of-done record")
	}
	facts := fmt.Sprintf("%d of %d checkpoints passed, %d reviews PASS", passed, total, reviews)
	if dod != nil {
		if dod.OK {
			facts += ", DoD ok"
		} else {
			facts += ", DoD not met"
		}
	}
	facts += fmt.Sprintf(", %.0f min, $%.2f", d.Run.Seconds/60, d.Run.CostUSD)
	note := facts + ". Built from signed gate records and the hub's own git summary only; nothing here comes from what the agent said."
	switch {
	case dod != nil && !dod.OK:
		return Verdict{Class: "bad", Title: "The definition of done is not met", Note: note}
	case d.State == nil && d.Records == nil:
		return Verdict{Class: "wait", Title: "There are no gate records for this run", Note: "Read what was done before you close the card. " + d.RecordsNote}
	case len(gaps) == 0:
		return Verdict{Class: "ok", Title: "Everything the gate recorded says this is done", Note: note}
	}
	return Verdict{Class: "wait", Title: "The gate's records are incomplete: read the evidence before you approve", Note: "Missing: " + strings.Join(gaps, "; ") + ". " + note}
}

func init() {
	extraRoutes = append(extraRoutes, func(s *Server) {
		s.handle("GET /cards/{id}/review", s.reviewPage)
		s.handle("POST /cards/{id}/review/approve", s.reviewApprove)
		s.handle("POST /cards/{id}/review/request-changes", s.reviewRequestChanges)
		s.handle("POST /cards/{id}/review/done", s.reviewDone)
	})
}

// BuildReview assembles the review page of spec §10 for the card's latest reviewable run. Status and
// evidence come only from gate records; agent text appears only as comments, never here.
func BuildReview(ctx context.Context, svc *runner.Service, cardID string) (ReviewData, error) {
	st := svc.Store()
	card, err := st.Card(cardID)
	if err != nil {
		return ReviewData{}, err
	}
	runs, err := st.RunsForCard(cardID)
	if err != nil {
		return ReviewData{}, err
	}
	pending, hasPending := svc.PendingResult(cardID)
	var run *store.Run
	for i := range runs {
		if (hasPending && runs[i].ID == pending.RunID) || runs[i].Status == store.StatusFinished {
			r := runs[i]
			run = &r
			break
		}
	}
	if run == nil {
		return ReviewData{}, ErrNoReview
	}
	d := ReviewData{Card: card, Workspace: svc.Workspace(), Run: *run, Rounds: run.RoundLog, GeneratedAt: store.Now()}
	if card.ParentID != "" {
		if p, err := st.Card(card.ParentID); err == nil {
			d.Parent = &p
		}
	}
	if hasPending {
		p := pending
		d.Pending = &p
	}
	dir := svc.RunDir(*run)
	if run.Slug == "" {
		d.RecordsNote = "this run has no build slug, so there are no gate records"
	} else if gst, err := svc.Gate().State(ctx, dir, run.Slug, true); err == nil {
		d.State, d.Records = gst, gst.Records
	} else {
		d.RecordsNote = "gate records unavailable: " + err.Error()
	}
	if d.State == nil && len(run.State) > 0 {
		var gs gate.State
		if json.Unmarshal(run.State, &gs) == nil {
			d.State = &gs
		}
	}
	if d.Records != nil && d.Records.Plan != nil {
		d.SpecPath = d.Records.Plan.Spec
	}
	d.Commits, d.DiffStat = gitSummary(ctx, dir, firstBase(d.State))
	d.PRURL = runner.PRLink(st, card.ID)
	maskReview(st, &d)
	return d, nil
}

func firstBase(st *gate.State) string {
	if st == nil || len(st.Bases) == 0 {
		return ""
	}
	keys := make([]string, 0, len(st.Bases))
	for k := range st.Bases {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, ea := strconv.Atoi(keys[i])
		b, eb := strconv.Atoi(keys[j])
		if ea == nil && eb == nil {
			return a < b
		}
		return keys[i] < keys[j]
	})
	return st.Bases[keys[0]]
}

var hashRe = func(s string) bool {
	if len(s) < 7 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// gitSummary reads commits since the run's base and a diff summary, bounded to 10 s.
func gitSummary(ctx context.Context, dir, base string) ([]Commit, string) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	run := func(args ...string) string {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
		var out bytes.Buffer
		cmd.Stdout = &out
		if cmd.Run() != nil {
			return ""
		}
		return out.String()
	}
	logArgs := []string{"log", "--format=%h%x09%s", "-n", "20"}
	diff := ""
	if hashRe(base) {
		logArgs = []string{"log", "--format=%h%x09%s", base + "..HEAD"}
		diff = strings.TrimSpace(run("diff", "--stat", base+"..HEAD"))
	}
	var commits []Commit
	for _, line := range strings.Split(strings.TrimSpace(run(logArgs...)), "\n") {
		h, s, ok := strings.Cut(line, "\t")
		if ok {
			commits = append(commits, Commit{Hash: h, Subject: s})
		}
	}
	return commits, diff
}

// maskReview masks every string that came from the repository, the plan or the gate's detail text.
func maskReview(st *store.Store, d *ReviewData) {
	for i := range d.Commits {
		d.Commits[i].Subject = st.Mask(d.Commits[i].Subject)
	}
	d.DiffStat = st.Mask(d.DiffStat)
	d.RecordsNote = st.Mask(d.RecordsNote)
	d.SpecPath = st.Mask(d.SpecPath)
	if r := d.Records; r != nil {
		if r.Plan != nil {
			r.Plan.Summary, r.Plan.PlainSummary = st.Mask(r.Plan.Summary), st.Mask(r.Plan.PlainSummary)
			r.Plan.Spec = st.Mask(r.Plan.Spec)
			for i := range r.Plan.Checkpoints {
				r.Plan.Checkpoints[i].Title = st.Mask(r.Plan.Checkpoints[i].Title)
				for j := range r.Plan.Checkpoints[i].Tests {
					r.Plan.Checkpoints[i].Tests[j].File = st.Mask(r.Plan.Checkpoints[i].Tests[j].File)
				}
			}
		}
		for i := range r.Reviews {
			r.Reviews[i].Reviewer = st.Mask(r.Reviews[i].Reviewer)
		}
		for i := range r.Evidence {
			r.Evidence[i].Summary = st.Mask(r.Evidence[i].Summary)
			for j := range r.Evidence[i].Results {
				r.Evidence[i].Results[j].Cmd = st.Mask(r.Evidence[i].Results[j].Cmd)
			}
		}
		if r.DoD != nil {
			for i := range r.DoD.Checks {
				r.DoD.Checks[i].Detail = st.Mask(r.DoD.Checks[i].Detail)
			}
			if r.DoD.Override != nil {
				o := st.Mask(*r.DoD.Override)
				r.DoD.Override = &o
			}
		}
	}
	if d.State != nil {
		d.State.Feature = st.Mask(d.State.Feature)
		for i := range d.State.Checkpoints {
			d.State.Checkpoints[i].Title = st.Mask(d.State.Checkpoints[i].Title)
		}
	}
}

// RenderReview writes the review document; standalone adds the CSS inline and drops forms and script.
func RenderReview(w io.Writer, d ReviewData) error {
	tpl, err := Templates()
	if err != nil {
		return err
	}
	if d.Standalone {
		css, err := assets.ReadFile("static/hub.css")
		if err != nil {
			return err
		}
		d.CSS = template.CSS(css)
		return tpl.ExecuteTemplate(w, "review-standalone", d)
	}
	return tpl.ExecuteTemplate(w, "review-body", d)
}

// ExportReview saves the approved review next to the run log, so the record survives the hub.
func ExportReview(ctx context.Context, svc *runner.Service, card store.Card, run store.Run) (string, error) {
	d, err := BuildReview(ctx, svc, card.ID)
	if err != nil {
		return "", err
	}
	d.Standalone = true
	var buf bytes.Buffer
	if err := RenderReview(&buf, d); err != nil {
		return "", err
	}
	path := filepath.Join(store.RunsDir(svc.Workspace()), run.ID+".review.html")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	return path, os.WriteFile(path, buf.Bytes(), 0o600)
}

func (s *Server) reviewPage(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	d, err := BuildReview(r.Context(), svc, id)
	if errors.Is(err, ErrNoReview) {
		err = errors.Join(store.ErrNotFound, errors.New("No finished run to review yet: the card's run has not reached its result."))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.basePage(r, "Review: "+d.Card.Title, ws)
	p.Nav = "reviews"
	p.Data = d
	s.render(w, r, "review", p)
}

// answerRedirect mirrors formAnswer: a gate that recorded the answer before failing shows both, and
// the flash is a key, never the gate's free text.
func (s *Server) answerRedirect(w http.ResponseWriter, r *http.Request, ws, id, key string, out runner.AnswerOutcome, err error) {
	if err != nil {
		if out.Message != "" {
			err = fmt.Errorf("%w (the gate said: %s)", err, out.Message)
		}
		s.fail(w, r, err)
		return
	}
	if out.AlreadyAnswered {
		key = "already-answered"
	}
	s.redirect(w, r, flashURL(ws, id, key))
}

func (s *Server) reviewApprove(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	out, err := svc.Approve(s.reqCtx(r), id)
	s.answerRedirect(w, r, ws, id, "approved", out, err)
}

func (s *Server) reviewRequestChanges(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	out, err := svc.RequestChanges(s.reqCtx(r), id, r.FormValue("note"))
	s.answerRedirect(w, r, ws, id, "changes-requested", out, err)
}

func (s *Server) reviewDone(w http.ResponseWriter, r *http.Request) {
	svc, ws, ok := s.service(w, r)
	if !ok {
		return
	}
	id, ok := s.pathID(w, r, "id", "c")
	if !ok {
		return
	}
	if _, err := svc.MarkDone(s.reqCtx(r), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.redirect(w, r, flashURL(ws, id, "done"))
}
