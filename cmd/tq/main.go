package main

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/envplan"
)

func main() {
	// Run from a guard: v2 identity's PATH mask (aws.exe there is a link to tq), tq answers as
	// the stub: command not found, exit 127 (ADR-0008).
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	os.Exit(cmd.RunMain(os.Args[1:], os.Stdout, os.Stderr))
}
