package scrub

import (
	"strings"
	"unicode"
)

// keyMarker replaces every scalar under a key that names a secret.
const keyMarker = "[redacted:key]"

// secretWords are whole words that name a secret and are not in the named rule's list.
var secretWords = map[string]bool{"auth": true, "authorization": true}

// secretKey reports whether a JSON key names a secret: the named rule's own name test (secretNameRe)
// with its count-name exception, or one of the words in secretWords.
func secretKey(key string) bool {
	if secretNameRe.MatchString(key) && !countName(key) {
		return true
	}
	for _, w := range keyWords(key) {
		if secretWords[w] {
			return true
		}
	}
	return false
}

// redactScalars replaces every scalar in v with keyMarker. Null stays null; objects and arrays
// keep their keys and their shape, at any depth.
func redactScalars(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = redactScalars(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = redactScalars(e)
		}
		return out
	default:
		return keyMarker
	}
}

// keyWords splits a key into lower-case words at _, ., -, digits and lower-to-upper case changes.
func keyWords(key string) []string {
	var words []string
	var cur []rune
	var prev rune
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for _, r := range key {
		switch {
		case r == '_' || r == '.' || r == '-' || (r >= '0' && r <= '9'):
			flush()
		case unicode.IsUpper(r) && unicode.IsLower(prev):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
		prev = r
	}
	flush()
	return words
}
