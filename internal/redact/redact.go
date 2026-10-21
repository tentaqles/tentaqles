// Package redact masks personal and financial data (CPF, CNPJ, e-mail, phone,
// card numbers, IBAN) and secrets in text, by data class. Detection is
// heuristic but validated where a checksum exists (CPF and CNPJ check digits,
// Luhn for cards, mod-97 for IBAN), so plain numbers of the same length are
// left alone. Values are never logged; Count reports names only. An unknown
// class name or an empty class list fails closed: it is treated as "all",
// never as no masking.
package redact

import (
	"bytes"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/tentaqles/tentaqles/internal/secrets"
)

type detector struct {
	name  string
	re    *regexp.Regexp
	valid func(string) bool
}

var (
	cpfRe   = regexp.MustCompile(`\b\d{3}\.?\d{3}\.?\d{3}-?\d{2}\b`)
	cnpjRe  = regexp.MustCompile(`\b\d{2}\.?\d{3}\.?\d{3}/?\d{4}-?\d{2}\b`)
	emailRe = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	phoneRe = regexp.MustCompile(`\+\d{8,15}\b|\+\d{1,3}[\s.-]?\(?\d{2,3}\)?[\s.-]?\d{4,5}[\s.-]?\d{4}\b|\(\d{2,3}\)[\s.-]?\d{4,5}[\s.-]?\d{4}\b|\b\d{2,3}[\s.-]\d{4,5}[\s.-]?\d{4}\b|\b\d{2,3}[\s.-]?\d{4,5}[\s.-]\d{4}\b`)
	cardRe  = regexp.MustCompile(`\b[3-6](?:\d[ -]?){12,18}\b`)
	ibanRe  = regexp.MustCompile(`\b[A-Z]{2}\d{2}(?:[ ]?[A-Z0-9]{4}){2,7}(?:[ ]?[A-Z0-9]{1,4})?\b`)
)

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteByte(byte(r))
		}
	}
	return b.String()
}

func allSame(d string) bool {
	for i := 1; i < len(d); i++ {
		if d[i] != d[0] {
			return false
		}
	}
	return true
}

func validCPF(s string) bool {
	d := digits(s)
	if len(d) != 11 || allSame(d) {
		return false
	}
	for _, n := range []int{9, 10} {
		sum := 0
		for i := 0; i < n; i++ {
			sum += int(d[i]-'0') * (n + 1 - i)
		}
		r := (sum * 10) % 11
		if r == 10 {
			r = 0
		}
		if r != int(d[n]-'0') {
			return false
		}
	}
	return true
}

