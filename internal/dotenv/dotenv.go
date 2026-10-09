// Package dotenv reads .env files and masks their values in output, so an
// agent can use a .env without its secrets ever reaching the transcript:
// `tq dotenv keys` lists names only, and `tq dotenv run` loads the file into
// a child process and masks every loaded value in what it prints.
package dotenv

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Entry is one KEY=VALUE line.
type Entry struct {
	Key   string
	Value string
	Line  int
}

// Parse reads a dotenv file: blank lines and # comments are skipped,
// `export ` prefixes are accepted, matching quotes are stripped, and an
// unquoted value ends at " #".
func Parse(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(strings.TrimPrefix(sc.Text(), string(rune(0xFEFF))))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		k, v, ok := strings.Cut(line, "=")
		k = strings.TrimSpace(k)
		if !ok || !ValidKey(k) {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		} else if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		out = append(out, Entry{Key: k, Value: v, Line: n})
	}
	return out, sc.Err()
}

// keyRe is what an environment variable name looks like. Anything else on
// the left of "=" (a base64 line of a private key ends in "=") is not a key
// and must never be echoed back as one.
var keyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]{0,127}$`)

// ValidKey reports whether k looks like an environment variable name. A
// long token mixing upper case, lower case and digits with no underscore is
// rejected too: that is the shape of key material (a base64 line of a PEM
// key pasted into a .env), not of a variable name.
func ValidKey(k string) bool {
	if !keyRe.MatchString(k) {
		return false
	}
	if len(k) >= 16 && !strings.Contains(k, "_") {
		var up, low, dig bool
		for _, r := range k {
			switch {
			case r >= 'A' && r <= 'Z':
				up = true
			case r >= 'a' && r <= 'z':
				low = true
			case r >= '0' && r <= '9':
				dig = true
			}
		}
		if up && low && dig {
			return false
		}
	}
	return true
}

// IsEnvFile reports whether path names a dotenv file: .env, .env.<x>, or
// <x>.env. Commands that read "any" file are limited to these so they cannot
// be pointed at keys, credentials or other secret stores.
func IsEnvFile(path string) bool {
	b := strings.ToLower(filepath.Base(path))
	return b == ".env" || strings.HasPrefix(b, ".env.") || strings.HasSuffix(b, ".env")
}

// Lookup returns key's value from a dotenv file, or "".
func Lookup(path, key string) string {
	es, err := Parse(path)
	if err != nil {
		return ""
	}
	v := ""
	for _, e := range es {
		if e.Key == key {
			v = e.Value // last one wins, like most loaders
		}
	}
	return v
}

// MinMaskLen is the shortest value masked: shorter ones ("1", "true",
// "dev") are not secrets and masking them would shred normal output.
const MinMaskLen = 6

// Masker writes through to W, replacing every secret value with
// [REDACTED:NAME]. It buffers up to the next newline so a value split
// across writes is still caught; Close flushes the rest.
type Masker struct {
	W       io.Writer
	mu      sync.Mutex
	buf     []byte
	olds    [][]byte
	news    [][]byte
	maxLine int
}

// NewMasker masks values (name -> value) on w.
func NewMasker(w io.Writer, values map[string]string) *Masker {
	type kv struct{ k, v string }
	var pairs []kv
	for k, v := range values {
		if len(v) >= MinMaskLen {
			pairs = append(pairs, kv{k, v})
		}
	}
	// Longest first, so a value containing another is masked whole.
	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i].v) != len(pairs[j].v) {
			return len(pairs[i].v) > len(pairs[j].v)
		}
		return pairs[i].k < pairs[j].k
	})
	m := &Masker{W: w, maxLine: 64 << 10}
	for _, p := range pairs {
		m.olds = append(m.olds, []byte(p.v))
		m.news = append(m.news, []byte("[REDACTED:"+p.k+"]"))
	}
	return m
}

func (m *Masker) mask(b []byte) []byte {
	for i := range m.olds {
		b = bytes.ReplaceAll(b, m.olds[i], m.news[i])
	}
	return b
}

// Write buffers p and writes out every complete line, masked.
func (m *Masker) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buf = append(m.buf, p...)
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		if _, err := m.W.Write(m.mask(m.buf[:i+1])); err != nil {
			return len(p), err
		}
		m.buf = m.buf[i+1:]
	}
	// A very long line without a newline is flushed in a chunk. Mask the
	// whole buffer first, then hold back the last maxLen bytes: a value that
	// is still incomplete must start inside that tail (it is at most maxLen
	// long), so no part of it is written before it can be masked. Masking
	// is idempotent, so the held tail is safely re-masked later.
	if len(m.buf) > m.maxLine {
		keep := 0
		for _, o := range m.olds {
			if len(o) > keep {
				keep = len(o)
			}
		}
		masked := m.mask(m.buf)
		cut := len(masked) - keep
		if _, err := m.W.Write(masked[:cut]); err != nil {
			return len(p), err
		}
		m.buf = append([]byte(nil), masked[cut:]...)
	}
	return len(p), nil
}

// Close flushes what is buffered.
func (m *Masker) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buf) == 0 {
		return nil
	}
	_, err := m.W.Write(m.mask(m.buf))
	m.buf = nil
	return err
}
