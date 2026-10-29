package scan

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

// encodePwsh is what `pwsh -EncodedCommand` takes: base64 of UTF-16LE.
func encodePwsh(s string) string {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u))
	for i, r := range u {
		b[2*i], b[2*i+1] = byte(r), byte(r>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func TestPowerShellForms(t *testing.T) {
	enc := encodePwsh("airflow dags trigger orders_daily")
	cases := []struct {
		prog    string
		args    string
		script  string
		encoded bool
		opaque  bool
	}{
		{"pwsh", "-Command airflow dags trigger x", "airflow dags trigger x", false, false},
		{"pwsh", "-c dbt run", "dbt run", false, false},
		{"pwsh", "-NoProfile -ExecutionPolicy Bypass -Com dbt run", "dbt run", false, false},
		{"powershell.exe", "-NoLogo dbt run --target prod", "dbt run --target prod", false, false},
		{"pwsh", "-e " + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-ec " + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-enc " + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-EncodedCom " + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-ENCODEDCOMMAND " + enc, "airflow dags trigger orders_daily", true, false},
		{`C:\Program Files\PowerShell\7\pwsh.exe`, "--EncodedCommand " + enc, "airflow dags trigger orders_daily", true, false},
		{"powershell", "/enc " + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-EncodedCommand:" + enc, "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-e " + strings.TrimRight(enc, "="), "airflow dags trigger orders_daily", true, false},
		{"pwsh", "-e not*base64", "", false, true},
		{"pwsh", "-e", "", false, true},
		{"pwsh", "-File run.ps1", "", false, true},
		{"pwsh", "-f run.ps1", "", false, true},
		{"pwsh", "-NoProfile -fi run.ps1", "", false, true},
		{"pwsh", "run.ps1", "", false, true},
		{"pwsh", "-Command -", "", false, true},
		{"pwsh", "-", "", false, true},
		{"pwsh", "-WorkingDirectory dags -Version", "", false, false},
		{"pwsh", "-v", "", false, false},
		// final review m1: an absolute Unix script path is a bare script,
		// not a /parameter
		{"pwsh", "/home/tester/x.ps1", "", false, true},
		{"pwsh", "-NoProfile /tmp/x.ps1", "", false, true},
		{"pwsh", "/x.ps1", "", false, true},
		{"pwsh", "/NoProfile /c dbt run", "dbt run", false, false},
		{"powershell", "/NoLogo /Command dbt run", "dbt run", false, false},
	}
	for _, c := range cases {
		r := PowerShell(c.prog, strings.Fields(c.args))
		if r.Script != c.script || r.Encoded != c.encoded || (r.Opaque != "") != c.opaque {
			t.Errorf("%s %s: %+v", c.prog, c.args, r)
		}
	}
}

func TestPowerShellRuns(t *testing.T) {
	enc := encodePwsh("dbt run --target prod")
	runs, trunc := PowerShellRuns(`echo hi; pwsh -NoProfile -EncodedCommand ` + enc + ` | Out-Null; bash -c "pwsh -File x.ps1"`)
	if trunc || len(runs) != 2 || runs[0].Script != "dbt run --target prod" || !runs[0].Encoded || runs[1].Opaque == "" {
		t.Fatalf("%v %+v", trunc, runs)
	}
	if runs, _ := PowerShellRuns("echo pwsh is a word here"); len(runs) != 0 {
		t.Fatalf("%+v", runs)
	}
	if _, trunc := PowerShellRuns(strings.Repeat("x ", MaxPwshWords+10)); !trunc {
		t.Fatal("a command past the word cap is truncated")
	}
	if !IsPowerShell(`C:\Windows\System32\WindowsPowerShell\v1.0\PowerShell.EXE`) || IsPowerShell("pwshx") {
		t.Fatal("IsPowerShell")
	}
}
