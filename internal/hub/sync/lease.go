package sync

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// The lease keeps two processes (a serving hub and a tq hub sync now) from sending the same outbox
// row twice. It is a row in the store's settings table, "<holder>|<expiry>", taken in one
// immediate transaction; it lapses on its own if a process dies holding it.
const settingLease = "sync.lease"

func (e *Engine) acquire() (bool, error) {
	tx, err := e.st.DB.Begin() // _txlock=immediate: the read and the write are one step
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var v string
	err = tx.QueryRow(`SELECT value FROM settings WHERE key = ?`, settingLease).Scan(&v)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	now := e.cfg.Now().UTC()
	if holder, exp, ok := strings.Cut(v, "|"); ok && holder != e.holder {
		if t, perr := time.Parse(time.RFC3339Nano, exp); perr == nil && t.After(now) {
			return false, nil
		}
	}
	val := e.holder + "|" + now.Add(leaseTTL).Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		settingLease, val); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (e *Engine) release() {
	_, _ = e.st.DB.Exec(`DELETE FROM settings WHERE key = ? AND value LIKE ?`, settingLease, e.holder+"|%")
}
