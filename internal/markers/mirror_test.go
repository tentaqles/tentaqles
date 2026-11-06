package markers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// plugin/scripts/tq_env.sh resolves the tq home the way paths.Home does,
// TQ_HOME ignored under a marker, to put Python's pycache prefix inside the
// protected state folder (ADR-0004): its marker list is this list, word for
// word.
func TestTqEnvShMirrorsTheMarkers(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "plugin", "scripts", "tq_env.sh"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^for _tq_m in ([A-Z0-9_ ]+); do`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("tq_env.sh has no marker loop")
	}
	if got, want := strings.Fields(string(m[1])), Markers; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("tq_env.sh markers\n got %v\nwant %v", got, want)
	}
}
