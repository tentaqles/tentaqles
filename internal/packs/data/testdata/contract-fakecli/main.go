// Command contract-fakecli stands in for datacontract-cli in the data pack's
// contract tests. It never touches the network.
//
//	--version                 prints TQ_FAKE_DC_VERSION (default 0.11.2)
//	lint|breaking ...         appends its argv to TQ_FAKE_DC_LOG, prints
//	                          TQ_FAKE_DC_OUT, exits with TQ_FAKE_DC_EXIT
//	                          (or 1 when an argument contains TQ_FAKE_DC_FAIL_ON)
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
	if len(args) > 0 && args[0] == "--version" {
		v := os.Getenv("TQ_FAKE_DC_VERSION")
		if v == "" {
			v = "0.11.2"
		}
		fmt.Println(v)
		return
	}
	if log := os.Getenv("TQ_FAKE_DC_LOG"); log != "" {
		f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintln(f, strings.Join(args, " "))
			f.Close()
		}
	}
	if d, err := time.ParseDuration(os.Getenv("TQ_FAKE_DC_SLEEP")); err == nil {
		time.Sleep(d)
	}
	fmt.Print(os.Getenv("TQ_FAKE_DC_OUT"))
	code, _ := strconv.Atoi(os.Getenv("TQ_FAKE_DC_EXIT"))
	if on := os.Getenv("TQ_FAKE_DC_FAIL_ON"); on != "" {
		for _, a := range args {
			if strings.Contains(a, on) {
				code = 1
			}
		}
	}
	os.Exit(code)
}
