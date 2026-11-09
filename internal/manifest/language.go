package manifest

import (
	"fmt"
	"sort"
	"strings"
)

// Language is a language a client works in: its BCP 47 tag, which the manifest stores, its name in
// English, which Claude Code is given, and its own name, so the list can be filtered either way.
type Language struct{ Tag, Name, Native string }

// Label is how the form lists a language: "Portuguese (Brazil) · português (Brasil)".
func (l Language) Label() string {
	if l.Native == "" || l.Native == l.Name {
		return l.Name
	}
	return l.Name + " · " + l.Native
}

// DefaultLanguage is the language of a client whose manifest names none, and the first in the list.
const DefaultLanguage = "en-US"

var languages = []Language{
	{"ar", "Arabic", "العربية"},
	{"bg", "Bulgarian", "български"},
	{"bn", "Bengali", "বাংলা"},
	{"ca", "Catalan", "català"},
	{"cs", "Czech", "čeština"},
	{"da", "Danish", "dansk"},
	{"de-DE", "German", "Deutsch"},
	{"el", "Greek", "Ελληνικά"},
	{"en-AU", "English (Australia)", ""},
	{"en-CA", "English (Canada)", ""},
	{"en-GB", "English (United Kingdom)", ""},
	{"en-IN", "English (India)", ""},
	{"en-US", "English (United States)", ""},
	{"es-419", "Spanish (Latin America)", "español (Latinoamérica)"},
	{"es-ES", "Spanish (Spain)", "español (España)"},
	{"es-MX", "Spanish (Mexico)", "español (México)"},
	{"et", "Estonian", "eesti"},
	{"fa", "Persian", "فارسی"},
	{"fi", "Finnish", "suomi"},
	{"fil", "Filipino", "Filipino"},
	{"fr-CA", "French (Canada)", "français (Canada)"},
	{"fr-FR", "French (France)", "français (France)"},
	{"he", "Hebrew", "עברית"},
	{"hi", "Hindi", "हिन्दी"},
	{"hr", "Croatian", "hrvatski"},
	{"hu", "Hungarian", "magyar"},
	{"id", "Indonesian", "Bahasa Indonesia"},
	{"it", "Italian", "italiano"},
	{"ja", "Japanese", "日本語"},
	{"ko", "Korean", "한국어"},
	{"lt", "Lithuanian", "lietuvių"},
	{"lv", "Latvian", "latviešu"},
	{"ms", "Malay", "Bahasa Melayu"},
	{"nb", "Norwegian (Bokmål)", "norsk bokmål"},
	{"nl", "Dutch", "Nederlands"},
	{"pl", "Polish", "polski"},
	{"pt-BR", "Portuguese (Brazil)", "português (Brasil)"},
	{"pt-PT", "Portuguese (Portugal)", "português (Portugal)"},
	{"ro", "Romanian", "română"},
	{"ru", "Russian", "русский"},
	{"sk", "Slovak", "slovenčina"},
	{"sl", "Slovenian", "slovenščina"},
	{"sr", "Serbian", "српски"},
	{"sv", "Swedish", "svenska"},
	{"sw", "Swahili", "Kiswahili"},
	{"th", "Thai", "ไทย"},
	{"tr", "Turkish", "Türkçe"},
	{"uk", "Ukrainian", "українська"},
	{"ur", "Urdu", "اردو"},
	{"vi", "Vietnamese", "Tiếng Việt"},
	{"zh-CN", "Chinese (Simplified)", "简体中文"},
	{"zh-TW", "Chinese (Traditional)", "繁體中文"},
}

// Languages is the list the form offers: the default first, then the rest by English name.
func Languages() []Language {
	out := make([]Language, 0, len(languages))
	var def Language
	for _, l := range languages {
		if l.Tag == DefaultLanguage {
			def = l
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return append([]Language{def}, out...)
}

// LanguageOf is the language with tag (case does not matter).
func LanguageOf(tag string) (Language, bool) {
	for _, l := range languages {
		if strings.EqualFold(l.Tag, strings.TrimSpace(tag)) {
			return l, true
		}
	}
	return Language{}, false
}

// FindLanguage is the language q names: its tag or its name, in English or its own, ignoring case;
// else the one language whose label contains q. Several or none is an error naming them.
func FindLanguage(q string) (Language, error) {
	q = strings.TrimSpace(q)
	if l, ok := LanguageOf(q); ok {
		return l, nil
	}
	var hits []Language
	for _, l := range Languages() {
		if strings.EqualFold(l.Name, q) || strings.EqualFold(l.Native, q) {
			return l, nil
		}
		if q != "" && strings.Contains(strings.ToLower(l.Label()), strings.ToLower(q)) {
			hits = append(hits, l)
		}
	}
	if len(hits) == 1 {
		return hits[0], nil
	}
	if len(hits) == 0 {
		return Language{}, fmt.Errorf("no language matches %q: name it, or give its tag (pt-BR, en-US, ...)", q)
	}
	var names []string
	for _, l := range hits {
		names = append(names, l.Name+" ("+l.Tag+")")
	}
	return Language{}, fmt.Errorf("%q matches %s: name one", q, strings.Join(names, ", "))
}

// ClientLanguage is the client's language: the manifest's, or the default when it names none.
func (m *Manifest) ClientLanguage() Language {
	if l, ok := LanguageOf(m.Language); ok {
		return l
	}
	l, _ := LanguageOf(DefaultLanguage)
	return l
}

// LanguageLine is the instruction that tells the agent the client's language: the line the Claude
// session-start preamble prints, and the Codex developer_instructions tentaqles writes.
func (m *Manifest) LanguageLine() string {
	l := m.ClientLanguage()
	return fmt.Sprintf("Language: %s (%s). Write every report, document, email and message for this client's people (clients and teammates) in %s.", l.Name, l.Tag, l.Name)
}

func (m *Manifest) validateLanguage() error {
	if m.Language == "" {
		return nil
	}
	if _, ok := LanguageOf(m.Language); !ok {
		return fmt.Errorf("language: unknown tag %q (for example en-US, pt-BR, es-ES)", m.Language)
	}
	return nil
}
