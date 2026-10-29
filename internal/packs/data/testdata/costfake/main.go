// Command costfake stands in for bq, snow, databricks, sqlcmd, psql and dbt
// in the cost tests. It never touches a network. Behaviour comes from files
// in $TQ_COST_FAKE_DIR, keyed by the name it was invoked as:
//
//	<tool>.out       printed to stdout
//	<tool>.exit      exit code (default 0)
//	<tool>.manifest  (dbt) copied to <--target-path>/manifest.json
//	<tool>.log       appended: argv, stdin and the -i input file
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println(name + " fake 9.9.9")
		return
	}
	dir := os.Getenv("TQ_COST_FAKE_DIR")
	var stdin []byte
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		stdin, _ = io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	}
	var log strings.Builder
	log.WriteString("argv:" + strings.Join(args, "\x1f") + "\n")
	log.WriteString("stdin:" + string(stdin) + "\n")
	for i, a := range args {
		if a == "-i" && i+1 < len(args) {
			b, _ := os.ReadFile(args[i+1])
			log.WriteString("input:" + string(b) + "\n")
		}
		if a == "--target-path" && i+1 < len(args) {
			if b, err := os.ReadFile(filepath.Join(dir, name+".manifest")); err == nil {
				_ = os.MkdirAll(args[i+1], 0o755)
				_ = os.WriteFile(filepath.Join(args[i+1], "manifest.json"), b, 0o644)
			}
		}
	}
	if f, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(log.String())
		f.Close()
	}
	if b, err := os.ReadFile(filepath.Join(dir, name+".out")); err == nil {
		os.Stdout.Write(b)
	}
	if b, err := os.ReadFile(filepath.Join(dir, name+".exit")); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
			os.Exit(n)
		}
	}
}
