package store

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Sync settings (spec B §5).
const (
	SettingSyncEnabled  = "sync.enabled"
	SettingSyncInterval = "sync.interval" // seconds
	// SettingSyncPolicyPrefix + an outbox action is that action's write policy key,
	// e.g. "sync.policy.move_on_start".
	SettingSyncPolicyPrefix = "sync.policy."

	DefaultSyncInterval = 300
	MinSyncInterval     = 60
)

// Write-policy values: off drops the write, ask creates a hub permission decision, auto sends it.
const (
	PolicyOff  = "off"
	PolicyAsk  = "ask"
	PolicyAuto = "auto"
)

var PolicyValues = []string{PolicyOff, PolicyAsk, PolicyAuto}

// SyncPolicy is the write policy of each outbound action.
type SyncPolicy struct {
	MoveOnStart    string `json:"move_on_start"`
	MoveOnReview   string `json:"move_on_review"`
	CommentResults string `json:"comment_results"`
	CreateCard     string `json:"create_card"`
	EditText       string `json:"edit_text"`
	CloseItem      string `json:"close_item"`
}

// DefaultSyncPolicy: lane moves and result comments are automatic; creating a company card,
// editing its text and closing it ask first.
func DefaultSyncPolicy() SyncPolicy {
	return SyncPolicy{MoveOnStart: PolicyAuto, MoveOnReview: PolicyAuto, CommentResults: PolicyAuto,
		CreateCard: PolicyAsk, EditText: PolicyAsk, CloseItem: PolicyAsk}
}

func (p *SyncPolicy) field(action string) *string {
	switch action {
	case OutboxMoveOnStart:
		return &p.MoveOnStart
	case OutboxMoveOnReview:
		return &p.MoveOnReview
	case OutboxCommentResults:
		return &p.CommentResults
	case OutboxCreateCard:
		return &p.CreateCard
	case OutboxEditText:
		return &p.EditText
	case OutboxCloseItem:
		return &p.CloseItem
	}
	return nil
}

// Get is the policy of action ("" for an unknown action).
func (p SyncPolicy) Get(action string) string {
	if f := p.field(action); f != nil {
		return *f
	}
	return ""
}

// Set validates and sets one action's policy.
func (p *SyncPolicy) Set(action, value string) error {
	if err := ValidatePolicy(action, value); err != nil {
		return err
	}
	*p.field(action) = value
	return nil
}

// ValidatePolicy checks an action name and a policy value.
func ValidatePolicy(action, value string) error {
	if !slices.Contains(OutboxActions, action) {
		return fmt.Errorf("sync policy %q: use one of %s", action, strings.Join(OutboxActions, ", "))
	}
	if !slices.Contains(PolicyValues, value) {
		return fmt.Errorf("sync policy %s=%q: use off, ask or auto", action, value)
	}
	return nil
}

// ValidateSyncInterval checks a poll interval in seconds.
func ValidateSyncInterval(n int) error {
	if n < MinSyncInterval {
		return fmt.Errorf("sync interval %d: use %d seconds or more", n, MinSyncInterval)
	}
	return nil
}

// SyncSettings are the workspace's sync settings as the page and the API show them.
type SyncSettings struct {
	Enabled  bool       `json:"enabled"`
	Interval int        `json:"interval"` // seconds
	Policy   SyncPolicy `json:"policy"`
}

// SyncSettings reads every sync setting. hasTracker is whether the manifest has a tracker block:
// sync.enabled defaults to it.
func (s *Store) SyncSettings(hasTracker bool) SyncSettings {
	p := DefaultSyncPolicy()
	for _, a := range OutboxActions {
		*p.field(a) = s.SyncPolicyFor(a)
	}
	return SyncSettings{Enabled: s.SyncEnabled(hasTracker), Interval: s.SyncInterval(), Policy: p}
}

// SyncEnabled is the stored sync.enabled, else def (true when a tracker block exists).
func (s *Store) SyncEnabled(def bool) bool {
	v, ok, err := s.Setting(SettingSyncEnabled)
	if err != nil || !ok {
		return def
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return def
	}
	return b
}

// SyncInterval is the poll interval in seconds; a missing or invalid value is the default.
func (s *Store) SyncInterval() int {
	v, ok, err := s.Setting(SettingSyncInterval)
	if err != nil || !ok {
		return DefaultSyncInterval
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || ValidateSyncInterval(n) != nil {
		return DefaultSyncInterval
	}
	return n
}

// SyncPolicyFor is one action's policy; a missing or invalid value is the default.
func (s *Store) SyncPolicyFor(action string) string {
	def := DefaultSyncPolicy().Get(action)
	v, ok, err := s.Setting(SettingSyncPolicyPrefix + action)
	if err != nil || !ok {
		return def
	}
	v = strings.TrimSpace(strings.ToLower(v))
	if !slices.Contains(PolicyValues, v) {
		return def
	}
	return v
}

func (s *Store) SetSyncEnabled(on bool) error {
	if err := s.SetSetting(SettingSyncEnabled, strconv.FormatBool(on)); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}

func (s *Store) SetSyncInterval(n int) error {
	if err := ValidateSyncInterval(n); err != nil {
		return err
	}
	if err := s.SetSetting(SettingSyncInterval, strconv.Itoa(n)); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}

func (s *Store) SetSyncPolicy(action, value string) error {
	if err := ValidatePolicy(action, value); err != nil {
		return err
	}
	if err := s.SetSetting(SettingSyncPolicyPrefix+action, value); err != nil {
		return err
	}
	s.Publish(Change{Kind: "settings"})
	return nil
}
