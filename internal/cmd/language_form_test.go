package cmd

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// drive feeds keys to the language question and returns what it shows and the tag it holds.
func drive(t *testing.T, keys ...tea.KeyMsg) (view, tag string) {
	t.Helper()
	v := "en-US"
	var m tea.Model = huh.NewForm(huh.NewGroup(languageSelect(&v)))
	m.Init()
	for _, k := range keys {
		m, _ = m.Update(k)
	}
	return m.View(), v
}

func typed(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var (
	down  = tea.KeyMsg{Type: tea.KeyDown}
	enter = tea.KeyMsg{Type: tea.KeyEnter}
)

// The list starts on English (United States); typing "por" leaves the two Portuguese entries,
// and the arrows move between them.
func TestLanguageSelect_FiltersAsYouType(t *testing.T) {
	view, tag := drive(t)
	if tag != "en-US" || !strings.Contains(view, "English (United States)") {
		t.Fatalf("start: %q\n%s", tag, view)
	}
	view, _ = drive(t, typed("p"), typed("o"), typed("r"))
	if !strings.Contains(view, "Portuguese (Brazil)") || !strings.Contains(view, "Portuguese (Portugal)") ||
		strings.Contains(view, "German") || strings.Contains(view, "Arabic") {
		t.Fatalf("after por:\n%s", view)
	}
	if _, tag = drive(t, typed("p"), typed("o"), typed("r"), enter); tag != "pt-BR" {
		t.Fatalf("enter after por: %q", tag)
	}
	if _, tag = drive(t, typed("p"), typed("o"), typed("r"), down, enter); tag != "pt-PT" {
		t.Fatalf("down from pt-BR: %q", tag)
	}
	// Without typing, down walks the list from the default: the next one by name.
	if _, tag = drive(t, down, enter); tag != "ar" {
		t.Fatalf("down from en-US: %q", tag)
	}
}
