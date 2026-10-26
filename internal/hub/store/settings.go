package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
)

// Budget caps one run. Tokens count input + output + cache write (cache reads are not counted).
type Budget struct {
	Tokens  int64   `json:"tokens"`
	USD     float64 `json:"usd"`
	Minutes float64 `json:"minutes"`
	Rounds  int     `json:"rounds"`
}

var DefaultBudget = Budget{Tokens: 2_000_000, USD: 10, Minutes: 120, Rounds: 12}

const DefaultConcurrency = 2

const (
	SettingConcurrency    = "concurrency"
	SettingDefaultBudget  = "default_budget"
	SettingNotify         = "notify"
	SettingWebTokenHash   = "web_token_hash"
	SettingProcessCommand = "process_command" // JSON argv for the process adapter (cards tagged adapter:process)
)

func (b Budget) valid() bool {
	ok := func(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && f > 0 }
	return b.Tokens > 0 && b.Rounds > 0 && ok(b.USD) && ok(b.Minutes)
}

func (b Budget) String() string {
	return fmt.Sprintf("tokens=%d,usd=%s,minutes=%s,rounds=%d", b.Tokens,
		strconv.FormatFloat(b.USD, 'f', -1, 64), strconv.FormatFloat(b.Minutes, 'f', -1, 64), b.Rounds)
}

// ParseBudget reads "tokens=…,usd=…,minutes=…,rounds=…"; keys left out keep base's value.
func ParseBudget(spec string, base Budget) (Budget, error) {
	b := base
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return b, nil
	}
	for _, part := range strings.Split(spec, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			return base, fmt.Errorf("budget %q: want key=value (tokens, usd, minutes, rounds)", part)
		}
		k, v = strings.ToLower(strings.TrimSpace(k)), strings.ReplaceAll(strings.TrimSpace(v), "_", "")
		switch k {
		case "tokens":
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n <= 0 {
				return base, fmt.Errorf("budget tokens=%q: want a positive integer", v)
			}
			b.Tokens = n
		case "usd":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
				return base, fmt.Errorf("budget usd=%q: want a positive number", v)
			}
			b.USD = f
		case "minutes":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 {
				return base, fmt.Errorf("budget minutes=%q: want a positive number", v)
			}
			b.Minutes = f
		case "rounds":
			n, err := strconv.Atoi(v)
			if err != nil || n <= 0 {
				return base, fmt.Errorf("budget rounds=%q: want a positive integer", v)
			}
			b.Rounds = n
		default:
			return base, fmt.Errorf("budget key %q: use tokens, usd, minutes or rounds", k)
		}
	}
	return b, nil
}

func (s *Store) Setting(key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetSetting(key, value string) error {
	_, err := s.DB.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func (s *Store) Concurrency() int {
	v, ok, err := s.Setting(SettingConcurrency)
	if err != nil || !ok {
		return DefaultConcurrency
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n < 1 {
		return DefaultConcurrency
	}
	return n
}

func (s *Store) DefaultBudget() Budget {
	v, ok, err := s.Setting(SettingDefaultBudget)
	if err != nil || !ok {
		return DefaultBudget
	}
	b := DefaultBudget
	if json.Unmarshal([]byte(v), &b) != nil || !b.valid() {
		return DefaultBudget
	}
	return b
}

func (s *Store) Notify() bool {
	v, ok, err := s.Setting(SettingNotify)
	if err != nil || !ok {
		return true
	}
	return strings.TrimSpace(strings.ToLower(v)) != "false"
}

// MaxConcurrency bounds the concurrency a human can set from the page or the API.
const MaxConcurrency = 16

// Validate reports a budget whose limits are not all positive, finite numbers.
func (b Budget) Validate() error {
	if !b.valid() {
		return fmt.Errorf("budget %s: tokens, usd, minutes and rounds must all be positive numbers", b.String())
	}
	return nil
}

func (s *Store) SetConcurrency(n int) error {
	if n < 1 || n > MaxConcurrency {
		return fmt.Errorf("concurrency %d: use 1 to %d", n, MaxConcurrency)
	}
	if err := s.SetSetting(SettingConcurrency, strconv.Itoa(n)); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}

// SetDefaultBudget stores the budget re-serialised from the validated struct, never caller text.
func (s *Store) SetDefaultBudget(b Budget) error {
	if err := b.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return err
	}
	if err := s.SetSetting(SettingDefaultBudget, string(raw)); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}

func (s *Store) SetNotify(on bool) error {
	if err := s.SetSetting(SettingNotify, strconv.FormatBool(on)); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}

// SettingsWrite carries the settings to store; nil fields are left alone.
type SettingsWrite struct {
	Concurrency   *int
	DefaultBudget *Budget
	Notify        *bool
}

// SetSettings stores every given setting and the activity entry describing the change in one
// transaction: an error before the commit changes nothing, so a half-applied update (and a change
// with no audit row) cannot exist. Each value is validated and re-serialised, never caller text.
// After the commit it appends the entry to activity.jsonl (best-effort mirror, as Log does: a failure
// is returned, the committed rows stay) and publishes one "settings" change.
func (s *Store) SetSettings(w SettingsWrite, entry activity.Entry) error {
	kv := map[string]string{}
	if w.Concurrency != nil {
		if *w.Concurrency < 1 || *w.Concurrency > MaxConcurrency {
			return fmt.Errorf("concurrency %d: use 1 to %d", *w.Concurrency, MaxConcurrency)
		}
		kv[SettingConcurrency] = strconv.Itoa(*w.Concurrency)
	}
	if w.DefaultBudget != nil {
		if err := w.DefaultBudget.Validate(); err != nil {
			return err
		}
		raw, err := json.Marshal(*w.DefaultBudget)
		if err != nil {
			return err
		}
		kv[SettingDefaultBudget] = string(raw)
	}
	if w.Notify != nil {
		kv[SettingNotify] = strconv.FormatBool(*w.Notify)
	}
	if len(kv) == 0 {
		return errors.New("no settings to store")
	}
	e, data, err := s.prepEntry(entry)
	if err != nil {
		return err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	for k, v := range kv {
		if _, err := tx.Exec(`INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, k, v); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(insertActivity, e.TS, e.Workspace, e.Actor, e.Verb, nullStr(e.CardID), nullStr(e.RunID), data); err != nil {
		tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return s.mirrorEntry(e)
}
