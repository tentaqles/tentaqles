package jira

import (
	"encoding/json"
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Atlassian Document Format (ADF) conversion, both ways, for a markdown subset: paragraphs (lines
// joined by hard breaks), headings, bullet and numbered lists, fenced code, inline code, bold,
// italics, links and bare web URLs. Anything else in ADF is read as its text.

type node = map[string]any

func doc(content []any) node {
	if content == nil {
		content = []any{}
	}
	return node{"type": "doc", "version": 1, "content": content}
}

var (
	fenceRe   = regexp.MustCompile("^\\s*```\\s*([A-Za-z0-9_+.#-]*)\\s*$")
	headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	bulletRe  = regexp.MustCompile(`^\s*[-*+]\s+(.*)$`)
	orderedRe = regexp.MustCompile(`^\s*\d+[.)]\s+(.*)$`)
)

// markdownToADF converts markdown to an ADF document.
func markdownToADF(md string) node {
	lines := strings.Split(strings.ReplaceAll(md, "\r\n", "\n"), "\n")
	var blocks []any
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		var content []any
		for i, l := range para {
			if i > 0 {
				content = append(content, node{"type": "hardBreak"})
			}
			content = append(content, inline(l)...)
		}
		para = nil
		if len(content) > 0 {
			blocks = append(blocks, node{"type": "paragraph", "content": content})
		}
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		if m := fenceRe.FindStringSubmatch(l); m != nil {
			flush()
			var code []string
			for i++; i < len(lines) && !fenceRe.MatchString(lines[i]); i++ {
				code = append(code, lines[i])
			}
			b := node{"type": "codeBlock"}
			if m[1] != "" {
				b["attrs"] = node{"language": m[1]}
			}
			if text := strings.Join(code, "\n"); text != "" {
				b["content"] = []any{node{"type": "text", "text": text}}
			}
			blocks = append(blocks, b)
			continue
		}
		if strings.TrimSpace(l) == "" {
			flush()
			continue
		}
		if m := headingRe.FindStringSubmatch(l); m != nil {
			flush()
			h := node{"type": "heading", "attrs": node{"level": len(m[1])}}
			if c := inline(strings.TrimSpace(m[2])); len(c) > 0 {
				h["content"] = c
			}
			blocks = append(blocks, h)
			continue
		}
		if list, re := listKind(l); list != "" {
			flush()
			var items []any
			for ; i < len(lines); i++ {
				m := re.FindStringSubmatch(lines[i])
				if m == nil {
					break
				}
				c := inline(m[1])
				if c == nil {
					c = []any{} // Jira rejects "content": null
				}
				item := node{"type": "listItem", "content": []any{node{"type": "paragraph", "content": c}}}
				items = append(items, item)
			}
			i--
			blocks = append(blocks, node{"type": list, "content": items})
			continue
		}
		para = append(para, l)
	}
	flush()
	return doc(blocks)
}

func listKind(l string) (string, *regexp.Regexp) {
	switch {
	case bulletRe.MatchString(l):
		return "bulletList", bulletRe
	case orderedRe.MatchString(l):
		return "orderedList", orderedRe
	}
	return "", nil
}

// inlineRe finds, in order: `code`, [text](url), **bold**, *italic* / _italic_, a bare web URL.
var inlineRe = regexp.MustCompile("`([^`]+)`" + `|\[([^\]]+)\]\(([^)\s]+)\)|\*\*([^*]+)\*\*|\b_([^_\s][^_]*)_\b|\*([^*\s][^*]*)\*|(https?://[^\s<>()]+)`)

func text(s string, marks ...node) node {
	n := node{"type": "text", "text": s}
	if len(marks) > 0 {
		ms := make([]any, len(marks))
		for i, m := range marks {
			ms[i] = m
		}
		n["marks"] = ms
	}
	return n
}

func webURL(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:")
}

func link(href string) node { return node{"type": "link", "attrs": node{"href": href}} }

func inline(s string) []any {
	var out []any
	plain := func(t string) {
		if t != "" {
			out = append(out, text(t))
		}
	}
	pos := 0
	for _, m := range inlineRe.FindAllStringSubmatchIndex(s, -1) {
		plain(s[pos:m[0]])
		pos = m[1]
		g := func(i int) string {
			if m[2*i] < 0 {
				return ""
			}
			return s[m[2*i]:m[2*i+1]]
		}
		switch {
		case m[2] >= 0:
			out = append(out, text(g(1), node{"type": "code"}))
		case m[4] >= 0:
			if webURL(g(3)) {
				out = append(out, text(g(2), link(g(3))))
			} else {
				plain(s[m[0]:m[1]])
			}
		case m[8] >= 0:
			out = append(out, text(g(4), node{"type": "strong"}))
		case m[10] >= 0:
			out = append(out, text(g(5), node{"type": "em"}))
		case m[12] >= 0:
			out = append(out, text(g(6), node{"type": "em"}))
		case m[14] >= 0:
			u := g(7)
			trimmed := strings.TrimRight(u, ".,;:!?'\"")
			out = append(out, text(trimmed, link(trimmed)))
			plain(u[len(trimmed):])
		}
	}
	plain(s[pos:])
	return out
}

// adfNode is an ADF node as read.
type adfNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text"`
	Attrs   map[string]any `json:"attrs"`
	Marks   []adfNode      `json:"marks"`
	Content []adfNode      `json:"content"`
}

