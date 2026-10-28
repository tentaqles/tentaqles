package secret

import (
	"errors"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

// testValue is built at run time so no token-shaped literal sits in the source.
func testValue() string { return "tk" + strings.Repeat("Zq9", 9) }

// mock swaps the OS keychain for go-keyring's in-memory mock: no test touches a real keychain.
func mock(t *testing.T) {
	t.Helper()
	MockInit()
}

func TestServiceAndAccount(t *testing.T) {
	if Service != "tentaqles" || Account("acme", "jira") != "acme/jira" {
		t.Fatal(Service, Account("acme", "jira"))
	}
}

func TestSetGetHasDelete(t *testing.T) {
	mock(t)
	if ok, err := Has("acme", "jira"); err != nil || ok {
		t.Fatalf("has before set: %v %v", ok, err)
	}
	if _, err := Get("acme", "jira"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get before set: %v", err)
	}
	if err := Set("acme", "jira", "  "+testValue()+"\r\n"); err != nil {
		t.Fatal(err)
	}
	got, err := Get("acme", "jira")
	if err != nil || got != testValue() {
		t.Fatalf("get: the stored value is trimmed and returned unchanged (%v)", err)
	}
	// It sits under service tentaqles, account <ws>/<provider>.
	if raw, err := keyring.Get("tentaqles", "acme/jira"); err != nil || raw != testValue() {
		t.Fatalf("keyring entry: %v", err)
	}
	if ok, err := Has("acme", "jira"); err != nil || !ok {
		t.Fatalf("has after set: %v %v", ok, err)
	}
	if ok, _ := Has("acme", "github"); ok {
		t.Fatal("another provider is not present")
	}
	if err := Delete("acme", "jira"); err != nil {
		t.Fatal(err)
	}
	if err := Delete("acme", "jira"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestSetRefusesEmptyAndBadNames(t *testing.T) {
	mock(t)
	if err := Set("acme", "jira", " \n"); err == nil {
		t.Fatal("an empty token was stored")
	}
	for _, c := range [][2]string{{"", "jira"}, {"ac/me", "jira"}, {`ac\me`, "jira"}, {"ac\nme", "jira"}, {" acme", "jira"}, {"acme", "trello"}, {"acme", ""}} {
		if err := ValidateNames(c[0], c[1]); err == nil {
			t.Fatalf("%q accepted", c)
		}
		if err := Set(c[0], c[1], testValue()); err == nil {
			t.Fatalf("set %q accepted", c)
		}
	}
	if err := ValidateNames("acme-2", "azure-devops"); err != nil {
		t.Fatal(err)
	}
}

func TestErrorsNeverCarryTheValue(t *testing.T) {
	MockInitWithError(errors.New("keychain locked"))
	t.Cleanup(MockInit)
	err := Set("acme", "jira", testValue())
	if err == nil || strings.Contains(err.Error(), testValue()) || !strings.Contains(err.Error(), "acme/jira") {
		t.Fatalf("%v", err)
	}
	if _, err := Get("acme", "jira"); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a backend failure is not 'absent': %v", err)
	}
	if _, err := Has("acme", "jira"); err == nil {
		t.Fatal("has hides a backend failure")
	}
}

type memBackend map[string]string

func (m memBackend) Get(s, u string) (string, error) {
	v, ok := m[s+"|"+u]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}
func (m memBackend) Set(s, u, v string) error { m[s+"|"+u] = v; return nil }
func (m memBackend) Delete(s, u string) error {
	if _, ok := m[s+"|"+u]; !ok {
		return keyring.ErrNotFound
	}
	delete(m, s+"|"+u)
	return nil
}

func TestInjectableBackend(t *testing.T) {
	mock(t)
	m := memBackend{}
	restore := SetBackend(m)
	if err := Set("acme", "linear", testValue()); err != nil {
		t.Fatal(err)
	}
	if m["tentaqles|acme/linear"] != testValue() {
		t.Fatal("the injected backend was not used")
	}
	restore()
	if ok, _ := Has("acme", "linear"); ok {
		t.Fatal("restore did not put the previous backend back")
	}
}
