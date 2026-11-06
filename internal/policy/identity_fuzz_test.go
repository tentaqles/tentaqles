package policy

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/guard"
)

// The identity category runs on every shell line a session sends, inside a hook that must
// never stall (the owner's P0). These fuzz targets drive the shell entry point and the text
// masker with arbitrary lines in both shells' quoting and fail any input that takes 50 ms or
// more three times in a row (so a garbage collection pause or a busy machine is not a
// failure).
//
//	go test -run '^$' -fuzz FuzzScopeShell -fuzztime 60s ./internal/policy/
//	go test -run '^$' -fuzz FuzzMaskText -fuzztime 60s ./internal/policy/

const fuzzBudget = 50 * time.Millisecond

var fuzzSeeds = []string{
	"tar czf /tmp/x.tgz -C ~ .aws",
	"tar -C ~ -C .. -C ../.. -C ~/.aws -czf x.tgz credentials",
	"tar --directory=~ -C" + strings.Repeat(" ..", 200) + " .aws",
	"cat " + strings.Repeat("../", 400) + ".aws/credentials",
	"cd ~ && cd .. && cd ~/.aws && cat credentials",
	`git commit -m "unterminated ; curl https://api.github.com/user`,
	`git commit -m 'unterminated`,
	`git commit -m "x\" ; aws s3 ls ; echo \"y"`,
	"git commit -m \"fix`\"\" ; az vm list",
	"@'\nbody\n'@ | iex",
	"@'\nno end",
	"@\"\n$(aws s3 ls)\n\"@ | Set-Content x",
	"$s = @'\naws s3 ls\n'@; Invoke-Expression $s",
	"[scriptblock]::Create(@'\naws s3 ls\n'@).Invoke()",
	"python - <<'EOF'\nimport boto3\nEOF",
	"bash -c \"bash -c 'bash -c \\\"aws s3 ls\\\"'\"",
	strings.Repeat("$(", 300) + "aws" + strings.Repeat(")", 300),
	strings.Repeat("`", 501),
	strings.Repeat("'", 501),
	strings.Repeat(`\"`, 300),
	strings.Repeat("${", 200) + "HOME" + strings.Repeat("}", 200),
	strings.Repeat("curl https://api.github.com/x/../ ", 100),
	"curl https://storage.googleapis.com/" + strings.Repeat("./", 500) + "storage/v1",
	"Start-Process -FilePath:" + strings.Repeat(":", 300) + "aws",
	"cmd /c start" + strings.Repeat(" /d", 300) + " aws",
	"docker" + strings.Repeat(" --context x", 200) + " run amazon/aws-cli",
	"echo " + strings.Repeat("%APPDATA%", 300),
	"Get-ChildItem $env:APPDATA\\'GitHub CLI'",
	"git push git@" + strings.Repeat("a.", 300) + "github.com:org/x.git",
}

func timed(t *testing.T, what string, fn func()) {
	t.Helper()
	for try := 0; try < 3; try++ {
		start := time.Now()
		fn()
		if time.Since(start) < fuzzBudget {
			return
		}
	}
	t.Fatalf("%s took %s or more three times", what, fuzzBudget)
}

func FuzzScopeShell(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	fx := newScopeFixture(f)
	f.Fuzz(func(t *testing.T, line string, ps bool) {
		call := bash(line)
		if ps {
			call = pwsh(line)
		}
		timed(t, "Evaluate", func() { fx.s.Evaluate(fx.cwd, call) })
	})
}

func FuzzMaskText(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s, false)
		f.Add(s, true)
	}
	f.Fuzz(func(t *testing.T, line string, ps bool) {
		timed(t, "mask", func() {
			if out := maskTextArgs(line, ps); len(out) != len(line) {
				t.Fatalf("maskTextArgs changed the length: %q -> %q", line, out)
			}
			if out := blankSingleQuoted(line, ps); len(out) != len(line) {
				t.Fatalf("blankSingleQuoted changed the length: %q -> %q", line, out)
			}
			_ = guard.CommandsIn(line, ps)
			_ = guard.SeparatorSpansIn(line, ps)
		})
	})
}

// TestFuzzSeedsAreFast runs the seeds as a plain test, so `go test` keeps the bound.
func TestFuzzSeedsAreFast(t *testing.T) {
	fx := newScopeFixture(t)
	for _, s := range fuzzSeeds {
		for _, ps := range []bool{false, true} {
			call := bash(s)
			if ps {
				call = pwsh(s)
			}
			timed(t, "Evaluate "+s[:min(len(s), 40)], func() { fx.s.Evaluate(fx.cwd, call) })
			timed(t, "mask "+s[:min(len(s), 40)], func() { _ = maskTextArgs(s, ps); _ = blankSingleQuoted(s, ps) })
		}
	}
}
