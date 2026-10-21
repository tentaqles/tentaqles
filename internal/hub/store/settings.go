package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
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
