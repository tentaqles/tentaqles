package connect

import (
	"encoding/json"
	"errors"
)

// A minimal JSON scanner that finds an object's members by byte span, so Add inserts one
// member without re-encoding the file. It runs only on input json.Valid accepted. (The same
// scanner lives in internal/mcpscope, which this ticket does not touch.)

type member struct {
	key                  string
	keyStart, valueStart int
	valueEnd             int // one past the value
}

var errJSON = errors.New("not valid JSON")

func skipWS(b []byte, i int) int {
	for i < len(b) && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	return i
}

// scanString returns one past the closing quote of the string starting at b[i] == '"'.
func scanString(b []byte, i int) (int, error) {
	for j := i + 1; j < len(b); j++ {
		switch b[j] {
		case '\\':
			j++
		case '"':
			return j + 1, nil
		}
	}
	return 0, errJSON
}

// scanValue returns one past the value starting at b[i] (no leading space).
func scanValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errJSON
	}
	switch b[i] {
	case '"':
		return scanString(b, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(b); j++ {
			switch b[j] {
			case '"':
				e, err := scanString(b, j)
				if err != nil {
					return 0, err
				}
				j = e - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, errJSON
	default:
		j := i
		for j < len(b) && b[j] != ',' && b[j] != '}' && b[j] != ']' && b[j] != ' ' && b[j] != '\n' && b[j] != '\r' && b[j] != '\t' {
			j++
		}
		if j == i {
			return 0, errJSON
		}
		return j, nil
	}
}

// members lists the members of the object starting at b[i] == '{', and the index of its '}'.
func members(b []byte, i int) ([]member, int, error) {
	if i >= len(b) || b[i] != '{' {
		return nil, 0, errJSON
	}
	var out []member
	j := skipWS(b, i+1)
	if j < len(b) && b[j] == '}' {
		return nil, j, nil
	}
	for {
		j = skipWS(b, j)
		if j >= len(b) || b[j] != '"' {
			return nil, 0, errJSON
		}
		ke, err := scanString(b, j)
		if err != nil {
			return nil, 0, err
		}
		var key string
		if err := json.Unmarshal(b[j:ke], &key); err != nil {
			return nil, 0, err
		}
		c := skipWS(b, ke)
		if c >= len(b) || b[c] != ':' {
			return nil, 0, errJSON
		}
		vs := skipWS(b, c+1)
		ve, err := scanValue(b, vs)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, member{key: key, keyStart: j, valueStart: vs, valueEnd: ve})
		j = skipWS(b, ve)
		if j >= len(b) {
			return nil, 0, errJSON
		}
		if b[j] == '}' {
			return out, j, nil
		}
		if b[j] != ',' {
			return nil, 0, errJSON
		}
		j++
	}
}
