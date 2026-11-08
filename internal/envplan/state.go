package envplan

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

// The __TQ_STATE encoding, after direnv's gzenv (zlib, then base64 URL-safe): a prefix marks
// it, so a state written by an older tq (plain base64 JSON) still decodes.

const statePrefix = "z1."

// maxState bounds what a decoded state may inflate to: the variable comes from the shell, and
// a crafted value must not make the prompt hook allocate without bound.
const maxState = 1 << 20

func encodeState(s State) string {
	if orig, ok := trackedPath(s); ok && s.PathSet != "" && s.Sep != "" {
		if drop, mask, ok := compactPath(orig, s.PathSet, s.Sep); ok {
			s.PathDrop, s.PathMask, s.PathSet, s.PathCompact = drop, mask, "", true
		}
	}
	b, _ := json.Marshal(s)
	var z bytes.Buffer
	w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return statePrefix + base64.RawURLEncoding.EncodeToString(z.Bytes())
}

func decodeState(enc string) State {
	s, _ := decodeStateOK(enc)
	return s
}

// DecodeStateOK is DecodeState that also reports whether enc was a state tq wrote: false
// for a truncated, non-base64, garbage or oversized value, and for "".
func DecodeStateOK(enc string) (State, bool) { return decodeStateOK(enc) }

func decodeStateOK(enc string) (State, bool) {
	var s State
	ok := false
	enc = strings.TrimSpace(enc)
	if rest, z1 := strings.CutPrefix(enc, statePrefix); z1 {
		if z, err := base64.RawURLEncoding.DecodeString(rest); err == nil {
			if r, err := zlib.NewReader(bytes.NewReader(z)); err == nil {
				b, err := io.ReadAll(io.LimitReader(r, maxState+1))
				_ = r.Close()
				if err == nil && len(b) <= maxState {
					ok = json.Unmarshal(b, &s) == nil
				}
			}
		}
	} else if b, err := base64.RawURLEncoding.DecodeString(enc); err == nil && enc != "" {
		ok = json.Unmarshal(b, &s) == nil
	}
	if !ok {
		s = State{}
	}
	if s.Prev == nil {
		s.Prev = map[string]*string{}
	}
	if s.PathCompact && s.PathSet == "" && s.Sep != "" {
		if orig, ok := trackedPath(s); ok {
			s.PathSet = expandPath(orig, s.PathDrop, s.PathMask, s.Sep)
		}
	}
	s.PathDrop, s.PathMask, s.PathCompact = nil, "", false
	return s, ok
}

// compactPath spells set as orig without the entries at drop, with mask first when not "".
// ok is false when set is not of that shape; the state then carries set as it is.
func compactPath(orig, set, sep string) ([]int, string, bool) {
	o := strings.Split(orig, sep)
	s := strings.Split(set, sep)
	try := func(mask string, rest []string) ([]int, bool) {
		var drop []int
		j := 0
		for i := range o {
			if j < len(rest) && o[i] == rest[j] {
				j++
				continue
			}
			drop = append(drop, i)
		}
		if j != len(rest) {
			return nil, false
		}
		return drop, expandPath(orig, drop, mask, sep) == set
	}
	if drop, ok := try("", s); ok {
		return drop, "", true
	}
	if len(s) > 0 && s[0] != "" {
		if drop, ok := try(s[0], s[1:]); ok {
			return drop, s[0], true
		}
	}
	// The shims folder and the mask folder, both first: spelled as one prefix.
	if len(s) > 1 && s[0] != "" && s[1] != "" {
		if drop, ok := try(s[0]+sep+s[1], s[2:]); ok {
			return drop, s[0] + sep + s[1], true
		}
	}
	return nil, "", false
}

func expandPath(orig string, drop []int, mask, sep string) string {
	skip := map[int]bool{}
	for _, i := range drop {
		skip[i] = true
	}
	var out []string
	if mask != "" {
		out = append(out, mask)
	}
	for i, e := range strings.Split(orig, sep) {
		if !skip[i] {
			out = append(out, e)
		}
	}
	return strings.Join(out, sep)
}
