package db

import (
	"strings"
	"testing"
	"time"

	"github.com/tentaqles/tentaqles/internal/manifest"
	"github.com/tentaqles/tentaqles/internal/packs"
)

// Integration round 3: an invalid test_db is reported as invalid, never as
// not set.
func TestDoctorTestDBInvalid(t *testing.T) {
	root := doctorEnv(t)
	for _, block := range []string{
		"db: {enabled: true, tools: [pg_prove], test_db: {hosts: [db]}}",
		"db: {enabled: true, test_db: {url_env: 'a b'}}",
		"db: {enabled: true, tools: [supabase], test_db: [x]}",
	} {
		// the manifest loader refuses it; the doctor must not hide it either
		m := &manifest.Manifest{Packs: packsOf(t, block)}
		in := &packs.Input{Root: root, Manifest: m, Config: packs.Merge(m.Packs, nil), Budget: time.Now().Add(time.Minute)}
		in.SetWorkspace("acme", root)
		fs := Doctor(in.ForPack("db"))
		if _, ok := find(fs, "db-test-db-unset"); ok {
			t.Errorf("%s: reported as not set: %q", block, codes(fs))
		}
		f, ok := find(fs, "db-test-db-invalid")
		if !ok || f.Level != "error" || !strings.Contains(f.Msg, "invalid") {
			t.Errorf("%s: want db-test-db-invalid: %+v (%q)", block, f, codes(fs))
		}
	}
}
