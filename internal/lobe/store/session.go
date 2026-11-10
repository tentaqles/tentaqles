package store

import (
	"crypto/sha256"
	"encoding/hex"
)

// SessionFile returns the store-relative path of a session's file, sessions/<name>.json. The name is
// id when id is 1 to 128 characters of letters, digits, '-' and '_'; otherwise it is "s_" and the first
// 16 hex characters of the SHA-256 of id, so an id never names a path.
func SessionFile(id string) string {
	return "sessions/" + sessionName(id) + ".json"
}

// CursorFile returns the store-relative path of a session's read cursor, cursors/<name>.json. The name
// is the one SessionFile gives the same id.
func CursorFile(id string) string {
	return "cursors/" + sessionName(id) + ".json"
}

// sessionName is the file name of a session id: the id itself when it is a safe name, otherwise a
// hashed one.
func sessionName(id string) string {
	if validSessionID(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "s_" + hex.EncodeToString(sum[:])[:16]
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
