package detect

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/providers"
)

func TestCheck_Installed(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		CLI: &providers.CLI{
			Command:     "gh",
			VersionArgs: []string{"--version"},
		},
	}
	d := Deps{
		LookPath: func(cmd string) (string, error) {
			if cmd == "gh" {
				return "/usr/bin/gh", nil
			}
			return "", errors.New("not found")
		},
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			return "gh version 2.40.0 (2024-01-01)\nhttps://github.com/cli/cli\n", nil
		},
		GOOS: "linux",
	}

	r := Check(p, d)
	if !r.Installed {
		t.Fatalf("expected Installed=true")
	}
	if r.Path != "/usr/bin/gh" {
		t.Fatalf("expected Path set, got %q", r.Path)
	}
	if r.Version != "gh version 2.40.0 (2024-01-01)" {
		t.Fatalf("unexpected version: %q", r.Version)
	}
	if r.Err != "" {
		t.Fatalf("expected no Err, got %q", r.Err)
	}
}

func TestCheck_NoCLI(t *testing.T) {
	p := providers.Provider{ID: "foo"}
	d := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("nope") },
		Run: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("nope")
		},
		GOOS: "linux",
	}

	r := Check(p, d)
	if r.Installed {
		t.Fatalf("expected Installed=false")
	}
	if r.Err != "no CLI" {
		t.Fatalf("expected Err=%q, got %q", "no CLI", r.Err)
	}
}

func TestCheck_NotFound(t *testing.T) {
	p := providers.Provider{ID: "foo", CLI: &providers.CLI{Command: "foo"}}
	d := Deps{
		LookPath: func(string) (string, error) { return "", errors.New("not found") },
		Run: func(context.Context, string, ...string) (string, error) {
			t.Fatalf("Run should not be called when LookPath fails")
			return "", nil
		},
		GOOS: "linux",
	}

	r := Check(p, d)
	if r.Installed {
		t.Fatalf("expected Installed=false")
	}
	if r.Err == "" {
		t.Fatalf("expected an Err")
	}
}

func TestCheck_VersionRunError(t *testing.T) {
	p := providers.Provider{ID: "foo", CLI: &providers.CLI{Command: "foo", VersionArgs: []string{"--version"}}}
	d := Deps{
		LookPath: func(string) (string, error) { return "/bin/foo", nil },
		Run: func(context.Context, string, ...string) (string, error) {
			return "foo v1.2.3\n", errors.New("exit status 1")
		},
		GOOS: "linux",
	}

	r := Check(p, d)
	if !r.Installed {
		t.Fatalf("expected Installed=true even when version command exits non-zero")
	}
	if r.Version != "foo v1.2.3" {
		t.Fatalf("unexpected version: %q", r.Version)
	}
	if r.Err != "exit status 1" {
		t.Fatalf("unexpected err: %q", r.Err)
	}
}

func TestCheck_Timeout(t *testing.T) {
	p := providers.Provider{ID: "foo", CLI: &providers.CLI{Command: "foo", VersionArgs: []string{"--version"}}}
	d := Deps{
		LookPath: func(string) (string, error) { return "/bin/foo", nil },
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
		GOOS:    "linux",
		Timeout: 20 * time.Millisecond,
	}

	r := Check(p, d)
	if !r.Installed {
		t.Fatalf("expected Installed=true")
	}
	if r.Version != "" {
		t.Fatalf("expected empty version on timeout, got %q", r.Version)
	}
	if !strings.Contains(r.Err, "timeout") {
		t.Fatalf("expected Err to contain 'timeout', got %q", r.Err)
	}
}

func TestCheckAll_OrderPreserved(t *testing.T) {
	var ps []providers.Provider
	for i := 0; i < 20; i++ {
		ps = append(ps, providers.Provider{
			ID:  string(rune('a' + i)),
			CLI: &providers.CLI{Command: string(rune('a' + i))},
		})
	}
	d := Deps{
		LookPath: func(cmd string) (string, error) { return "/bin/" + cmd, nil },
		Run:      func(context.Context, string, ...string) (string, error) { return "v1\n", nil },
		GOOS:     "linux",
	}

	results := CheckAll(ps, d)
	if len(results) != len(ps) {
		t.Fatalf("expected %d results, got %d", len(ps), len(results))
	}
	for i, r := range results {
		if r.ID != ps[i].ID {
			t.Fatalf("order not preserved at index %d: got %q want %q", i, r.ID, ps[i].ID)
		}
	}
}

