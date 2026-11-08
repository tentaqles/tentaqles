package manifest

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MarkerSchema is the schema of the name-only marker that stays at
// <root>/.tentaqles.yaml once the manifest itself lives under
// ~/.tentaqles/identities/<name>/manifest.yaml. The marker names
// the workspace and nothing else; the guard never reads anything else from it.
const MarkerSchema = "tentaqles-marker-v1"

// ProtectedFileName is the manifest's file name under identities/<name>/.
const ProtectedFileName = "manifest.yaml"

// Kind tells what a <root>/.tentaqles.yaml file is.
type Kind int

const (
	// KindUnknown is any file that is not a marker.
	KindUnknown Kind = iota
	// KindMarker is the name-only marker.
	KindMarker
)

// Marker is the root marker: the schema and the workspace name.
type Marker struct {
	Schema string `yaml:"schema"`
	Client string `yaml:"client"`
}

// ErrMarkerClient is returned by Peek when a marker names no client.
var ErrMarkerClient = errors.New("marker has no 'client'")

// MarkerBytes renders the marker file for workspace ws: the "names only"
// comment, the schema and the client. The name goes through the YAML encoder
// so a folder name with a special character still reads back as itself.
func MarkerBytes(ws string) []byte {
	body, err := yaml.Marshal(Marker{Schema: MarkerSchema, Client: ws})
	if err != nil {
		// A string field cannot fail to encode; keep the function total.
		body = []byte("schema: " + MarkerSchema + "\nclient: " + ws + "\n")
	}
	return append([]byte("# tentaqles workspace marker — names only, never secrets; the manifest lives under ~/.tentaqles/identities/"+ws+"/"+ProtectedFileName+"\n"), body...)
}

// Peek reads only the top-level schema and client of the file at path and tells whether it is a
// marker. It validates nothing else: a marker's client is checked by the caller against the folder
// name.
func Peek(path string) (Kind, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return KindUnknown, "", err
	}
	var m Marker
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return KindUnknown, "", fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Schema != MarkerSchema {
		return KindUnknown, m.Client, nil
	}
	if m.Client == "" {
		return KindMarker, "", fmt.Errorf("%s: %w", path, ErrMarkerClient)
	}
	return KindMarker, m.Client, nil
}
