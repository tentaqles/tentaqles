package shell

import (
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// A notice is one line the shell prints on stderr, in that shell's syntax:
// fixed words, the workspace name only when it is a valid workspace name
// (workspace.NameRe), never a path or anything a folder or manifest holds.
func TestEmit_NoticeLinePerShell(t *testing.T) {
	ops := envplan.Ops{Notice: &envplan.Notice{Kind: envplan.NoticeMismatch, Workspace: "acme"}}
	want := map[string]string{
		"bash":       "printf '%s\\n' 'tq: integrity mismatch: run tq hooks verify acme' >&2\n",
		"zsh":        "printf '%s\\n' 'tq: integrity mismatch: run tq hooks verify acme' >&2\n",
		"fish":       "printf '%s\\n' 'tq: integrity mismatch: run tq hooks verify acme' >&2\n",
		"pwsh":       "[Console]::Error.WriteLine('tq: integrity mismatch: run tq hooks verify acme')\n",
		"powershell": "[Console]::Error.WriteLine('tq: integrity mismatch: run tq hooks verify acme')\n",
		"cmd":        "echo tq: integrity mismatch: run tq hooks verify acme\n",
	}
	for sh, w := range want {
		got, err := Emit(sh, ops)
		if err != nil || got != w {
			t.Errorf("%s:\n got %q\nwant %q (%v)", sh, got, w, err)
		}
	}
	got, err := Emit("bash", envplan.Ops{Changed: true, Unset: []string{"TQ_WS"}, Notice: &envplan.Notice{Kind: envplan.NoticeUntrusted, Workspace: "acme"}})
	if err != nil || got != "unset TQ_WS\nprintf '%s\\n' 'tq: untrusted: run tq allow acme' >&2\n" {
		t.Fatalf("the notice comes after the changes: %q %v", got, err)
	}
	if _, err := Emit("bash", envplan.Ops{Notice: &envplan.Notice{Kind: "other"}}); err == nil {
		t.Fatal("an unknown notice is refused")
	}
	if got, err := Emit("bash", envplan.Ops{}); err != nil || got != "" {
		t.Fatalf("nothing to say, nothing emitted: %q", got)
	}
}

// The three notices, word for word.
func TestNoticeText(t *testing.T) {
	for kind, want := range map[string]string{
		envplan.NoticeUntrusted: "tq: untrusted: run tq allow acme",
		envplan.NoticeMismatch:  "tq: integrity mismatch: run tq hooks verify acme",
		envplan.NoticeLegacy:    "tq: legacy trust record: run tq allow acme to sign hooks and manifest",
	} {
		if got, ok := NoticeText(envplan.Notice{Kind: kind, Workspace: "acme"}); !ok || got != want {
			t.Errorf("%s: %q", kind, got)
		}
	}
}

// A workspace name that is not a valid one (a folder named with quotes,
// '$', backticks, a path separator, a line break, upper case) never
// reaches the line: the notice prints without it, and every shell's
// line holds only the fixed words.
func TestNoticeNeverCarriesAnUnvalidatedName(t *testing.T) {
	for _, bad := range []string{"it's", "a$(rm -rf ~)", "`x`", `a"b`, "a\nb", "a;b", "C:\\x", "../x", "Acme", "a b", "a%PATH%", "a&calc"} {
		for _, kind := range []string{envplan.NoticeUntrusted, envplan.NoticeMismatch, envplan.NoticeLegacy} {
			text, ok := NoticeText(envplan.Notice{Kind: kind, Workspace: bad})
			if !ok || strings.Contains(text, bad) || !strings.Contains(text, "run tq doctor") {
				t.Fatalf("%s %q: %q", kind, bad, text)
			}
			for _, sh := range Shells {
				line, err := Emit(sh, envplan.Ops{Notice: &envplan.Notice{Kind: kind, Workspace: bad}})
				if err != nil || strings.Contains(line, bad) || strings.Count(line, "\n") != 1 {
					t.Fatalf("%s %s %q: %q %v", sh, kind, bad, line, err)
				}
			}
		}
	}
}
