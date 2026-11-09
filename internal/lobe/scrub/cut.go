package scrub

import "unicode/utf8"

// runeLen counts the characters of s as Unicode code points.
func runeLen(s string) int { return utf8.RuneCountInString(s) }

// firstRunes keeps at most the first n characters of s.
func firstRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for pos := range s {
		if i == n {
			return s[:pos]
		}
		i++
	}
	return s
}

// lastRunes keeps at most the last n characters of s.
func lastRunes(s string, n int) string {
	total := runeLen(s)
	if total <= n {
		return s
	}
	skip := total - n
	for pos := range s {
		if skip == 0 {
			return s[pos:]
		}
		skip--
	}
	return ""
}

// dropLastRunes removes the last n characters of s (all of them when n is larger).
func dropLastRunes(s string, n int) string {
	return firstRunes(s, runeLen(s)-n)
}

// dropFirstRunes removes the first n characters of s (all of them when n is larger).
func dropFirstRunes(s string, n int) string {
	return lastRunes(s, runeLen(s)-n)
}
