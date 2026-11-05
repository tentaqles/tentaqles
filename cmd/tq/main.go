package main

import (
	"os"

	"github.com/tentaqles/tentaqles/internal/cmd"
)

func main() {
	os.Exit(cmd.RunMain(os.Args[1:], os.Stdout, os.Stderr))
}
