package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeAWSConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const awsOneSession = "[profile dev]\nsso_session = acme\nsso_account_id = 123456789012\n\n[sso-session acme]\nsso_start_url = https://example.awsapps.com/start\nsso_region = us-east-1\n"

func TestAWSLoginArgs(t *testing.T) {
	two := awsOneSession + "\r\n[sso-session  other ]\r\nsso_region = us-east-1\r\n"
	cases := []struct {
		name    string
		cfg     string
		sso     bool
		session string
		want    []string
	}{
		{"missing file", filepath.Join(t.TempDir(), "nope"), false, "", []string{"configure"}},
		{"empty path", "", false, "", []string{"configure"}},
		{"no sessions", writeAWSConfig(t, "[default]\nregion = us-east-1\n"), false, "", []string{"configure"}},
		{"one session", writeAWSConfig(t, awsOneSession), false, "", []string{"sso", "login", "--sso-session", "acme"}},
		{"sso flag wins over file", writeAWSConfig(t, awsOneSession), true, "", []string{"configure", "sso"}},
		{"sso flag no file", "", true, "", []string{"configure", "sso"}},
		{"session flag wins", writeAWSConfig(t, two), false, "other", []string{"sso", "login", "--sso-session", "other"}},
		{"session flag over sso flag", writeAWSConfig(t, awsOneSession), true, "x", []string{"sso", "login", "--sso-session", "x"}},
	}
	for _, c := range cases {
		got, err := awsLoginArgs(c.cfg, c.sso, c.session)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %v, %v want %v", c.name, got, err, c.want)
		}
	}
	_, err := awsLoginArgs(writeAWSConfig(t, two), false, "")
	if err == nil || !strings.Contains(err.Error(), "acme, other") || !strings.Contains(err.Error(), "--sso-session") {
		t.Fatalf("two sessions without a flag: %v", err)
	}
}

func TestAWSConfigFileFromEnv(t *testing.T) {
	if got := awsConfigFile([]string{"A=b", "AWS_CONFIG_FILE=/x/config"}); got != "/x/config" {
		t.Fatalf("got %q", got)
	}
	if got := awsConfigFile([]string{"A=b"}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestIsAWSSSOArgs(t *testing.T) {
	for args, want := range map[string]bool{"sso login": true, "configure sso": true, "configure": false, "": false} {
		if got := isAWSSSOArgs(strings.Fields(args)); got != want {
			t.Errorf("%q: %v", args, got)
		}
	}
}
