package integration

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The release ships one program under two names (ADR-0021): `tentaqles`, the command of
// docs/IDEATION.md, and `tq`, which the plugin hook resolves. The two builds compile the same
// main package with the same flags, so the binaries are the same bytes and a browser shim or a
// PATH-mask stub copied from one matches the other. Both land in every archive, and both
// installers put both on the PATH.
func TestGoreleaserBuildsOneProgramUnderTwoNames(t *testing.T) {
	root := filepath.Join("..", "..")
	raw, err := os.ReadFile(filepath.Join(root, ".goreleaser.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Builds   []map[string]any `yaml:"builds"`
		Archives []struct {
			IDs []string `yaml:"ids"`
		} `yaml:"archives"`
		Casks []struct {
			Binaries []string `yaml:"binaries"`
		} `yaml:"homebrew_casks"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	byBinary := map[string]map[string]any{}
	for _, b := range cfg.Builds {
		name, _ := b["binary"].(string)
		byBinary[name] = b
	}
	tq, tentaqles := byBinary["tq"], byBinary["tentaqles"]
	if tq == nil || tentaqles == nil || len(cfg.Builds) != 2 {
		t.Fatalf("want exactly the builds tq and tentaqles, got %v", cfg.Builds)
	}
	if tq["main"] != "./cmd/tentaqles" {
		t.Errorf("tq builds %v, want ./cmd/tentaqles (the same package as tentaqles)", tq["main"])
	}
	strip := func(b map[string]any) map[string]any {
		c := map[string]any{}
		for k, v := range b {
			if k != "id" && k != "binary" {
				c[k] = v
			}
		}
		return c
	}
	if !reflect.DeepEqual(strip(tq), strip(tentaqles)) {
		t.Errorf("the two builds differ beyond id and binary:\n tq:        %v\n tentaqles: %v", strip(tq), strip(tentaqles))
	}
	if f, _ := tq["flags"].([]any); !slices.Contains(f, any("-trimpath")) {
		t.Errorf("builds without -trimpath: %v", tq["flags"])
	}
	for _, a := range cfg.Archives {
		for _, id := range []string{tq["id"].(string), tentaqles["id"].(string)} {
			if !slices.Contains(a.IDs, id) {
				t.Errorf("archive ids %v miss the build %s", a.IDs, id)
			}
		}
	}

	for _, c := range cfg.Casks {
		if !slices.Contains(c.Binaries, "tq") || !slices.Contains(c.Binaries, "tentaqles") {
			t.Errorf("the Homebrew cask installs %v, want tq and tentaqles", c.Binaries)
		}
	}
	if len(cfg.Casks) == 0 {
		t.Error("no Homebrew cask")
	}

	for _, inst := range []string{"install.sh", "install.ps1"} {
		b, err := os.ReadFile(filepath.Join(root, "installers", inst))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		if !strings.Contains(s, "tentaqles") || !strings.Contains(s, "tq") || strings.Contains(s, "tq init") {
			t.Errorf("%s does not install both names or still points at tq init", inst)
		}
	}
}
