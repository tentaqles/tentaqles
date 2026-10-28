package web

import (
	"net/http"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

func TestSyncSettingsAPI(t *testing.T) {
	e := newEnv(t, "acme")
	ts := e.server(Options{LocalKey: "lk"})
	code, m, raw := apiDo(t, ts.URL, "GET", "/api/settings?ws=acme", nil, nil)
	if code != 200 {
		t.Fatalf("%d %s", code, raw)
	}
	sy, _ := m["sync"].(map[string]any)
	pol, _ := sy["policy"].(map[string]any)
	if sy["interval"] != float64(300) || pol["move_on_start"] != "auto" || pol["create_card"] != "ask" || len(pol) != 6 {
		t.Fatalf("GET sync: %s", raw)
	}
	code, m, raw = apiDo(t, ts.URL, "POST", "/api/settings?ws=acme",
		map[string]any{"sync": map[string]any{"interval": 120, "enabled": true, "policy": map[string]string{"close_item": "off"}}}, nil)
	sy, _ = m["sync"].(map[string]any)
	pol, _ = sy["policy"].(map[string]any)
	if code != 200 || sy["interval"] != float64(120) || sy["enabled"] != true || pol["close_item"] != "off" {
		t.Fatalf("POST sync: %d %s", code, raw)
	}
	for _, bad := range []map[string]any{
		{"interval": 59},
		{"policy": map[string]string{"close_item": "never"}},
		{"policy": map[string]string{"close_items": "off"}},
	} {
		if code, m, _ := apiDo(t, ts.URL, "POST", "/api/settings?ws=acme", map[string]any{"sync": bad}, nil); code != http.StatusBadRequest {
			t.Fatalf("%v: %d %v", bad, code, m)
		}
	}
	if code, m, _ := apiDo(t, ts.URL, "POST", "/api/settings?ws=acme", map[string]any{"sync": map[string]any{"interval": 600}},
		map[string]string{HeaderAgent: "1"}); code != http.StatusForbidden || m["code"] != "human_only" {
		t.Fatalf("agent: %d %v", code, m)
	}
	st := e.svcs["acme"].Store()
	if st.SyncInterval() != 120 || st.SyncPolicyFor(store.OutboxCloseItem) != store.PolicyOff {
		t.Fatal("refused updates changed the stored values")
	}
}
