package scan

import (
	"reflect"
	"testing"
)

func TestScriptFiles(t *testing.T) {
	for _, c := range []struct {
		cmd  string
		want []string
	}{
		{"bash build.sh", []string{"build.sh"}},
		{"sh -e -x scripts/run", []string{"scripts/run"}},
		{"zsh --login ci/x.zsh arg", []string{"ci/x.zsh"}},
		{"./build.sh --fast", []string{"./build.sh"}},
		{`C:\repo\tools\run.sh`, []string{`C:\repo\tools\run.sh`}},
		{"cd infra && sudo -E env A=1 ./deploy.bash; source env.sh", []string{"./deploy.bash", "env.sh"}},
		{". ./lib.sh", []string{"./lib.sh"}},
		{"timeout 600 ./long.sh", []string{"./long.sh"}},
		{"bash -c 'dbt run'", nil},
		{"bash -lc ./x.sh", nil},
		{"bash -s < x.sh", nil},
		{"bash $SCRIPT", nil},
		{"echo build.sh", nil},
		{"git add build.sh", nil},
		{"ls", nil},
	} {
		if got := ScriptFiles(c.cmd); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: %q, want %q", c.cmd, got, c.want)
		}
	}
	if got := ScriptFiles("./a.sh; ./b.sh; ./c.sh; ./d.sh; ./e.sh"); len(got) != MaxScripts {
		t.Fatalf("capped at MaxScripts: %v", got)
	}
}