func validCNPJ(s string) bool {
	d := digits(s)
	if len(d) != 14 || allSame(d) {
		return false
	}
	w1 := []int{5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	w2 := []int{6, 5, 4, 3, 2, 9, 8, 7, 6, 5, 4, 3, 2}
	check := func(w []int, n int) bool {
		sum := 0
		for i, x := range w {
			sum += int(d[i]-'0') * x
		}
		r := sum % 11
		exp := 0
		if r >= 2 {
			exp = 11 - r
		}
		return exp == int(d[n]-'0')
	}
	return check(w1, 12) && check(w2, 13)
}

func luhn(s string) bool {
	d := digits(s)
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	sum, alt := 0, false
	for i := len(d) - 1; i >= 0; i-- {
		n := int(d[i] - '0')
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

func validIBAN(s string) bool {
	s = strings.ReplaceAll(strings.ToUpper(s), " ", "")
	if len(s) < 15 || len(s) > 34 {
		return false
	}
	r := s[4:] + s[:4]
	mod := 0
	for _, c := range r {
		var v int
		switch {
		case c >= '0' && c <= '9':
			v = int(c - '0')
		case c >= 'A' && c <= 'Z':
			v = int(c-'A') + 10
		default:
			return false
		}
		if v >= 10 {
			mod = (mod*100 + v) % 97
		} else {
			mod = (mod*10 + v) % 97
		}
	}
	return mod == 1
}

func validPhone(s string) bool {
	d := digits(s)
	return len(d) >= 10 && len(d) <= 15 && !allSame(d)
}

var byClass = map[string][]detector{
	"checksummed": {
		{"cpf", cpfRe, validCPF},
		{"cnpj", cnpjRe, validCNPJ},
		{"card", cardRe, luhn},
		{"iban", ibanRe, validIBAN},
	},
	"pii": {
		{"cpf", cpfRe, validCPF},
		{"email", emailRe, func(string) bool { return true }},
		{"phone", phoneRe, validPhone},
	},
	"financial": {
		{"card", cardRe, luhn},
		{"cnpj", cnpjRe, validCNPJ},
		{"iban", ibanRe, validIBAN},
	},
}

// Known lists the selectable classes.
// AllowMarker on a line leaves that line unmasked and uncounted (known-synthetic data).
const AllowMarker = "tq:allow-pii"

var Known = []string{"checksummed", "pii", "financial", "sensitive", "children", "secrets", "all"}

// ParseClasses parses a comma-separated list; "sensitive" and "children" use
// the pii detectors; "all" expands to pii, financial and secrets.
func ParseClasses(csv string) ([]string, error) {
	var out []string
	for _, c := range strings.Split(csv, ",") {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		ok := false
		for _, k := range Known {
			if k == c {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("unknown data class %q (known: %s)", c, strings.Join(Known, ", "))
		}
		if c == "all" {
			out = append(out, "checksummed", "pii", "financial", "secrets")
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no data class given")
	}
	return out, nil
}

func allDetectors() []detector {
	return append(append([]detector{}, byClass["pii"]...), byClass["financial"]...)
}

func detectors(classes []string) ([]detector, bool) {
	if len(classes) == 0 {
		// No class given: fail closed, mask everything.
		return allDetectors(), true
	}
	var ds []detector
	withSecrets := false
	seen := map[string]bool{}
	for _, c := range classes {
		switch c {
		case "all":
			return allDetectors(), true
		case "secrets":
			withSecrets = true
		case "sensitive", "children":
			c = "pii"
		case "pii", "financial", "checksummed":
		default:
			// Unknown class: fail closed, mask everything.
			return allDetectors(), true
		}
		for _, d := range byClass[c] {
			if !seen[d.name] {
				seen[d.name] = true
				ds = append(ds, d)
			}
		}
	}
	return ds, withSecrets
}

// Redact replaces every validated match with [REDACTED:<detector>].
func Redact(text string, classes []string) string {
	if !strings.Contains(text, AllowMarker) {
		return redactText(text, classes)
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if !strings.Contains(l, AllowMarker) {
			lines[i] = redactText(l, classes)
		}
	}
	return strings.Join(lines, "\n")
}

func redactText(text string, classes []string) string {
	ds, withSecrets := detectors(classes)
	for _, d := range ds {
		text = d.re.ReplaceAllStringFunc(text, func(m string) string {
			if d.valid(m) {
				return "[REDACTED:" + d.name + "]"
			}
			return m
		})
	}
	if withSecrets {
		text = secrets.Redact(text)
	}
	return text
}

// Count reports findings per detector without the values.
func Count(text string, classes []string) map[string]int {
	if !strings.Contains(text, AllowMarker) {
		return countText(text, classes)
	}
	out := map[string]int{}
	for _, l := range strings.Split(text, "\n") {
		if strings.Contains(l, AllowMarker) {
			continue
		}
		for k, v := range countText(l, classes) {
			out[k] += v
		}
	}
	return out
}

func countText(text string, classes []string) map[string]int {
	out := map[string]int{}
	ds, withSecrets := detectors(classes)
	for _, d := range ds {
		for _, m := range d.re.FindAllString(text, -1) {
			if d.valid(m) {
				out[d.name]++
			}
		}
	}
	if withSecrets {
		for _, f := range secrets.Scan(text) {
			out[f.Pattern]++
		}
	}
	return out
}

// Writer masks a stream line by line; a partial last line is held until the
// next newline or Close, so a value split across writes is still caught.
type Writer struct {
	w       io.Writer
	classes []string
	buf     bytes.Buffer
}

func NewWriter(w io.Writer, classes []string) *Writer { return &Writer{w: w, classes: classes} }

func (m *Writer) Write(p []byte) (int, error) {
	m.buf.Write(p)
	for {
		i := bytes.IndexByte(m.buf.Bytes(), '\n')
		if i < 0 {
			return len(p), nil
		}
		line := m.buf.Next(i + 1)
		if _, err := io.WriteString(m.w, Redact(string(line), m.classes)); err != nil {
			return len(p), err
		}
	}
}

func (m *Writer) Close() error {
	if m.buf.Len() == 0 {
		return nil
	}
	_, err := io.WriteString(m.w, Redact(m.buf.String(), m.classes))
	m.buf.Reset()
	return err
}
