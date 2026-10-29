package scan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Cell is one notebook cell.
type Cell struct {
	Index    int    // 0-based position in the notebook
	Kind     string // code | markdown | raw | parameters
	Language string // the cell's language when the format records it ("python", "sparksql", ...)
	Source   string
	// Line is the 1-based line in the file where the cell's source starts
	// (the cell object's line for JSON formats).
	Line int
	// LinesExact: source line i of the cell is file line Line+i (Fabric).
	// For the JSON formats every source line maps to the cell's Line.
	LinesExact bool
	Outputs    int // number of stored outputs (JSON formats)
	// OutputText is the text of the stored outputs (stream text, text/plain
	// and text/html data), for output-leak checks. Never copy it into a finding.
	OutputText string
}

// Notebook formats.
const (
	FormatIPynb   = "ipynb"
	FormatFabric  = "fabric"
	FormatSynapse = "synapse"
)

// DetectNotebook tells the notebook format of a file from its name and
// content, or "" when it is not a notebook.
func DetectNotebook(name string, data []byte) string {
	base := path.Base(strings.ReplaceAll(name, `\`, "/"))
	switch {
	case strings.HasSuffix(strings.ToLower(base), ".ipynb"):
		return FormatIPynb
	case base == "notebook-content.py" || bytes.HasPrefix(bytes.TrimSpace(data), []byte("# Fabric notebook source")):
		return FormatFabric
	case strings.HasSuffix(strings.ToLower(base), ".json") && bytes.Contains(data, []byte(`"properties"`)) && bytes.Contains(data, []byte(`"cells"`)) && bytes.Contains(data, []byte(`"nbformat"`)):
		return FormatSynapse
	}
	return ""
}

// NotebookCells extracts the cells of a .ipynb, a Fabric notebook-content.py
// or a Synapse notebook JSON (the format is detected from name and data).
func NotebookCells(name string, data []byte) ([]Cell, error) {
	switch DetectNotebook(name, data) {
	case FormatIPynb:
		return jsonCells(data, []string{"cells"})
	case FormatFabric:
		return FabricCells(data), nil
	case FormatSynapse:
		return jsonCells(data, []string{"properties", "cells"})
	}
	return nil, fmt.Errorf("%s: not a notebook", name)
}

type jsonCell struct {
	CellType string          `json:"cell_type"`
	Source   json.RawMessage `json:"source"`
	Outputs  []struct {
		Text json.RawMessage            `json:"text"`
		Data map[string]json.RawMessage `json:"data"`
	} `json:"outputs"`
	Metadata struct {
		Language string `json:"language"`
		Kernel   struct {
			Language string `json:"language"`
		} `json:"kernelspec"`
		Microsoft struct {
			Language string `json:"language"`
		} `json:"microsoft"`
	} `json:"metadata"`
}

// textOf reads a notebook text field: a string or a list of strings.
func textOf(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []string
	if json.Unmarshal(raw, &parts) == nil {
		return strings.Join(parts, "")
	}
	return ""
}

// jsonCells walks to the cells array at path and decodes each element,
// recording the line it starts on.
func jsonCells(data []byte, at []string) ([]Cell, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := seek(dec, at); err != nil {
		return nil, err
	}
	var out []Cell
	for i := 0; dec.More(); i++ {
		off := dec.InputOffset()
		var jc jsonCell
		if err := dec.Decode(&jc); err != nil {
			return out, err
		}
		line := 1 + bytes.Count(data[:off], []byte("\n"))
		// The offset sits before the separator; skip to the object itself.
		if j := bytes.IndexByte(data[off:], '{'); j >= 0 {
			line = 1 + bytes.Count(data[:int(off)+j], []byte("\n"))
		}
		c := Cell{Index: i, Kind: jc.CellType, Source: textOf(jc.Source), Line: line, Outputs: len(jc.Outputs)}
		c.Language = firstNonEmpty(jc.Metadata.Microsoft.Language, jc.Metadata.Language, jc.Metadata.Kernel.Language)
		var ot []string
		for _, o := range jc.Outputs {
			if t := textOf(o.Text); t != "" {
				ot = append(ot, t)
			}
			for _, k := range []string{"text/plain", "text/html", "text/markdown", "application/json"} {
				if v, ok := o.Data[k]; ok {
					if t := textOf(v); t != "" {
						ot = append(ot, t)
					} else {
						ot = append(ot, string(v))
					}
				}
			}
		}
		c.OutputText = strings.Join(ot, "\n")
		out = append(out, c)
	}
	return out, nil
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// seek advances dec into the array at the key path (objects only).
func seek(dec *json.Decoder, at []string) error {
	for depth, key := range at {
		t, err := dec.Token()
		if err != nil {
			return err
		}
		if d, ok := t.(json.Delim); !ok || d != '{' {
			return errors.New("notebook: expected an object")
		}
		found := false
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return err
			}
			if k, _ := kt.(string); k == key {
				found = true
				break
			}
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return err
			}
		}
		if !found {
			return fmt.Errorf("notebook: no %q at depth %d", key, depth)
		}
	}
	t, err := dec.Token()
	if err != nil {
		return err
	}
	if d, ok := t.(json.Delim); !ok || d != '[' {
		return errors.New("notebook: cells is not an array")
	}
	return nil
}

var fabricMarker = regexp.MustCompile(`^# (CELL|MARKDOWN|METADATA|PARAMETERS CELL|PARAMETERS) \*{5,}\s*$`)

// FabricCells splits a Fabric notebook-content.py on its cell markers
// (`# CELL ****`, `# MARKDOWN ****`, `# PARAMETERS CELL ****`; the
// `# METADATA ****` blocks are skipped). Markdown lines lose their "# "
// prefix. Language comes from the metadata block's "language" when present.
func FabricCells(data []byte) []Cell {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	var out []Cell
	var cur *Cell
	var body []string
	kind := ""
	closeCell := func() {
		if cur != nil {
			for len(body) > 0 && strings.TrimSpace(body[0]) == "" {
				body = body[1:]
				cur.Line++
			}
			for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
				body = body[:len(body)-1]
			}
			cur.Source = strings.Join(body, "\n")
			out = append(out, *cur)
		}
		cur, body = nil, nil
	}
	langRe := regexp.MustCompile(`"language"\s*:\s*"([^"]+)"`)
	for i, l := range lines {
		if m := fabricMarker.FindStringSubmatch(l); m != nil {
			closeCell()
			kind = m[1]
			switch kind {
			case "CELL":
				cur = &Cell{Index: len(out), Kind: "code", Line: i + 2}
			case "MARKDOWN":
				cur = &Cell{Index: len(out), Kind: "markdown", Line: i + 2}
			case "PARAMETERS CELL", "PARAMETERS":
				cur = &Cell{Index: len(out), Kind: "parameters", Line: i + 2}
			}
			if cur != nil {
				cur.LinesExact = true
			}
			continue
		}
		if kind == "METADATA" {
			if m := langRe.FindStringSubmatch(l); m != nil && len(out) > 0 && out[len(out)-1].Language == "" {
				out[len(out)-1].Language = m[1]
			}
			continue
		}
		if cur == nil {
			continue // the "# Fabric notebook source" header
		}
		if cur.Kind == "markdown" {
			l = strings.TrimPrefix(strings.TrimPrefix(l, "#"), " ")
		}
		body = append(body, l)
	}
	closeCell()
	return out
}

// CodeSource joins the code (and parameters) cells into one Python source
// for PythonLines, and maps each joined line back to the file line.
// Lines starting with % or ! (magics, shell) are blanked.
func CodeSource(cells []Cell) (src []byte, fileLine func(int) int) {
	var b strings.Builder
	var lineMap []int
	for _, c := range cells {
		if c.Kind != "code" && c.Kind != "parameters" {
			continue
		}
		if l := strings.ToLower(c.Language); l != "" && l != "python" && l != "pyspark" && l != "python3" {
			continue
		}
		for i, l := range strings.Split(c.Source, "\n") {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "%") || strings.HasPrefix(t, "!") {
				l = ""
			}
			b.WriteString(l)
			b.WriteByte('\n')
			fl := c.Line
			if c.LinesExact {
				fl = c.Line + i
			}
			lineMap = append(lineMap, fl)
		}
		b.WriteByte('\n') // cells never join into one logical line
		lineMap = append(lineMap, c.Line)
	}
	return []byte(b.String()), func(n int) int {
		if n >= 1 && n <= len(lineMap) {
			return lineMap[n-1]
		}
		return 0
	}
}
