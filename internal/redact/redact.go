// Package redact masks personal and financial data (CPF, CNPJ, e-mail, phone,
// card numbers, IBAN) and secrets in text, by data class. Detection is
// heuristic but validated where a checksum exists (CPF and CNPJ check digits,
// Luhn for cards, mod-97 for IBAN), so plain numbers of the same length are
// left alone. Values are never logged; Count reports names only.
//
// Resolve is the one class-to-detector mapping, used by `tq redact` and by
// the hook's exit rules alike: the checksummed detectors (CPF, CNPJ, card,
// IBAN) always run, pii/sensitive/children add e-mail and phone, secrets adds
// the secret patterns, all runs everything. An unknown class or an empty list
// is an error; Redact and Count, which cannot return one, treat it as "all"
// (fail closed: never less masking).
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
	name string
	re   *regexp.Regexp
	// span returns the length of the validated value at the start of a
	// regexp match, 0 when there is none.
	span func(string) int
}

func whole(valid func(string) bool) func(string) int {
	return func(m string) int {
		if valid(m) {
			return len(m)
		}
		return 0
	}
}

// cardSpan accepts the whole match when it passes Luhn, otherwise the longest
// prefix of 13 to 19 digits that ends at a group boundary (a space or a dash)
// and passes Luhn: a card followed by its expiry or CVV ("4111 1111 1111
// 1111 123") greedily matches as one longer number that fails the check.
func cardSpan(m string) int {
	if luhn(m) {
		return len(strings.TrimRight(m, " -")) // the greedy match may end on a separator
	}
	best := 0
	n := 0
	for i := 0; i < len(m); i++ {
		c := m[i]
		if c >= '0' && c <= '9' {
			n++
			continue
		}
		if n >= 13 && n <= 19 && luhn(m[:i]) {
			best = i
		}
	}
	return best
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

// checksummedDetectors always run, before contactDetectors, so phone never
// masks part of a CNPJ or a card.
var (
	checksummedDetectors = []detector{
		{"cpf", cpfRe, whole(validCPF)},
		{"cnpj", cnpjRe, whole(validCNPJ)},
		{"card", cardRe, cardSpan},
		{"iban", ibanRe, whole(validIBAN)},
	}
	contactDetectors = []detector{
		{"email", emailRe, whole(func(string) bool { return true })},
		{"phone", phoneRe, whole(validPhone)},
	}
)

// AllowMarker on a line exempts it from the personal and financial detectors
// (known-synthetic data). Secrets on the line are still masked and counted.
const AllowMarker = "tq:allow-pii"

// Known lists the selectable classes ("public" and "internal" are manifest
// classes that add nothing beyond the checksummed detectors).
var Known = []string{"checksummed", "pii", "financial", "sensitive", "children", "secrets", "public", "internal", "all"}

// Set is a resolved class list: the detectors to run, in order, and whether
// the secret patterns run too.
type Set struct {
	detectors []detector
	secrets   bool
}

// Resolve maps data classes to detectors (see the package comment). It is
// the only place that does, so `tq redact` masks at least what the hook
// checks.
func Resolve(classes []string) (Set, error) {
	if len(classes) == 0 {
		return Set{}, fmt.Errorf("no data class given")
	}
	s := Set{detectors: checksummedDetectors}
	contact := false
	for _, c := range classes {
		switch strings.ToLower(strings.TrimSpace(c)) {
		case "all":
			contact, s.secrets = true, true
		case "pii", "sensitive", "children":
			contact = true
		case "secrets":
			s.secrets = true
		case "checksummed", "financial", "public", "internal":
		default:
			return Set{}, fmt.Errorf("unknown data class %q (known: %s)", c, strings.Join(Known, ", "))
		}
	}
	if contact {
		s.detectors = append(append([]detector{}, checksummedDetectors...), contactDetectors...)
	}
	return s, nil
}

// resolveOrAll is Resolve that fails closed for callers without an error
// path: an empty or unknown class list runs every detector.
func resolveOrAll(classes []string) Set {
	if s, err := Resolve(classes); err == nil {
		return s
	}
	s, _ := Resolve([]string{"all"})
	return s
}

// ParseClasses parses a comma-separated list and validates it with Resolve.
func ParseClasses(csv string) ([]string, error) {
	var out []string
	for _, c := range strings.Split(csv, ",") {
		if c = strings.ToLower(strings.TrimSpace(c)); c != "" {
			out = append(out, c)
		}
	}
	if _, err := Resolve(out); err != nil {
		return nil, err
	}
	return out, nil
}

// Redact replaces every validated match with [REDACTED:<detector>]. A line
// carrying AllowMarker keeps its personal and financial values but secrets on
// it are still masked.
func Redact(text string, classes []string) string {
	if !strings.Contains(text, AllowMarker) {
		return redactText(text, classes, false)
	}
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = redactText(l, classes, strings.Contains(l, AllowMarker))
	}
	return strings.Join(lines, "\n")
}

// mask replaces every validated value, detector by detector in order, and
// reports each one to hit. A masked value cannot be matched again by a later
// detector.
func mask(text string, s Set, hit func(name string)) string {
	for _, d := range s.detectors {
		d := d
		text = d.re.ReplaceAllStringFunc(text, func(m string) string {
			n := d.span(m)
			if n == 0 {
				return m
			}
			if hit != nil {
				hit(d.name)
			}
			return "[REDACTED:" + d.name + "]" + m[n:]
		})
	}
	return text
}

func redactText(text string, classes []string, skipPII bool) string {
	s := resolveOrAll(classes)
	if !skipPII {
		text = mask(text, s, nil)
	}
	if s.secrets {
		text = secrets.Redact(text)
	}
	return text
}

// Count reports findings per detector without the values. A line carrying
// AllowMarker is exempt from the personal and financial detectors only.
func Count(text string, classes []string) map[string]int {
	if !strings.Contains(text, AllowMarker) {
		return countText(text, classes, false)
	}
	out := map[string]int{}
	for _, l := range strings.Split(text, "\n") {
		for k, v := range countText(l, classes, strings.Contains(l, AllowMarker)) {
			out[k] += v
		}
	}
	return out
}

// CountAll is Count that ignores AllowMarker. Callers that must not honour
// the marker (published text, files outside test directories) use it.
func CountAll(text string, classes []string) map[string]int {
	return countText(text, classes, false)
}

func countText(text string, classes []string, skipPII bool) map[string]int {
	out := map[string]int{}
	s := resolveOrAll(classes)
	if !skipPII {
		mask(text, s, func(name string) { out[name]++ })
	}
	if s.secrets {
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
