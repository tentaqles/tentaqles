package decide

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tentaqles/tentaqles/internal/dotenv"
)

// Cache stores raw Jev responses on disk, one file per request hash. The
// files hold answers only (probabilities), never the state.
type Cache struct {
	Dir string
	TTL time.Duration
}

// NewCache returns a cache in dir with a 7-day TTL.
func NewCache(dir string) *Cache { return &Cache{Dir: dir, TTL: 7 * 24 * time.Hour} }

func (c *Cache) get(key string) *Response {
	if c == nil {
		return nil
	}
	p := filepath.Join(c.Dir, key+".json")
	fi, err := os.Stat(p)
	if err != nil || (c.TTL > 0 && time.Since(fi.ModTime()) > c.TTL) {
		return nil
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var r Response
	if json.Unmarshal(raw, &r) != nil || r.validate() != nil {
		return nil
	}
	return &r
}

func (c *Cache) put(key string, raw []byte) {
	if c == nil {
		return
	}
	if os.MkdirAll(c.Dir, 0o700) != nil {
		return
	}
	tmp := filepath.Join(c.Dir, key+".tmp")
	if os.WriteFile(tmp, raw, 0o600) == nil {
		_ = os.Rename(tmp, filepath.Join(c.Dir, key+".json"))
	}
}

// Log appends one JSON line per call. It records what a call cost and how
// it went — never the state or the question text.
type Log struct {
	Path string
	mu   sync.Mutex
}

// Entry is one log line.
type Entry struct {
	Time      time.Time `json:"ts"`
	Workspace string    `json:"ws,omitempty"`
	Purpose   string    `json:"purpose,omitempty"`
	Model     string    `json:"model"`
	Questions int       `json:"questions"`
	MS        int64     `json:"ms"`
	Cached    bool      `json:"cached,omitempty"`
	InTokens  int       `json:"input_tokens,omitempty"`
	CostUSD   float64   `json:"cost_usd,omitempty"`
	Error     string    `json:"error,omitempty"`
}

func (l *Log) write(c *Client, start time.Time, r *Response, nq int, err error) {
	if l == nil {
		return
	}
	e := Entry{Time: start.UTC(), Workspace: c.Workspace, Purpose: c.Purpose, Model: c.Model, Questions: nq, MS: time.Since(start).Milliseconds()}
	if r != nil {
		e.Cached = r.Cached
		if !r.Cached {
			e.InTokens = r.Usage.InputTokens
			e.CostUSD = CostUSD(r.Usage.InputTokens)
		}
	}
	if err != nil {
		e.Error = err.Error()
	}
	line, mErr := json.Marshal(e)
	if mErr != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if os.MkdirAll(filepath.Dir(l.Path), 0o700) != nil {
		return
	}
	f, oErr := os.OpenFile(l.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if oErr != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// KeyName is the environment variable holding the TypeSafe key.
const KeyName = "TYPESAFE_API_KEY"

// ResolveKey finds the TypeSafe key: the environment first, then each
// dotenv file in order. The value is returned, never printed or logged.
func ResolveKey(envFiles ...string) string {
	if v := strings.TrimSpace(os.Getenv(KeyName)); v != "" {
		return v
	}
	for _, f := range envFiles {
		if f == "" {
			continue
		}
		if v := dotenv.Lookup(f, KeyName); v != "" {
			return v
		}
	}
	return ""
}
