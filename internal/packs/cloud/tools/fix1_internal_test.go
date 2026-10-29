package tools

import (
	"testing"
)

// TestCheckovFilePathFallback: without file_abs_path, checkov's file_path
// ("/main.tf") is relative to the scanned directory. On Linux it looks
// absolute and used to come out as "/main.tf" (review r0 #6). The CI Linux
// job is where the old code fails; the cases hold on every OS.
func TestCheckovFilePathFallback(t *testing.T) {
	root := t.TempDir()
	for _, fp := range []string{"/main.tf", `\main.tf`, "main.tf", "/modules/db/main.tf"} {
		raw := []byte(`{"check_type":"terraform","results":{"failed_checks":[{"check_id":"CKV_X","check_name":"n","file_path":` + jsonStr(fp) + `,"file_line_range":[2,3]}]}}`)
		fs, err := ParseCheckov(raw, root, "infra/terraform", "")
		if err != nil || len(fs) != 1 {
			t.Fatalf("%s: %v %v", fp, fs, err)
		}
		want := "infra/terraform/main.tf"
		if fp == "/modules/db/main.tf" {
			want = "infra/terraform/modules/db/main.tf"
		}
		if fs[0].File != want {
			t.Errorf("%s: file = %q, want %q", fp, fs[0].File, want)
		}
	}
	if got := checkovRel("/main.tf"); got != "main.tf" {
		t.Errorf("checkovRel(/main.tf) = %q", got)
	}
	if got := checkovRel(`C:\repo\main.tf`); got != `C:\repo\main.tf` {
		t.Errorf("checkovRel(drive path) = %q", got)
	}
}

// TestCfnLintEmptyStdoutNonZeroExit: an empty stdout with a non-zero exit
// (errors 2, warnings 4) is a failed run, not a pass (review r0 #7).
func TestCfnLintEmptyStdoutNonZeroExit(t *testing.T) {
	for _, code := range []int{2, 4, 6, 8} {
		if fs, err := ParseCfnLint(nil, "", code); err == nil {
			t.Errorf("exit %d with empty stdout parsed as %v", code, fs)
		}
	}
	if fs, err := ParseCfnLint([]byte("  \n"), "", 0); err != nil || len(fs) != 0 {
		t.Errorf("exit 0 empty = %v %v", fs, err)
	}
}
