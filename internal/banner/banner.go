// Package banner prints the Tentaqles banner and its status line. The art and the colour live in
// one text asset, banner.txt, so they can be replaced without touching code.
package banner

import (
	_ "embed"
	"fmt"
	"io"
	"strings"
)

//go:embed banner.txt
var asset string

// Env is what decides how the banner prints: the environment and the OS.
type Env struct {
	Getenv func(string) string
	GOOS   string
}

// Status is the line under the art.
type Status struct {
	Version, Identity, Dir string
}

// Silent reports whether scripts asked for no banner, by flag or by TQ_NO_BANNER=1.
func Silent(noBanner bool, getenv func(string) string) bool {
	return noBanner || getenv("TQ_NO_BANNER") == "1"
}

// section returns what follows the [name] line of the asset, up to the next section.
func section(name string) string {
	var out []string
	in := false
	for _, line := range strings.Split(strings.ReplaceAll(asset, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			in = line == "["+name+"]"
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// UTF8 reports whether the terminal takes UTF-8. The locale variables decide when one is set. With
// none, Windows says yes (Go writes to the console as UTF-16, whatever its code page) and other
// systems say no.
func UTF8(e Env) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := e.Getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return e.GOOS == "windows"
}

// ansiColors are the colour names the asset may use.
var ansiColors = map[string]string{
	"black": "30", "red": "31", "green": "32", "yellow": "33", "blue": "34", "magenta": "35", "cyan": "36", "white": "37",
}

// colorOn reports whether escape codes will show as colour: never under NO_COLOR, and on Windows
// only in a terminal known to read them, since the old console prints them as text.
func colorOn(e Env) bool {
	if e.Getenv("NO_COLOR") != "" {
		return false
	}
	if e.GOOS != "windows" {
		return true
	}
	return e.Getenv("WT_SESSION") != "" || e.Getenv("TERM_PROGRAM") != "" || e.Getenv("ConEmuANSI") == "ON"
}

// Print writes the art, a blank line and the status lines. A name or folder holding control
// characters is printed with them removed, so it cannot drive the terminal.
func Print(w io.Writer, s Status, e Env) {
	art, sep := section("utf8"), "  ·  "
	if !UTF8(e) {
		art, sep = section("ascii"), " | "
	}
	if code, ok := ansiColors[strings.TrimSpace(section("color"))]; ok && colorOn(e) {
		art = "\x1b[" + code + "m" + art + "\x1b[0m"
	}
	fmt.Fprintf(w, "%s\n\n  tentaqles %s%sidentity: %s\n  %s\n\n", art, clean(s.Version), sep, clean(s.Identity), clean(s.Dir))
}

func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}