func TestHints_NoInstallNoCLI(t *testing.T) {
	p := providers.Provider{ID: "foo"}
	hints := Hints(p, "windows")
	if len(hints) != 1 || hints[0] != "no CLI to install" {
		t.Fatalf("unexpected hints: %v", hints)
	}
}

func TestHints_Windows(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		Install: providers.Install{
			Windows: providers.InstallOS{Winget: "gh", Scoop: "gh"},
		},
	}
	hints := Hints(p, "windows")
	want := []string{"winget install gh", "scoop install gh"}
	if len(hints) != len(want) {
		t.Fatalf("unexpected hints: %v", hints)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Fatalf("hint %d: got %q want %q", i, hints[i], want[i])
		}
	}
}

func TestHints_Darwin(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		Install: providers.Install{
			Macos: providers.InstallOS{Brew: "gh"},
		},
	}
	hints := Hints(p, "darwin")
	want := []string{"brew install gh"}
	if len(hints) != 1 || hints[0] != want[0] {
		t.Fatalf("unexpected hints: %v", hints)
	}
}

func TestHints_Linux(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		Install: providers.Install{
			Linux: providers.InstallOS{Apt: "gh", Pip: "gh", Npm: "gh", URL: "https://example.com"},
		},
	}
	hints := Hints(p, "linux")
	want := []string{"sudo apt install gh", "pip install gh", "npm install -g gh", "see https://example.com"}
	if len(hints) != len(want) {
		t.Fatalf("unexpected hints: %v", hints)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Fatalf("hint %d: got %q want %q", i, hints[i], want[i])
		}
	}
}

func TestHints_FallbackToPipNpmURL(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		Install: providers.Install{
			Windows: providers.InstallOS{Pip: "gh", Npm: "gh", URL: "https://example.com"},
		},
	}
	hints := Hints(p, "windows")
	want := []string{"pip install gh", "npm install -g gh", "see https://example.com"}
	if len(hints) != len(want) {
		t.Fatalf("unexpected hints: %v", hints)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Fatalf("hint %d: got %q want %q", i, hints[i], want[i])
		}
	}
}

func TestHints_NoteAppended(t *testing.T) {
	p := providers.Provider{
		ID: "gh",
		Install: providers.Install{
			Windows: providers.InstallOS{Winget: "gh", Note: "requires admin"},
		},
	}
	hints := Hints(p, "windows")
	want := []string{"winget install gh", "requires admin"}
	if len(hints) != len(want) {
		t.Fatalf("unexpected hints: %v", hints)
	}
	for i := range want {
		if hints[i] != want[i] {
			t.Fatalf("hint %d: got %q want %q", i, hints[i], want[i])
		}
	}
}

func TestDefaultDeps(t *testing.T) {
	d := DefaultDeps()
	if d.LookPath == nil || d.Run == nil || d.GOOS == "" {
		t.Fatalf("DefaultDeps returned incomplete Deps: %+v", d)
	}
}

func TestCheck_ProbeFailedSurfacesErr(t *testing.T) {
	p := providers.Provider{ID: "foo", CLI: &providers.CLI{Command: "foo", VersionArgs: []string{"--version"}}}
	d := Deps{
		LookPath: func(string) (string, error) { return "/bin/foo", nil },
		Run: func(context.Context, string, ...string) (string, error) {
			return "", errors.New("exit status 127")
		},
		GOOS: "linux",
	}

	r := Check(p, d)
	if !r.Installed {
		t.Fatalf("expected Installed=true")
	}
	if r.Version != "" {
		t.Fatalf("expected empty version, got %q", r.Version)
	}
	if r.Err == "" {
		t.Fatal("expected Err to be populated when the probe fails")
	}
}

func TestDefaultDeps_Timeout(t *testing.T) {
	if got := DefaultDeps().Timeout; got != DefaultTimeout {
		t.Fatalf("DefaultDeps().Timeout = %v, want %v", got, DefaultTimeout)
	}
}
