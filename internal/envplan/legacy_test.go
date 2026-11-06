package envplan

import (
	"encoding/base64"
	"encoding/json"
)

func legacyB64(s State) string {
	b, _ := json.Marshal(s)
	return base64.RawURLEncoding.EncodeToString(b)
}