// adfToMarkdown converts an ADF document to markdown. A JSON string (API v2, acli) is returned as
// is; null or invalid JSON is "".
func adfToMarkdown(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var n adfNode
	if json.Unmarshal(raw, &n) != nil {
		return ""
	}
	return strings.TrimSpace(blocksMD(n.Content, ""))
}

func attr(n adfNode, k string) string {
	switch v := n.Attrs[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// blocksMD renders block nodes separated by blank lines; indent prefixes nested list lines.
func blocksMD(ns []adfNode, indent string) string {
	var parts []string
	for _, n := range ns {
		if s := blockMD(n, indent); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}

func blockMD(n adfNode, indent string) string {
	switch n.Type {
	case "paragraph":
		return inlineMD(n.Content)
	case "heading":
		lvl, _ := strconv.Atoi(attr(n, "level"))
		if lvl < 1 || lvl > 6 {
			lvl = 1
		}
		return strings.Repeat("#", lvl) + " " + inlineMD(n.Content)
	case "bulletList", "orderedList":
		var lines []string
		for i, item := range n.Content {
			marker := "- "
			if n.Type == "orderedList" {
				marker = strconv.Itoa(i+1) + ". "
			}
			var head []string
			var nested []string
			for _, c := range item.Content {
				if c.Type == "bulletList" || c.Type == "orderedList" {
					nested = append(nested, blockMD(c, indent+"  "))
				} else if s := blockMD(c, indent); s != "" {
					head = append(head, s)
				}
			}
			lines = append(lines, indent+marker+strings.Join(head, " "))
			lines = append(lines, nested...)
		}
		return strings.Join(lines, "\n")
	case "codeBlock":
		var b strings.Builder
		for _, c := range n.Content {
			b.WriteString(c.Text)
		}
		code := b.String()
		if !strings.HasSuffix(code, "\n") {
			code += "\n"
		}
		return "```" + attr(n, "language") + "\n" + code + "```"
	case "blockquote":
		inner := blocksMD(n.Content, "")
		return "> " + strings.ReplaceAll(inner, "\n", "\n> ")
	case "rule":
		return "---"
	case "table":
		var rows []string
		for _, r := range n.Content {
			var cells []string
			for _, c := range r.Content {
				cells = append(cells, strings.ReplaceAll(blocksMD(c.Content, ""), "\n", " "))
			}
			rows = append(rows, "| "+strings.Join(cells, " | ")+" |")
		}
		return strings.Join(rows, "\n")
	case "mediaSingle", "mediaGroup", "media":
		return "" // attachments are listed by name and url, never inlined
	case "text", "hardBreak", "mention", "emoji", "inlineCard", "date", "status":
		return inlineMD([]adfNode{n})
	}
	return blocksMD(n.Content, indent) // panel, expand, layout, unknown: their content
}

func inlineMD(ns []adfNode) string {
	var b strings.Builder
	for _, n := range ns {
		switch n.Type {
		case "text":
			b.WriteString(marked(n))
		case "hardBreak":
			b.WriteString("\n")
		case "mention", "emoji", "status":
			b.WriteString(attr(n, "text"))
		case "inlineCard":
			b.WriteString(attr(n, "url"))
		case "date":
			b.WriteString(attr(n, "timestamp"))
		default:
			b.WriteString(inlineMD(n.Content))
		}
	}
	return b.String()
}

func marked(n adfNode) string {
	s := n.Text
	var href string
	for _, m := range n.Marks {
		switch m.Type {
		case "code":
			s = "`" + s + "`"
		case "strong":
			s = "**" + s + "**"
		case "em":
			s = "_" + s + "_"
		case "strike":
			s = "~~" + s + "~~"
		case "link":
			href = attr(m, "href")
		}
	}
	if href != "" {
		if href == n.Text {
			return href
		}
		return "[" + s + "](" + href + ")"
	}
	return s
}

var (
	breakRe = regexp.MustCompile(`(?i)<br\s*/?>`)
	blockRe = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|pre|blockquote|tr|table|ul|ol)>`)
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	blankRe = regexp.MustCompile(`\n{3,}`)
)

// htmlText is the text of a renderedFields HTML value: the fallback when the ADF has no text.
func htmlText(h string) string {
	h = breakRe.ReplaceAllString(h, "\n")
	h = blockRe.ReplaceAllString(h, "\n\n")
	h = html.UnescapeString(tagRe.ReplaceAllString(h, ""))
	lines := strings.Split(h, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(blankRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
