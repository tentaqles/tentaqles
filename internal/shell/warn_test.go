package shell

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// Ops.Warn is one line the shell prints on stderr, in that shell's syntax,
// quoted so the message is data; it is emitted with or without changes,
// after them, and a line break in it is refused.
func TestEmit_WarnLinePerShell(t *testing.T) {
	ops := envplan.Ops{Warn: "tq: acme untrusted: it's changed & more"}
	want := map[string]string{
		"bash":       "printf '%s\\n' 'tq: acme untrusted: it'\\''s changed & more' >&2\n",
		"zsh":        "printf '%s\\n' 'tq: acme untrusted: it'\\''s changed & more' >&2\n",
		"fish":       "printf '%s\\n' 'tq: acme untrusted: it\\'s changed & more' >&2\n",
		"pwsh":       "[Console]::Error.WriteLine('tq: acme untrusted: it''s changed & more')\n",
		"powershell": "[Console]::Error.WriteLine('tq: acme untrusted: it''s changed & more')\n",
		"cmd":        "echo tq: acme untrusted: it's changed ^& more 1>&2\n",
	}
	for sh, w := range want {
		got, err := Emit(sh, ops)
		if err != nil || got != w {
			t.Errorf("%s:\n got %q\nwant %q (%v)", sh, got, w, err)
		}
	}
	got, err := Emit("bash", envplan.Ops{Changed: true, Unset: []string{"TQ_WS"}, Warn: "w"})
	if err != nil || got != "unset TQ_WS\nprintf '%s\\n' 'w' >&2\n" {
		t.Fatalf("the warning comes after the changes: %q %v", got, err)
	}
	if _, err := Emit("bash", envplan.Ops{Warn: "a\nb"}); err == nil {
		t.Fatal("a line break in the warning is refused")
	}
	if got, err := Emit("bash", envplan.Ops{}); err != nil || got != "" {
		t.Fatalf("nothing to say, nothing emitted: %q", got)
	}
}
