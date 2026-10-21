package store

import (
	"sort"
	"strings"
)

// FrontMatter reads "key: value" lines between a leading "---" line and the next "---" line.
func FrontMatter(body string) map[string]string {
	out := map[string]string{}
	b := strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(b, "---\n") {
		return out
	}
	end := strings.Index(b[4:], "\n---")
	if end < 0 {
		return out
	}
	for _, line := range strings.Split(b[4:4+end], "\n") {
		k, v, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(k) != "" {
			out[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
		}
	}
	return out
}

func StripFrontMatter(body string) string {
	b := strings.ReplaceAll(body, "\r\n", "\n")
	if !strings.HasPrefix(b, "---\n") {
		return body
	}
	end := strings.Index(b[4:], "\n---")
	if end < 0 {
		return body
	}
	rest := b[4+end+4:]
	return strings.TrimPrefix(rest, "\n")
}

func WithBudgetFrontMatter(body string, b Budget) string {
	fm := FrontMatter(body)
	fm["budget"] = b.String()
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("---\n")
	for _, k := range keys {
		sb.WriteString(k + ": " + fm[k] + "\n")
	}
	sb.WriteString("---\n")
	sb.WriteString(StripFrontMatter(body))
	return sb.String()
}

// CardBudget is the card's front-matter budget on top of def, or def.
func CardBudget(c Card, def Budget) (Budget, error) {
	if v, ok := FrontMatter(c.Body)["budget"]; ok {
		return ParseBudget(v, def)
	}
	return def, nil
}
