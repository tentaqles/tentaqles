package store

import (
	"crypto/rand"
	"regexp"
	"strings"
)

const idAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

var idRe = regexp.MustCompile(`^[a-z]-[a-z2-7]{12}$`)

// NewID returns prefix + "-" + 12 random base32 characters (60 bits).
func NewID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails on supported platforms (Go 1.24+)
	out := make([]byte, 0, len(prefix)+13)
	out = append(out, prefix...)
	out = append(out, '-')
	for _, x := range b {
		out = append(out, idAlphabet[x&31])
	}
	return string(out)
}

func ValidID(prefix, id string) bool {
	return strings.HasPrefix(id, prefix+"-") && idRe.MatchString(id)
}
