// Command dagster is a fake Dagster CLI for the data/dagster-validate tests.
// It never loads code or opens a connection.
//
//	--version                    prints FAKE_DAGSTER_VERSION (default 1.9.3)
//	definitions validate         prints FAKE_DAGSTER_OUT, sleeps FAKE_DAGSTER_SLEEP
//	                             seconds and exits FAKE_DAGSTER_EXIT (default 0)
//
// The arguments it got are written to FAKE_DAGSTER_ARGS when set.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]
	if f := os.Getenv("FAKE_DAGSTER_ARGS"); f != "" {
		_ = os.WriteFile(f, []byte(strings.Join(args, " ")), 0o644)
	}
	if len(args) == 1 && args[0] == "--version" {
		v := os.Getenv("FAKE_DAGSTER_VERSION")
		if v == "" {
			v = "1.9.3"
		}
		fmt.Printf("dagster, version %s\n", v)
		return
	}
	if len(args) < 2 || args[0] != "definitions" || args[1] != "validate" {
		fmt.Fprintln(os.Stderr, "fake dagster: unsupported command")
		os.Exit(2)
	}
	if s, err := strconv.Atoi(os.Getenv("FAKE_DAGSTER_SLEEP")); err == nil && s > 0 {
		time.Sleep(time.Duration(s) * time.Second)
	}
	fmt.Fprint(os.Stderr, os.Getenv("FAKE_DAGSTER_OUT"))
	code, _ := strconv.Atoi(os.Getenv("FAKE_DAGSTER_EXIT"))
	os.Exit(code)
}
