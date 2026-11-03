package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// Test doubles for tq decide's Jev client.

// fakeJev is a TypeSafe stand-in on 127.0.0.1 (an allowed base_url).
func fakeJev(t *testing.T, answer func(questions map[string]any) map[string]any, status int) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": answer(body.Questions)})
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func noulAll(p float64) func(map[string]any) map[string]any {
	return func(qs map[string]any) map[string]any {
		out := map[string]any{}
		for id := range qs {
			out[id] = map[string]any{"noul": p}
		}
		return out
	}
}

func jevWorkspace(t *testing.T, url, mode string) string {
	t.Helper()
	t.Setenv("TYPESAFE_API_KEY", "test-key")
	return setupTrustedWorkspaceWithManifest(t, "acme", "decision:\n  backend: typesafe\n  mode: "+mode+"\n  base_url: "+url+"/v1/systemone\n")
}
