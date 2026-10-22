package runner

import (
	"context"
	"errors"

	"github.com/tentaqles/tentaqles/internal/hub/activity"
	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// UpdateCard edits a card's title, body, project folder or tags. An agent session may change the
// tags only, and only on a card in a planning lane (backlog, todo, other): the lanes it may also add
// to and move between. Everything else about a card is the human's (spec §15). Adding agent-ready
// here starts nothing; a run starts when a card enters an active lane.
func (s *Service) UpdateCard(ctx context.Context, id string, u store.CardUpdate) (store.Card, error) {
	if u.Title == nil && u.Body == nil && u.ProjectDir == nil && u.Tags == nil {
		return store.Card{}, errors.New("nothing to update: send a title, body, project folder or tags")
	}
	card, err := s.st.Card(id)
	if err != nil {
		return store.Card{}, err
	}
	actor := s.human()
	if IsAgent(ctx) {
		if u.Title != nil || u.Body != nil || u.ProjectDir != nil {
			return store.Card{}, ErrHumanOnly
		}
		lane, err := s.st.Lane(card.Lane)
		if err != nil {
			return store.Card{}, err
		}
		if !agentLane(lane.Kind) {
			return store.Card{}, ErrHumanOnly
		}
		actor = "agent"
	}
	return s.st.UpdateCard(id, u, actor)
}

// Settings are the workspace's run settings as the page shows them.
type Settings struct {
	Concurrency   int          `json:"concurrency"`
	DefaultBudget store.Budget `json:"default_budget"`
	Notify        bool         `json:"notify"`
}

// SettingsUpdate carries the settings to change; nil fields stay as they are. A budget is
// replaced whole and must be valid.
type SettingsUpdate struct {
	Concurrency   *int          `json:"concurrency,omitempty"`
	DefaultBudget *store.Budget `json:"default_budget,omitempty"`
	Notify        *bool         `json:"notify,omitempty"`
}

func (s *Service) Settings() Settings {
	return Settings{Concurrency: s.st.Concurrency(), DefaultBudget: s.st.DefaultBudget(), Notify: s.st.Notify()}
}

// UpdateSettings is human-only. Every field is validated before any is written, so a refused update
// changes nothing.
func (s *Service) UpdateSettings(ctx context.Context, u SettingsUpdate) (Settings, error) {
	if IsAgent(ctx) {
		return Settings{}, ErrHumanOnly
	}
	if u.Concurrency == nil && u.DefaultBudget == nil && u.Notify == nil {
		return Settings{}, errors.New("nothing to update: send concurrency, default_budget or notify")
	}
	if u.Concurrency != nil && (*u.Concurrency < 1 || *u.Concurrency > store.MaxConcurrency) {
		return Settings{}, errors.New("concurrency: use a whole number from 1 to 16")
	}
	if u.DefaultBudget != nil {
		if err := u.DefaultBudget.Validate(); err != nil {
			return Settings{}, err
		}
	}
	var fields []any
	if u.Concurrency != nil {
		if err := s.st.SetConcurrency(*u.Concurrency); err != nil {
			return Settings{}, err
		}
		fields = append(fields, "concurrency")
	}
	if u.DefaultBudget != nil {
		if err := s.st.SetDefaultBudget(*u.DefaultBudget); err != nil {
			return Settings{}, err
		}
		fields = append(fields, "default_budget")
	}
	if u.Notify != nil {
		if err := s.st.SetNotify(*u.Notify); err != nil {
			return Settings{}, err
		}
		fields = append(fields, "notify")
	}
	_ = s.st.Log(activity.Entry{Actor: s.human(), Verb: activity.VerbSettingsUpdate, Data: map[string]any{"fields": fields}})
	return s.Settings(), nil
}
