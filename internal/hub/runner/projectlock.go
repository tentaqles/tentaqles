package runner

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// The per-project pre-relocation lock (spec B §11). Two cards of one project may run at once, but
// a run's rounds before it relocates into its own worktree run in the project folder itself. So a
// run takes the project's lock when its first such round starts and keeps it until it relocates
// or ends; another run of the same project folder stays queued meanwhile. After relocation every
// build has its own worktree and needs no lock.
//
// The lock lives in memory (what this process holds) and in a settings row of the store, keyed by
// the folded project folder, whose value is the holding run's id. RecoverStale drops rows whose
// run ended, relocated or no longer exists.
const settingProjectLock = "runner.project_lock." // + folded project folder; value = run id

// projectKey is the lock key of a run: its project folder, before any relocation.
func (s *Service) projectKey(r store.Run) string {
	r.Worktree = ""
	return foldPath(filepath.ToSlash(filepath.Clean(s.RunDir(r))))
}

// needsProjectLock: the run's next round runs in the project folder (it has not relocated).
func needsProjectLock(r store.Run) bool { return r.Worktree == "" }

// holdsProject reports whether holder is still an active run that has not relocated.
func holdsProject(q interface {
	QueryRow(string, ...any) *sql.Row
}, holder string) (bool, error) {
	var status, wt string
	err := q.QueryRow(`SELECT status, COALESCE(worktree, '') FROM runs WHERE id = ?`, holder).Scan(&status, &wt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return slices.Contains(store.ActiveStatuses, status) && wt == "", nil
}

// takeProjectLock takes (or keeps) r's project lock. When another active pre-relocation run holds
// it, ok is false and holder names that run. A row left by an ended, relocated or missing run is
// taken over.
func (s *Service) takeProjectLock(r store.Run) (ok bool, holder string, err error) {
	key := s.projectKey(r)
	tx, err := s.st.DB.Begin() // _txlock=immediate: the read and the write are one step across processes
	if err != nil {
		return false, "", err
	}
	defer tx.Rollback()
	err = tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingProjectLock+key).Scan(&holder)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, "", err
	}
	if holder != "" && holder != r.ID {
		busy, err := holdsProject(tx, holder)
		if err != nil {
			return false, "", err
		}
		if busy {
			return false, holder, nil
		}
	}
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingProjectLock+key, r.ID); err != nil {
		return false, "", err
	}
	if err := tx.Commit(); err != nil {
		return false, "", err
	}
	s.plMu.Lock()
	s.projLocks[key] = r.ID
	s.plMu.Unlock()
	return true, r.ID, nil
}

// releaseProjectLock frees r's project lock if r holds it (relocation or the end of the run).
func (s *Service) releaseProjectLock(r store.Run) {
	key := s.projectKey(r)
	_, _ = s.st.DB.Exec(`DELETE FROM settings WHERE key = ? AND value = ?`, settingProjectLock+key, r.ID)
	s.plMu.Lock()
	if s.projLocks[key] == r.ID {
		delete(s.projLocks, key)
	}
	delete(s.lockNoted, r.ID)
	s.plMu.Unlock()
}

// ProjectLocks lists the project locks this process holds: project folder key -> run id.
func (s *Service) ProjectLocks() map[string]string {
	s.plMu.Lock()
	defer s.plMu.Unlock()
	out := make(map[string]string, len(s.projLocks))
	for k, v := range s.projLocks {
		out[k] = v
	}
	return out
}

// recoverProjectLocks drops the lock rows of runs that ended, relocated or are gone, and loads the
// rest (active pre-relocation runs, e.g. one waiting on the human) into memory.
func (s *Service) recoverProjectLocks() error {
	rows, err := s.st.DB.Query(`SELECT key, value FROM settings WHERE key LIKE ?`, settingProjectLock+"%")
	if err != nil {
		return err
	}
	type row struct{ key, run string }
	var all []row
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return err
		}
		all = append(all, row{strings.TrimPrefix(k, settingProjectLock), v})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	var errs []error
	for _, r := range all {
		keep, err := holdsProject(s.st.DB, r.run)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !keep {
			if _, err := s.st.DB.Exec(`DELETE FROM settings WHERE key = ? AND value = ?`, settingProjectLock+r.key, r.run); err != nil {
				errs = append(errs, err)
			}
			s.plMu.Lock()
			if s.projLocks[r.key] == r.run {
				delete(s.projLocks, r.key)
			}
			s.plMu.Unlock()
			continue
		}
		s.plMu.Lock()
		s.projLocks[r.key] = r.run
		s.plMu.Unlock()
	}
	return errors.Join(errs...)
}

// noteProjectWait comments once (per run, in this process) that r waits for holder.
func (s *Service) noteProjectWait(r store.Run, holder string) {
	s.plMu.Lock()
	seen := s.lockNoted[r.ID]
	s.lockNoted[r.ID] = true
	s.plMu.Unlock()
	if seen {
		return
	}
	card, err := s.st.Card(r.CardID)
	if err != nil {
		return
	}
	r.Worktree = ""
	s.comment(card, r, fmt.Sprintf("This run waits for run %s, which is working in the project folder %s before moving into its own worktree; it starts when that run has relocated or ended.",
		holder, s.RunDir(r)))
}
