package main

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/envplan"
	"github.com/tentaqles/tentaqles/internal/lobe/hook"
)

func main() {
	// Run from a client's PATH mask (aws.exe there is a link to tq), tq answers as
	// the stub: command not found, exit 127.
	if ok, code := envplan.StubMain(os.Stderr); ok {
		os.Exit(code)
	}
	// Run from a client's shims folder (xdg-open, open, tq-open), tq answers as the browser
	// shim: the URL in the client's profile.
	if ok, code := cmd.ShimMain(os.Stderr, os.Args[1:]); ok {
		os.Exit(code)
	}
	// A lobe hook is answered here, before any command code runs: it fails open and never stalls.
	if ok, code := hook.Main(os.Args[1:], os.Stdin, os.Stdout); ok {
		os.Exit(code)
	}
	os.Exit(cmd.RunMain(os.Args[1:], os.Stdout, os.Stderr))
}
