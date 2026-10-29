package readers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// doc is a decoded JSON document plus the line of every object and array
// up to a depth, keyed by JSON pointer.
type doc struct {
	root  any
	lines map[string]int // nil when the document is on one line
}

// decodeJSON decodes raw (numbers as json.Number) and, for a multi-line
// document, indexes the lines of the objects and arrays down to maxDepth.
func decodeJSON(raw []byte, maxDepth int) (*doc, error) {
	raw = trimBOM(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("parse JSON: trailing data after the document")
	}
	d := &doc{root: root}
	if bytes.IndexByte(bytes.TrimSpace(raw), '\n') >= 0 {
		lines, err := indexLines(raw, maxDepth)
		if err != nil {
			return nil, err
		}
		d.lines = lines
	}
	return d, nil
}

// line is the 1-based line of the value at ptr: 1 for a one-line document
// or an unindexed pointer's nearest indexed ancestor.
func (d *doc) line(ptr string) int {
	if d.lines == nil {
		return 1
	}
	for p := ptr; ; {
		if l, ok := d.lines[p]; ok {
			return l
		}
		i := strings.LastIndexByte(p, '/')
		if i < 0 {
			return 1
		}
		p = p[:i]
	}
}

type lineWalker struct {
	raw   []byte
	dec   *json.Decoder
	nl    []int
	max   int
	lines map[string]int
}

func indexLines(raw []byte, maxDepth int) (map[string]int, error) {
	w := &lineWalker{raw: raw, dec: json.NewDecoder(bytes.NewReader(raw)), max: maxDepth, lines: map[string]int{}}
	w.dec.UseNumber()
	for i, b := range raw {
		if b == '\n' {
			w.nl = append(w.nl, i)
		}
	}
	if err := w.value("", 0); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return w.lines, nil
}

// start is the offset where the next token begins: InputOffset is the end
// of the previous token, followed by blanks, ':' or ','.
func (w *lineWalker) start() int {
	i := int(w.dec.InputOffset())
	for i < len(w.raw) && strings.IndexByte(" \t\r\n:,", w.raw[i]) >= 0 {
		i++
	}
	return i
}

func (w *lineWalker) lineAt(off int) int {
	return sort.SearchInts(w.nl, off) + 1
}

func (w *lineWalker) value(ptr string, depth int) error {
	off := w.start()
	tok, err := w.dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if depth <= w.max {
		w.lines[ptr] = w.lineAt(off)
	}
	switch d {
	case '{':
		for w.dec.More() {
			kt, err := w.dec.Token()
			if err != nil {
				return err
			}
			k, _ := kt.(string)
			if err := w.value(ptr+"/"+escPointer(k), depth+1); err != nil {
				return err
			}
		}
	case '[':
		for i := 0; w.dec.More(); i++ {
			if err := w.value(ptr+"/"+strconv.Itoa(i), depth+1); err != nil {
				return err
			}
		}
	}
	_, err = w.dec.Token() // the closing delimiter
	return err
}

// escPointer escapes one RFC 6901 reference token.
func escPointer(s string) string {
	if !strings.ContainsAny(s, "~/") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}

// encodeJSON marshals v compactly, without HTML escaping, with a newline.
func encodeJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}
