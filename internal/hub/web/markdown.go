package web

import (
	"fmt"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"strings"
)

var (
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	ulRe      = regexp.MustCompile(`^[-*]\s+(.*)$`)
	olRe      = regexp.MustCompile(`^\d+[.)]\s+(.*)$`)
	codeRe    = regexp.MustCompile("`([^`]+)`")
	linkRe    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s<]+)\)`) // runs on escaped text: a < is a code span's tag
)

// RenderMarkdown renders the small safe subset of spec §11: headings, lists, fenced and inline
// code, links to http/https or relative paths. Everything else, raw HTML included, is escaped.
func RenderMarkdown(src string) template.HTML {
	var b strings.Builder
	var para []string
	list, inCode := "", false
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString("<p>" + inline(strings.Join(para, " ")) + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if list != "" {
			b.WriteString("</" + list + ">\n")
			list = ""
		}
	}
	openList := func(kind string) {
		if list != kind {
			closeList()
			b.WriteString("<" + kind + ">\n")
			list = kind
		}
	}
	for _, ln := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				flushPara()
				closeList()
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(html.EscapeString(ln) + "\n")
			continue
		}
		switch {
		case t == "":
			flushPara()
			closeList()
		case headingRe.MatchString(t):
			flushPara()
			closeList()
			m := headingRe.FindStringSubmatch(t)
			level := len(m[1]) + 2 // card headings never outrank the page's own
			if level > 6 {
				level = 6
			}
			fmt.Fprintf(&b, "<h%d>%s</h%d>\n", level, inline(m[2]), level)
		case ulRe.MatchString(t):
			flushPara()
			openList("ul")
			b.WriteString("<li>" + inline(ulRe.FindStringSubmatch(t)[1]) + "</li>\n")
		case olRe.MatchString(t):
			flushPara()
			openList("ol")
			b.WriteString("<li>" + inline(olRe.FindStringSubmatch(t)[1]) + "</li>\n")
		default:
			closeList()
			para = append(para, t)
		}
	}
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	flushPara()
	closeList()
	return template.HTML(b.String())
}

// safeHref admits http, https and relative links only. Protocol-relative forms (`//host`, and
// the backslash spellings browsers treat the same) and control characters are refused.
func safeHref(raw string) (string, bool) {
	// judge the entity-decoded spelling too (`javascript&colon;…`), so no reading of it is unsafe
	if dec := html.UnescapeString(raw); dec != raw {
		if _, ok := safeHref(dec); !ok {
			return "", false
		}
	}
	if strings.ContainsAny(raw, "\\") || strings.HasPrefix(raw, "//") {
		return "", false
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return "", false
		}
		return u.String(), true
	case "":
		if u.Host != "" || u.Opaque != "" {
			return "", false
		}
		return u.String(), true
	}
	return "", false
}

// inline escapes first, then re-introduces code spans and safe links on the escaped text.
func inline(s string) string {
	e := html.EscapeString(s)
	e = codeRe.ReplaceAllString(e, "<code>$1</code>")
	return linkRe.ReplaceAllStringFunc(e, func(m string) string {
		sub := linkRe.FindStringSubmatch(m)
		href, ok := safeHref(html.UnescapeString(sub[2]))
		if !ok {
			return sub[1]
		}
		return `<a href="` + html.EscapeString(href) + `" rel="noopener noreferrer">` + sub[1] + `</a>`
	})
}
