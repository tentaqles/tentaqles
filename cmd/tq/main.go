package main

import (
	"fmt"
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
)

func main() {
	if err := cmd.NewRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "tq:", err)
		os.Exit(1)
	}
}
