package manifest

import (
	"sort"
	"strings"
	"testing"
)

// The form's list starts on English (United States); the rest follow by English name.
func TestLanguages_DefaultFirstThenByName(t *testing.T) {
	ls := Languages()
	if ls[0].Tag != DefaultLanguage || ls[0].Name != "English (United States)" {
		t.Fatalf("first = %+v", ls[0])
	}
	rest := ls[1:]
	if !sort.SliceIsSorted(rest, func(i, j int) bool { return rest[i].Name < rest[j].Name }) {
		t.Fatal("the rest is not by name")
	}
	seen := map[string]bool{}
	for _, l := range ls {
		if seen[l.Tag] || l.Tag == "" || l.Name == "" {
			t.Fatalf("bad or repeated entry %+v", l)
		}
		seen[l.Tag] = true
	}
	for _, tag := range []string{"pt-BR", "en-GB", "es-ES", "de-DE", "fr-FR", "ja", "zh-CN"} {
		if !seen[tag] {
			t.Errorf("%s is missing", tag)
		}
	}
}

// Typing "por" in the form leaves the Portuguese entries: the filter matches the label.
func TestLanguages_LabelFiltersByEitherName(t *testing.T) {
	var hits []string
	for _, l := range Languages() {
		if strings.Contains(strings.ToLower(l.Label()), "por") {
			hits = append(hits, l.Tag)
		}
	}
	if strings.Join(hits, ",") != "pt-BR,pt-PT" {
		t.Fatalf("por matches %v", hits)
	}
	if l, _ := LanguageOf("pt-BR"); l.Label() != "Portuguese (Brazil) · português (Brasil)" {
		t.Fatalf("label = %q", l.Label())
	}
}

func TestFindLanguage(t *testing.T) {
	for q, want := range map[string]string{
		"pt-BR": "pt-BR", "PT-br": "pt-BR", "Portuguese (Brazil)": "pt-BR", "português (Brasil)": "pt-BR",
		"brasil": "pt-BR", "japanese": "ja", "en-us": "en-US", "united kingdom": "en-GB",
	} {
		if l, err := FindLanguage(q); err != nil || l.Tag != want {
			t.Errorf("%q = %+v %v, want %s", q, l, err, want)
		}
	}
	if _, err := FindLanguage("portuguese"); err == nil || !strings.Contains(err.Error(), "pt-PT") {
		t.Errorf("portuguese is two languages: %v", err)
	}
	if _, err := FindLanguage("klingon"); err == nil {
		t.Error("klingon was found")
	}
}

func TestLoad_Language(t *testing.T) {
	m, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nlanguage: pt-BR\n"))
	if err != nil || m.ClientLanguage().Name != "Portuguese (Brazil)" {
		t.Fatalf("%+v %v", m, err)
	}
	m, err = Load(write(t, "schema: tentaqles-client-v2\nclient: x\n"))
	if err != nil || m.ClientLanguage().Tag != DefaultLanguage {
		t.Fatalf("no language: %+v %v", m, err)
	}
	if _, err := Load(write(t, "schema: tentaqles-client-v2\nclient: x\nlanguage: xx-YY\n")); err == nil {
		t.Fatal("an unknown language loaded")
	}
}
