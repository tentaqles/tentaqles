package ml

// The scan limits never pass silently, and an unreadable manifest is a
// finding (review r0 m-1, m-3).

import (
	"context"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/packs"
)

func lowerFileCap(t *testing.T, n int) {
	t.Helper()
	old := maxFiles
	maxFiles = n
	t.Cleanup(func() { maxFiles = old })
}

func scanLimit(fs []packs.Finding) (packs.Finding, bool) {
	for _, f := range fs {
		if f.Rule == ruleScanLimit {
			return f, true
		}
	}
	return packs.Finding{}, false
}

func TestFileCapIsReported(t *testing.T) {
	lintIsolate(t)
	lowerFileCap(t, 2)
	nb := `{"cells": [{"cell_type": "code", "source": ["x = 1"], "outputs": [], "metadata": {}}], "metadata": {}, "nbformat": 4, "nbformat_minor": 5}`
	files := map[string]string{
		"a.py":    "import joblib\nm = joblib.load(p)\nmodel.fit(X, y)\n",
		"b.py":    "import joblib\nm = joblib.load(p)\nmodel.fit(X, y)\n",
		"c.py":    "import joblib\nm = joblib.load(p)\nmodel.fit(X, y)\n",
		"a.ipynb": nb, "b.ipynb": nb, "c.ipynb": nb,
	}
	in := flagInput(t, "ml: {enabled: true}", files)
	ctx := context.Background()
	checks := map[string]func(context.Context, *packs.Input) packs.Result{
		"ml/unsafe-deserialize": runDeserialize,
		"ml/notebook-outputs":   runNotebookOutputs,
		"ml/leakage":            runLeakage,
	}
	for id, run := range checks {
		r := run(ctx, in)
		f, ok := scanLimit(r.Findings)
		if !ok {
			t.Errorf("%s: no scan-limit finding past the cap: %+v", id, r.Findings)
			continue
		}
		if f.Level != packs.LevelWarn || f.File == "" || !strings.Contains(f.Message, "scanned 2 of ") || !strings.Contains(f.Message, "2-file cap") {
			t.Errorf("%s: finding = %+v", id, f)
		}
	}
}

func TestDeadlineIsReported(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {enabled: true}", map[string]string{
		"a.py": "import joblib\nm = joblib.load(p)\n", "requirements.txt": "numpy\n",
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for id, run := range map[string]func(context.Context, *packs.Input) packs.Result{
		"ml/unsafe-deserialize": runDeserialize,
		"ml/env-lock":           runEnvLock,
	} {
		f, ok := scanLimit(run(ctx, in).Findings)
		if !ok || !strings.Contains(f.Message, "time limit") {
			t.Errorf("%s: a cancelled check passes silently: %+v", id, f)
		}
	}
}

func TestUnderCapHasNoScanLimit(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {enabled: true}", map[string]string{"a.py": "import joblib\nm = joblib.load(p)\n"})
	if f, ok := scanLimit(runDeserialize(context.Background(), in).Findings); ok {
		t.Errorf("unexpected %+v", f)
	}
}

func TestUnreadablePyprojectIsAFinding(t *testing.T) {
	lintIsolate(t)
	in := flagInput(t, "ml: {enabled: true}", map[string]string{"pyproject.toml": "[project]\ndependencies = [\"numpy\"]\n"})
	// --staged over an index that cannot show the file: ReadNew fails.
	in.Staged, in.Git = true, dirGit{}
	r := runEnvLock(context.Background(), in)
	if len(r.Findings) != 1 || r.Findings[0].Rule != "unreadable" || r.Findings[0].File != "pyproject.toml" || r.Findings[0].Level != packs.LevelWarn {
		t.Fatalf("result = %+v", r)
	}
}
