package ml

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
	"github.com/tentaqles/tentaqles/internal/testutil"
)

const cardConfig = `
ml:
  enabled: true
  paths: { train: ["src/train/**"], features: ["src/features/**"] }
  data_manifest: data/snapshots.yaml
  models:
    - { name: churn, card: models/churn/MODEL_CARD.md, metrics: reports/churn/metrics.json }
`

func cardInput(t *testing.T, card string, changed ...string) *packs.Input {
	t.Helper()
	root := testutil.TempDir(t)
	writeFiles(t, root, map[string]string{
		"models/churn/MODEL_CARD.md": card,
		"data/snapshots.yaml":        readFixture(t, "promote-card/snapshots.yaml"),
		"src/train/fit.py":           "print('fit')\n",
	})
	in := mlInput(t, root, cardConfig, "")
	if len(changed) == 0 {
		in.All = true
		in.Changed = []packs.ChangedFile{{Path: "models/churn/MODEL_CARD.md", Status: 'A'}}
	} else {
		in.Base = "HEAD"
		for _, c := range changed {
			in.Changed = append(in.Changed, packs.ChangedFile{Path: c, Status: 'M'})
		}
	}
	return in.ForPack("ml")
}

func TestModelCardGolden(t *testing.T) {
	dirs, _ := filepath.Glob(filepath.Join("testdata", "promote-card", "*", "want.json"))
	if len(dirs) < 7 {
		t.Fatalf("expected the golden cases, got %d", len(dirs))
	}
	for _, w := range dirs {
		dir := filepath.Base(filepath.Dir(w))
		t.Run(dir, func(t *testing.T) {
			var want struct{ Errors, Warns []string }
			raw, _ := os.ReadFile(w)
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			in := cardInput(t, readFixture(t, "promote-card/"+dir+"/MODEL_CARD.md"))
			r := runModelCard(context.Background(), in)
			if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, want.Errors) {
				t.Fatalf("errors %v, want %v (%+v)", got, want.Errors, r.Findings)
			}
			if got := rules(r.Findings, packs.LevelWarn); !reflect.DeepEqual(got, want.Warns) {
				t.Fatalf("warns %v, want %v (%+v)", got, want.Warns, r.Findings)
			}
			for _, f := range r.Findings {
				if strings.Contains(f.Message, "tester@acme.test") {
					t.Fatalf("a finding carries the value: %+v", f)
				}
				if f.File != "models/churn/MODEL_CARD.md" || f.Line == 0 {
					t.Fatalf("finding without a location: %+v", f)
				}
			}
		})
	}
}

func TestModelCardTemplateFails(t *testing.T) {
	in := cardInput(t, readFixture(t, "promote-card/template/MODEL_CARD.md"))
	r := runModelCard(context.Background(), in)
	errs := rules(r.Findings, packs.LevelError)
	if len(errs) < 9 {
		t.Fatalf("the unfilled template must fail every section: %v", errs)
	}
	for _, e := range errs {
		if e == "card-heading-missing" {
			t.Fatalf("the template must carry every required heading: %+v", r.Findings)
		}
	}
}

func TestModelCardMissing(t *testing.T) {
	in := cardInput(t, "x")
	os.Remove(filepath.Join(in.Root, "models", "churn", "MODEL_CARD.md"))
	r := runModelCard(context.Background(), in)
	if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, []string{"card-missing"}) {
		t.Fatalf("%+v", r.Findings)
	}
}

func TestModelCardNotUpdated(t *testing.T) {
	good := readFixture(t, "promote-card/good/MODEL_CARD.md")
	in := cardInput(t, good, "src/train/fit.py")
	if !modelCardApplies(in) {
		t.Fatal("training code changed: the check applies")
	}
	r := runModelCard(context.Background(), in)
	if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, []string{"card-not-updated"}) {
		t.Fatalf("%+v", r.Findings)
	}
	in = cardInput(t, good, "src/train/fit.py", "models/churn/MODEL_CARD.md")
	if r := runModelCard(context.Background(), in); len(r.Findings) != 0 {
		t.Fatalf("card changed with the code: %+v", r.Findings)
	}
	in = cardInput(t, good, "README.md")
	if modelCardApplies(in) {
		t.Fatal("an unrelated change does not apply")
	}
}

func TestModelCardSnapshotUnknown(t *testing.T) {
	good := readFixture(t, "promote-card/good/MODEL_CARD.md")
	in := cardInput(t, strings.Replace(good, "churn-2026-09", "churn-2026-10", 1))
	r := runModelCard(context.Background(), in)
	if got := rules(r.Findings, packs.LevelError); !reflect.DeepEqual(got, []string{"card-snapshot-mismatch"}) {
		t.Fatalf("%+v", r.Findings)
	}
}

// With a dvc.lock data manifest the snapshot is not compared, and the card
// check says so (review r0 finding 9).
func TestModelCardDVCLockNotCompared(t *testing.T) {
	good := readFixture(t, "promote-card/good/MODEL_CARD.md")
	in := cardInput(t, strings.Replace(good, "churn-2026-09", "churn-2026-10", 1))
	setConfig(t, in, strings.Replace(cardConfig, "data/snapshots.yaml", "dvc.lock", 1), "")
	in = in.ForPack("ml")
	r := runModelCard(context.Background(), in)
	if got := rules(r.Findings, packs.LevelError); len(got) != 0 {
		t.Fatalf("errors %v", got)
	}
	if got := rules(r.Findings, packs.LevelWarn); !reflect.DeepEqual(got, []string{"card-snapshot-unchecked"}) {
		t.Fatalf("warns %+v", r.Findings)
	}
}

func TestAppendPromotionHistory(t *testing.T) {
	good := readFixture(t, "promote-card/good/MODEL_CARD.md")
	out, err := appendPromotionRow(good, []string{"2026-10-28", "7", "production", "dec_1", "human"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 2026-10-28 | 7 | production | dec_1 | human |\n") || !strings.HasSuffix(out, "| human |\n") {
		t.Fatalf("row not appended:\n%s", out)
	}
	out2, _ := appendPromotionRow(out, []string{"2026-10-29", "8", "production", "dec_2", "human"})
	if strings.Index(out2, "dec_1") > strings.Index(out2, "dec_2") {
		t.Fatal("rows keep their order")
	}
	if _, err := appendPromotionRow("# card\n", []string{"a", "b", "c", "d", "e"}); err == nil {
		t.Fatal("a card without the section is an error")
	}
	// A section followed by another one keeps the next section intact.
	mid := strings.Replace(good, "## Promotion history", "## Promotion history", 1) + "\n## Notes\n\nkeep\n"
	out3, err := appendPromotionRow(mid, []string{"d", "v", "t", "id", "human"})
	if err != nil || !strings.Contains(out3, "| d | v | t | id | human |\n\n## Notes") {
		t.Fatalf("%v\n%s", err, out3)
	}
}
