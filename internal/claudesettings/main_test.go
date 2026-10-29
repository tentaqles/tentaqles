package claudesettings

import (
	"os"
	"testing"
)

// Desired reads the global guard.yaml: point TQ_HOME at an empty folder so no
// test reads the developer's real file. Tests that need one set their own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tq-claudesettings-")
	if err != nil {
		panic(err)
	}
	os.Setenv("TQ_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
