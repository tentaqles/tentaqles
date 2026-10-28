package azdo

import (
	"strings"
	"testing"
)

func TestMarkdownToHTML(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"hello", "<p>hello</p>"},
		{"a <b> & c", "<p>a &lt;b&gt; &amp; c</p>"},
		{"line one\nline two", "<p>line one<br>line two</p>"},
		{"one\n\ntwo", "<p>one</p><p>two</p>"},
		{"## Result", "<h2>Result</h2>"},
		{"**bold** and *it* and `x<y`", "<p><strong>bold</strong> and <em>it</em> and <code>x&lt;y</code></p>"},
		{"- a\n- b\n\nafter", "<ul><li>a</li><li>b</li></ul><p>after</p>"},
		{"1. a\n2) b", "<ol><li>a</li><li>b</li></ol>"},
		{"```\nif a < b {\n}\n```", "<pre><code>if a &lt; b {\n}</code></pre>"},
		{"[PR](https://example.test/pr/1)", `<p><a href="https://example.test/pr/1">PR</a></p>`},
		{"[bad](javascript:alert(1))", "<p>[bad](javascript:alert(1))</p>"},
		{"> quoted\n> more", "<blockquote>quoted<br>more</blockquote>"},
		{"`**not bold**`", "<p><code>**not bold**</code></p>"},
	} {
		if got := MarkdownToHTML(tc.in); got != tc.want {
			t.Errorf("MarkdownToHTML(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
}

func TestHTMLToText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"plain", "plain"},
		{"<div>one</div><div>two</div>", "one\ntwo"},
		{"<p>a &amp; b&nbsp;c</p>", "a & b c"},
		{"x<br>y<br/>z", "x\ny\nz"},
		{"<ul><li>a</li><li>b</li></ul>", "- a\n- b"},
		{"<script>alert(1)</script><style>p{}</style>ok", "ok"},
		{"<p>one</p>\n\n\n\n<p>two</p>", "one\n\ntwo"},
		{`<a href="https://example.test">link</a>`, "link"},
	} {
		if got := HTMLToText(tc.in); got != tc.want {
			t.Errorf("HTMLToText(%q)\n got %q\nwant %q", tc.in, got, tc.want)
		}
	}
	// A round trip keeps the words.
	md := "## Done\n\n- tests **green**\n- PR [1](https://example.test/1)"
	if got := HTMLToText(MarkdownToHTML(md)); !strings.Contains(got, "tests green") || !strings.Contains(got, "Done") {
		t.Fatalf("round trip %q", got)
	}
}
