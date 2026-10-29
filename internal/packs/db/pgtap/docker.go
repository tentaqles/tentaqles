package pgtap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// dockerContextNotLocal is the refusal of a remote Docker context; it never
// quotes the context or its endpoint.
const dockerContextNotLocal = "test_db is not local: the current Docker context names a Docker daemon on another machine; the tests run only against a local database (docker context use default, or a local context)"

// maxDockerFile bounds a Docker CLI config or context file read.
const maxDockerFile = 1 << 20

// dockerLocal reports whether the Docker daemon the child tools reach is on
// this machine. DOCKER_HOST, when set, wins over any context (the Docker
// CLI's precedence) and must be local. Otherwise every context that could
// be current must be local: DOCKER_CONTEXT, and the currentContext of the
// config under DOCKER_CONFIG and under ~/.docker (the child sees HOME but
// not DOCKER_CONFIG, so both are checked). A context tq cannot resolve, or
// a config it cannot parse, is not local.
func dockerLocal() (bool, string) {
	if h := os.Getenv("DOCKER_HOST"); h != "" {
		return DockerHostLocal(h), dockerNotLocal
	}
	if !dockerContextLocal() {
		return false, dockerContextNotLocal
	}
	return true, ""
}

func dockerConfigDirs() []string {
	var out []string
	if d := os.Getenv("DOCKER_CONFIG"); d != "" {
		out = append(out, d)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = append(out, filepath.Join(home, ".docker"))
	}
	return out
}

// contextDigest is the directory name the Docker CLI stores a context's
// metadata under: the hex SHA-256 of its name.
func contextDigest(name string) string {
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])
}

func readSmall(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, maxDockerFile))
}

func dockerContextLocal() bool {
	dirs := dockerConfigDirs()
	var names []string
	if v := os.Getenv("DOCKER_CONTEXT"); v != "" {
		names = append(names, v)
	}
	for _, d := range dirs {
		b, err := readSmall(filepath.Join(d, "config.json"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return false
		}
		var c struct {
			CurrentContext string `json:"currentContext"`
		}
		if json.Unmarshal(b, &c) != nil {
			return false
		}
		if c.CurrentContext != "" {
			names = append(names, c.CurrentContext)
		}
	}
	for _, n := range names {
		if n == "default" {
			continue
		}
		host, ok := contextHost(dirs, n)
		if !ok || !DockerHostLocal(host) {
			return false
		}
	}
	return true
}

// contextHost finds the docker endpoint host of context name in the first
// config directory that holds it.
func contextHost(dirs []string, name string) (string, bool) {
	for _, d := range dirs {
		b, err := readSmall(filepath.Join(d, "contexts", "meta", contextDigest(name), "meta.json"))
		if err != nil {
			continue
		}
		var m struct {
			Endpoints map[string]struct {
				Host string `json:"Host"`
			} `json:"Endpoints"`
		}
		if json.Unmarshal(b, &m) != nil {
			return "", false
		}
		ep, ok := m.Endpoints["docker"]
		if !ok {
			return "", false
		}
		return ep.Host, true
	}
	return "", false
}
