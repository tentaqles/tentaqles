package gate

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type vectorFile struct {
	KeyHex  string `json:"key_hex"`
	Vectors []struct {
		Action, Slug, Actor, Message, MAC string
	} `json:"vectors"`
}

func TestTokenMatchesPythonVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "hub_token_vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var vf vectorFile
	if err := json.Unmarshal(raw, &vf); err != nil {
		t.Fatal(err)
	}
	key, _ := hex.DecodeString(vf.KeyHex)
	if len(vf.Vectors) < 7 {
		t.Fatalf("fixture has %d vectors", len(vf.Vectors))
	}
	for _, v := range vf.Vectors {
		if got := Message(v.Action, v.Slug, v.Actor); got != v.Message {
			t.Fatalf("message for %q:\n got %s\nwant %s", v.Actor, got, v.Message)
		}
		if got := MintToken(key, v.Action, v.Slug, v.Actor); got != v.MAC {
			t.Fatalf("token for %q: got %s want %s", v.Actor, got, v.MAC)
		}
	}
}

func TestActorIsHubPrefixedOSUser(t *testing.T) {
	a := Actor()
	if !strings.HasPrefix(a, "hub:") || len(a) <= 4 || strings.ContainsAny(a[4:], `\/`) {
		t.Fatalf("actor %q", a)
	}
}

func TestKeyPathIsUnderTQHome(t *testing.T) {
	t.Setenv("TQ_HOME", filepath.Join(t.TempDir(), "tq"))
	if KeyPath() != filepath.Join(os.Getenv("TQ_HOME"), "build-gate", "hmac.key") {
		t.Fatal(KeyPath())
	}
}
