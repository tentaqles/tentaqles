package store

import (
	"crypto/sha256"
	"encoding/hex"
)

// SessionFile returns the store-relative path of a session's file, sessions/<name>.json. The name is
// id when id is 1 to 128 characters of letters, digits, '-' and '_'; otherwise it is "s_" and the first
// 16 hex characters of the SHA-256 of id, so an id never names a path.
func SessionFile(id string) string {
	name := id
	if !validSessionID(id) {
		sum := sha256.Sum256([]byte(id))
		name = "s_" + hex.EncodeToString(sum[:])[:16]
	}
	return "sessions/" + name + ".json"
}

func validSessionID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
		if !ok {
			return false
		}
	}
	return true
}
