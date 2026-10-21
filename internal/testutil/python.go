package testutil

import (
	"os/exec"
	"strings"
	"sync"
	"testing"
)

var (
	pythonOnce sync.Once
	pythonArgv []string
)

// Python returns the argv prefix of a working Python 3 (python3, python, then py -3; the
// Windows Store alias is skipped), or skips the test when there is none.
func Python(t testing.TB) []string {
	t.Helper()
	pythonOnce.Do(func() { pythonArgv = findPython() })
	if pythonArgv == nil {
		t.Skip("python 3 not found")
	}
	return append([]string(nil), pythonArgv...)
}

func findPython() []string {
	for _, c := range [][]string{{"python3"}, {"python"}, {"py", "-3"}} {
		p, err := exec.LookPath(c[0])
		if err != nil || strings.Contains(strings.ToLower(p), "windowsapps") {
			continue
		}
		argv := append([]string{p}, c[1:]...)
		out, err := exec.Command(argv[0], append(argv[1:], "-c", "import sys; print(sys.version_info[0])")...).Output()
		if err == nil && strings.TrimSpace(string(out)) == "3" {
			return argv
		}
	}
	return nil
}
