package codexcfg

import (
	"strings"
	"testing"
)

func TestPinnedFlags(t *testing.T) {
	if got := strings.Join(Pinned.Flags(), " "); got != "-a on-request -s workspace-write" {
		t.Fatalf("flags: %q", got)
	}
	if got := (Plan{}).Flags(); got != nil {
		t.Fatalf("zero plan: %v", got)
	}
}

func TestLaunchArgs(t *testing.T) {
	p := Pinned
	for _, tc := range []struct {
		p    Plan
		args []string
		want string
	}{
		{p, []string{"hi"}, "-a on-request -s workspace-write hi"},
		{p, []string{"exec", "hi"}, "-a on-request -s workspace-write exec hi"},
		{p, []string{"-a", "never", "hi"}, "-s workspace-write -a never hi"},
		{p, []string{"--ask-for-approval=never"}, "-s workspace-write --ask-for-approval=never"},
		{p, []string{"-s", "read-only"}, "-a on-request -s read-only"},
		{p, []string{"--sandbox=read-only"}, "-a on-request --sandbox=read-only"},
		{p, []string{"-c", "sandbox_mode=\"read-only\""}, "-a on-request -c sandbox_mode=\"read-only\""},
		{p, []string{"--config=approval_policy=\"never\""}, "-s workspace-write --config=approval_policy=\"never\""},
		{p, []string{"--yolo"}, "--yolo"},
		{p, []string{BypassFlag, "x"}, BypassFlag + " x"},
		{p, []string{"--", "-a"}, "-a on-request -s workspace-write -- -a"},
		{Plan{}, []string{"hi"}, "hi"},
	} {
		if got := strings.Join(LaunchArgs(tc.p, tc.args), " "); got != tc.want {
			t.Errorf("%+v %v: got %q want %q", tc.p, tc.args, got, tc.want)
		}
	}
}
