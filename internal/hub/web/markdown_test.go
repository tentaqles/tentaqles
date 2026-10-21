package web

import (
	"strings"
	"testing"
)

func TestMarkdownSubset(t *testing.T) {
	got := string(RenderMarkdown("# Goal\n\nShip **it** today.\n\n- one\n- two `code`\n\n1. first\n\n```\nx < y\n```\n\nSee [docs](https://example.com/a?b=1&c=2) and [local](docs/prd.md)."))
	for _, want := range []string{"<h3>Goal</h3>", "<li>one</li>", "<li>two <code>code</code></li>", "<ol>", "<pre><code>x &lt; y\n</code></pre>",
		`<a href="https://example.com/a?b=1&amp;c=2" rel="noopener noreferrer">docs</a>`, `<a href="docs/prd.md" rel="noopener noreferrer">local</a>`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
}

func TestMarkdownIsSafe(t *testing.T) {
	cases := []string{
		"<script>alert(1)</script>",
		"<img src=x onerror=alert(1)>",
		"[click](javascript:alert(1))",
		"[click](JaVaScRiPt:alert(1))",
		"[click](data:text/html;base64,PHNjcmlwdD4=)",
		"[x](//evil.example/a)",
		"\" onmouseover=\"alert(1)",
	}
	for _, c := range cases {
		got := string(RenderMarkdown(c))
		if strings.Contains(got, "<script") || strings.Contains(got, "<img") || strings.Contains(got, "javascript:") ||
			strings.Contains(strings.ToLower(got), "href=\"javascript") || strings.Contains(got, "data:text") && strings.Contains(got, "href") ||
			strings.Contains(got, `href="//`) || strings.Contains(got, `" onmouseover="`) {
			t.Fatalf("%q rendered unsafely: %s", c, got)
		}
	}
}

func TestMarkdownRejectsBackslashAndEntityTricks(t *testing.T) {
	for _, c := range []string{"[x](/\\evil.example)", "[x](\\\\evil.example)", "[x](javascript&colon;alert(1))", "[x](java&#x09;script:alert(1))"} {
		got := string(RenderMarkdown(c))
		if strings.Contains(got, `href="/\`) || strings.Contains(got, `href="\`) || strings.Contains(strings.ToLower(got), `href="javascript`) ||
			strings.Contains(strings.ToLower(got), `href="java`) {
			t.Fatalf("%q rendered unsafely: %s", c, got)
		}
	}
}
