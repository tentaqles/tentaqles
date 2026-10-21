package gate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/tentaqles/tentaqles/internal/paths"
)

// KeyPath is the gate's signing key; the hub reads it only inside the answer path.
func KeyPath() string { return filepath.Join(paths.Home(), "build-gate", "hmac.key") }

// Actor is the hub actor the gate records: "hub:<os user name>" (Windows DOMAIN\user → user).
func Actor() string {
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	if name == "" {
		name = os.Getenv("USERNAME")
	}
	if name == "" {
		name = os.Getenv("USER")
	}
	if name == "" {
		name = "user"
	}
	return "hub:" + name
}

// Message is json.dumps(["hub", action, slug, actor], ensure_ascii=True, separators=(",", ":")),
// byte for byte (gate._hub_mac).
func Message(action, slug, actor string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range []string{"hub", action, slug, actor} {
		if i > 0 {
			b.WriteByte(',')
		}
		writePyString(&b, s)
	}
	b.WriteByte(']')
	return b.String()
}

// writePyString writes s the way Python's json module does with ensure_ascii=True: printable ASCII
// as is, the short escapes, and everything else as \uXXXX (lowercase hex, surrogate pairs above
// U+FFFF). Invalid UTF-8 cannot come from Python, so it is written as U+FFFD.
func writePyString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			switch {
			case r >= 0x20 && r <= 0x7e:
				b.WriteRune(r)
			case r > 0xffff:
				r1, r2 := utf16.EncodeRune(r)
				fmt.Fprintf(b, `\u%04x\u%04x`, r1, r2)
			default:
				fmt.Fprintf(b, `\u%04x`, r)
			}
		}
	}
	b.WriteByte('"')
}

// MintToken is gate.mint_hub_token with an explicit key (the raw bytes of hmac.key).
func MintToken(key []byte, action, slug, actor string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(Message(action, slug, actor)))
	return hex.EncodeToString(m.Sum(nil))
}
