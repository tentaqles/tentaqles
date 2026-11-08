package shell

import (
	"testing"

	"github.com/tentaqles/tentaqles/internal/envplan"
)

// The notices, word for word; a name that is no client name never reaches the line.
func TestNoticeText(t *testing.T) {
	for kind, want := range map[string]string{
		envplan.NoticeMask:      "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tentaqles settings render acme",
		envplan.NoticeCmdLong:   "tq: PATH over cmd.exe's 8191-character line: out-of-scope CLIs still resolve here, shorten PATH or use PowerShell",
		envplan.NoticeStateLost: "tq: shell state lost: tq unset its own variables and cannot restore the values from before it; open a new terminal to get them back",
	} {
		if got, ok := NoticeText(envplan.Notice{Kind: kind, Workspace: "acme"}); !ok || got != want {
			t.Errorf("%s: %q", kind, got)
		}
	}
	if got, _ := NoticeText(envplan.Notice{Kind: envplan.NoticeMask, Workspace: "a; rm -rf ~"}); got != "tq: PATH mask missing: out-of-scope CLIs still resolve here, run tentaqles doctor" {
		t.Errorf("a bad name: %q", got)
	}
	if _, ok := NoticeText(envplan.Notice{Kind: "untrusted"}); ok {
		t.Error("a removed kind still has a text")
	}
}
