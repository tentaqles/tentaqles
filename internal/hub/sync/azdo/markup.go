package azdo

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Azure DevOps keeps descriptions, acceptance criteria and comments as HTML. The hub keeps
// markdown, so outbound text is converted with MarkdownToHTML (a small, safe subset: paragraphs,
// headings, lists, quotes, code, bold, italics and http(s)/mailto links) and inbound text is
// flattened with HTMLToText.

var (
	reHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	reUL      = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	reOL      = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
	reQuote   = regexp.MustCompile(`^>\s?(.*)$`)
	reLink    = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reBold    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic  = regexp.MustCompile(`\*([^*\s][^*]*)\*`)
)

// MarkdownToHTML converts comment or description markdown to the HTML Azure DevOps renders.
// Everything is escaped first, so no raw HTML from the markdown reaches the tracker.
func MarkdownToHTML(md string) string {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var out strings.Builder
	var para, quote, code []string
	list := "" // "ul" | "ol" | ""
	inCode := false

	flushPara := func() {
		if len(para) > 0 {
			out.WriteString("<p>" + strings.Join(para, "<br>") + "</p>")
			para = nil
		}
	}
	flushQuote := func() {
		if len(quote) > 0 {
			out.WriteString("<blockquote>" + strings.Join(quote, "<br>") + "</blockquote>")
			quote = nil
		}
	}
	closeList := func() {
		if list != "" {
			out.WriteString("</" + list + ">")
			list = ""
		}
	}
	flush := func() { flushPara(); flushQuote(); closeList() }

	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			if inCode {
				out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
				code, inCode = nil, false
			} else {
				flush()
				inCode = true
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		if m := reHeading.FindStringSubmatch(line); m != nil {
			flush()
			n := string(rune('0' + len(m[1])))
			out.WriteString("<h" + n + ">" + inline(m[2]) + "</h" + n + ">")
			continue
		}
		if m := reUL.FindStringSubmatch(line); m != nil {
			startList(&out, &list, "ul", flushPara, flushQuote)
			out.WriteString("<li>" + inline(m[1]) + "</li>")
			continue
		}
		if m := reOL.FindStringSubmatch(line); m != nil {
			startList(&out, &list, "ol", flushPara, flushQuote)
			out.WriteString("<li>" + inline(m[1]) + "</li>")
			continue
		}
		if m := reQuote.FindStringSubmatch(line); m != nil {
			flushPara()
			closeList()
			quote = append(quote, inline(m[1]))
			continue
		}
		flushQuote()
		closeList()
		para = append(para, inline(strings.TrimSpace(line)))
	}
	if inCode { // an unclosed fence still renders as code
		out.WriteString("<pre><code>" + html.EscapeString(strings.Join(code, "\n")) + "</code></pre>")
	}
	flush()
	return out.String()
}

func startList(out *strings.Builder, list *string, kind string, flushPara, flushQuote func()) {
	flushPara()
	flushQuote()
	if *list == kind {
		return
	}
	if *list != "" {
		out.WriteString("</" + *list + ">")
	}
	out.WriteString("<" + kind + ">")
	*list = kind
}

// inline renders code spans, links, bold and italics in one line of markdown.
func inline(s string) string {
	parts := strings.Split(s, "`")
	var b strings.Builder
	for i, p := range parts {
		closed := i+1 < len(parts) // a closing backtick follows
		switch {
		case i%2 == 1 && closed:
			b.WriteString("<code>" + html.EscapeString(p) + "</code>")
		case i%2 == 1: // an unbalanced backtick stays literal
			b.WriteString("`" + emphasis(html.EscapeString(p)))
		default:
			b.WriteString(emphasis(html.EscapeString(p)))
		}
	}
	return b.String()
}

// emphasis renders links, then bold and italics outside them: each anchor is swapped for a NUL
// placeholder during the emphasis pass, so asterisks in an href stay as they are.
func emphasis(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	var anchors []string
	s = reLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := reLink.FindStringSubmatch(m)
		u := html.UnescapeString(sm[2])
		low := strings.ToLower(u)
		if !strings.HasPrefix(low, "https://") && !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "mailto:") {
			return m
		}
		anchors = append(anchors, `<a href="`+html.EscapeString(u)+`">`+bolds(sm[1])+`</a>`)
		return "\x00" + strconv.Itoa(len(anchors)-1) + "\x00"
	})
	s = bolds(s)
	if len(anchors) == 0 {
		return s
	}
	return rePlaceholder.ReplaceAllStringFunc(s, func(m string) string {
		n, _ := strconv.Atoi(strings.Trim(m, "\x00"))
		return anchors[n]
	})
}

var rePlaceholder = regexp.MustCompile("\x00[0-9]+\x00")

// bolds renders **bold** and *italics*.
func bolds(s string) string {
	s = reBold.ReplaceAllString(s, "<strong>$1</strong>")
	return reItalic.ReplaceAllString(s, "<em>$1</em>")
}

var (
	reScript   = regexp.MustCompile(`(?is)<script\b.*?</script\s*>`)
	reStyle    = regexp.MustCompile(`(?is)<style\b.*?</style\s*>`)
	reBR       = regexp.MustCompile(`(?i)<br\s*/?>`)
	reLI       = regexp.MustCompile(`(?i)<li\b[^>]*>`)
	reBlockEnd = regexp.MustCompile(`(?i)</(p|div|li|h[1-6]|tr|blockquote|pre|ul|ol|table)\s*>`)
	reTag      = regexp.MustCompile(`<[^>]*>`)
	reBlank    = regexp.MustCompile(`\n{3,}`)
)

// HTMLToText flattens tracker HTML to plain text with line breaks and "- " list items.
func HTMLToText(s string) string {
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = reScript.ReplaceAllString(s, "")
	s = reStyle.ReplaceAllString(s, "")
	s = reBR.ReplaceAllString(s, "\n")
	s = reLI.ReplaceAllString(s, "- ")
	s = reBlockEnd.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, " ", " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	s = reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
	return strings.TrimSpace(s)
}
