package main

import (
	"fmt"
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
	"github.com/tentaqles/tentaqles/internal/hub/sync/cred"
)

func main() {
	// The real credential chain (cli, keychain, env, mcp) is installed only in the binary, so no
	// test reaches the OS keychain through cred.Resolve.
	cred.SetDefault(cred.NewChain())
	if err := cmd.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tq:", err)
		os.Exit(1)
	}
}
