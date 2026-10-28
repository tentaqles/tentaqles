package asana

import (
	"encoding/xml"
	"errors"
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// markdownToHTML converts the small Markdown subset tq writes (result comments: paragraphs,
// headings, bullet and numbered lists, fenced code, **bold**, *em*, `code` and http(s) links)
// into Asana rich text: well-formed XML inside <body>, using only tags Asana accepts (strong, em,
// code, pre, a, ul, ol, li). Everything else is escaped text, so a comment can never break the
// request. Links other than http and https stay literal. Every converted span (code, link, bold,
// emphasis) is held whole while the rest of the line is formatted, so spans nest but never cross
// ("_a **b_ c**" keeps its underscores); a result that still fails an XML check goes out as
// escaped text.

var (
	codeRe    = regexp.MustCompile("`([^`]+)`")
	linkRe    = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s()\x00]+)\)`) // no placeholder in a URL
	boldRe    = regexp.MustCompile(`\*\*([^*]+?)\*\*`)
	emStarRe  = regexp.MustCompile(`(^|[^\w*])\*([^*\s](?:[^*]*[^*\s])?)\*($|[^\w*])`)
	emUnderRe = regexp.MustCompile(`(^|[^\w_])_([^_\s](?:[^_]*[^_\s])?)_($|[^\w_])`)
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.*)$`)
	bulletRe  = regexp.MustCompile(`^[-*+]\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^\d+[.)]\s+(.*)$`)
	holderRe  = regexp.MustCompile("\x00([0-9]+)\x00")
)

func markdownToHTML(md string) string {
	return safeBody(convert(md), md)
}

// safeBody returns out when it is well-formed XML, else md as escaped text in <body>.
func safeBody(out, md string) string {
	d := xml.NewDecoder(strings.NewReader(out))
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			return "<body>" + html.EscapeString(cleanMarkdown(md)) + "</body>"
		}
	}
}

func cleanMarkdown(md string) string {
	return strings.ReplaceAll(strings.ReplaceAll(md, "\r\n", "\n"), "\x00", "")
}

// convert is the Markdown to rich text conversion, before the safety check.
func convert(md string) string {
	md = cleanMarkdown(md)
	lines := strings.Split(md, "\n")
	var blocks, items []string
	listTag := ""
	flush := func() {
		if len(items) > 0 {
			blocks = append(blocks, "<"+listTag+">"+strings.Join(items, "")+"</"+listTag+">")
			items = nil
		}
		listTag = ""
	}
	for i := 0; i < len(lines); i++ {
		ln := lines[i]
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "```") {
			j := i + 1
			for j < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[j]), "```") {
				j++
			}
			if j < len(lines) {
				flush()
				blocks = append(blocks, "<pre>"+html.EscapeString(strings.Join(lines[i+1:j], "\n"))+"</pre>")
				i = j
				continue
			}
		}
		if m := bulletRe.FindStringSubmatch(t); m != nil {
			if listTag != "ul" {
				flush()
				listTag = "ul"
			}
			items = append(items, "<li>"+inline(m[1])+"</li>")
			continue
		}
		if m := orderedRe.FindStringSubmatch(t); m != nil {
			if listTag != "ol" {
				flush()
				listTag = "ol"
			}
			items = append(items, "<li>"+inline(m[1])+"</li>")
			continue
		}
		flush()
		if m := headingRe.FindStringSubmatch(t); m != nil {
			blocks = append(blocks, "<strong>"+inline(m[1])+"</strong>")
			continue
		}
		blocks = append(blocks, inline(ln))
	}
	flush()
	return "<body>" + strings.Join(blocks, "\n") + "</body>"
}

// inline formats one line: code spans and links are set aside first (their text is escaped, never
// formatted), the rest is escaped and then gets bold (with emphasis inside it) and emphasis. Each
// converted span is held as a placeholder too, so a later span can wrap it but never cut it.
func inline(s string) string {
	var held []string
	hold := func(v string) string {
		held = append(held, v)
		return "\x00" + strconv.Itoa(len(held)-1) + "\x00"
	}
	s = codeRe.ReplaceAllStringFunc(s, func(m string) string {
		return hold("<code>" + html.EscapeString(m[1:len(m)-1]) + "</code>")
	})
	s = linkRe.ReplaceAllStringFunc(s, func(m string) string {
		sm := linkRe.FindStringSubmatch(m)
		return hold(`<a href="` + html.EscapeString(sm[2]) + `">` + html.EscapeString(sm[1]) + "</a>")
	})
	emph := func(s string) string {
		for range 2 { // twice: a match uses up the separator the next one needs ("*a* *b*")
			for _, re := range []*regexp.Regexp{emStarRe, emUnderRe} {
				s = re.ReplaceAllStringFunc(s, func(m string) string {
					sm := re.FindStringSubmatch(m)
					return sm[1] + hold("<em>"+sm[2]+"</em>") + sm[3]
				})
			}
		}
		return s
	}
	s = html.EscapeString(s)
	s = boldRe.ReplaceAllStringFunc(s, func(m string) string {
		return hold("<strong>" + emph(boldRe.FindStringSubmatch(m)[1]) + "</strong>")
	})
	s = emph(s)
	// A held link can hold a code span ("[`x`](url)"): expand placeholders inside held text too.
	// A placeholder only ever refers to an earlier hold, so the recursion ends.
	var expand func(s string, below int) string
	expand = func(s string, below int) string {
		return holderRe.ReplaceAllStringFunc(s, func(m string) string {
			n, err := strconv.Atoi(m[1 : len(m)-1])
			if err != nil || n >= below {
				return ""
			}
			return expand(held[n], n)
		})
	}
	return expand(s, len(held))
}
