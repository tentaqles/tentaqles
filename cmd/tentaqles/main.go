// Command tentaqles is the CLI. The release builds this package under both names, tentaqles and its
// alias tq, so the two binaries are the same bytes; cmd/tq is the same program for
// `go install .../cmd/tq`.
package main

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/lobe/hook"
)

func main() {
	// Run from an identity's PATH mask, this program answers as the stub: command not found.
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	// Run from a client's shims folder, this program answers as the browser shim.
	if ok, code := cmd.ShimMain(os.Stderr, os.Args[1:]); ok {
		os.Exit(code)
	}
	// A lobe hook is answered here, before any command code runs: it fails open and never stalls.
	if ok, code := hook.Main(os.Args[1:], os.Stdin, os.Stdout); ok {
		os.Exit(code)
	}
	os.Exit(cmd.RunMain(os.Args[1:], os.Stdout, os.Stderr))
}
