// Command tentaqles is the CLI. cmd/tq builds the same program under its alias.
package main

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/envplan"
)

func main() {
	// Run from an identity's PATH mask, this program answers as the stub: command not found (ADR-0008).
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	os.Exit(cmd.RunMain(os.Args[1:], os.Stdout, os.Stderr))
}
