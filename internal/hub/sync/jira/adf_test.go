package jira

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMarkdownToADF(t *testing.T) {
	md := "# Title\n\nFirst line\nsecond with `code` and **bold** and [a link](https://example.com/x) and https://example.com/y.\n\n" +
		"- one\n* two\n\n1. first\n2. second\n\n```python\nprint('hi')\n\n```\n\n[bad](javascript:alert(1))"
	raw, _ := json.Marshal(markdownToADF(md))
	got := string(raw)
	for _, want := range []string{
		`{"attrs":{"level":1},"content":[{"text":"Title","type":"text"}],"type":"heading"}`,
		`{"type":"hardBreak"}`,
		`{"marks":[{"type":"code"}],"text":"code","type":"text"}`,
		`{"marks":[{"type":"strong"}],"text":"bold","type":"text"}`,
		`{"marks":[{"attrs":{"href":"https://example.com/x"},"type":"link"}],"text":"a link","type":"text"}`,
		`{"marks":[{"attrs":{"href":"https://example.com/y"},"type":"link"}],"text":"https://example.com/y","type":"text"}`,
		`{"text":".","type":"text"}`,
		`{"content":[{"content":[{"content":[{"text":"one","type":"text"}],"type":"paragraph"}],"type":"listItem"},{"content":[{"content":[{"text":"two","type":"text"}],"type":"paragraph"}],"type":"listItem"}],"type":"bulletList"}`,
		`"type":"orderedList"`,
		`{"attrs":{"language":"python"},"content":[{"text":"print('hi')` + `\n` + `","type":"text"}],"type":"codeBlock"}`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s\nin %s", want, got)
		}
	}
	if strings.Contains(got, "javascript:") && strings.Contains(got, `"href":"javascript`) {
		t.Fatalf("a non-web link became a link mark: %s", got)
	}
	if !strings.HasPrefix(got, `{"content":[`) || !strings.HasSuffix(got, `"type":"doc","version":1}`) {
		t.Fatal(got)
	}
	empty, _ := json.Marshal(markdownToADF(""))
	if string(empty) != `{"content":[],"type":"doc","version":1}` {
		t.Fatal(string(empty))
	}
	// an unclosed fence keeps its text
	raw, _ = json.Marshal(markdownToADF("```\nx := 1"))
	if !strings.Contains(string(raw), `"codeBlock"`) || !strings.Contains(string(raw), "x := 1") {
		t.Fatal(string(raw))
	}
}

func TestADFToMarkdown(t *testing.T) {
	doc := `{"type":"doc","version":1,"content":[
	 {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Plan"}]},
	 {"type":"paragraph","content":[
	   {"type":"text","text":"See "},
	   {"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://example.com/d"}}]},
	   {"type":"text","text":", run "},
	   {"type":"text","text":"make","marks":[{"type":"code"}]},
	   {"type":"hardBreak"},
	   {"type":"text","text":"now","marks":[{"type":"strong"}]},
	   {"type":"text","text":" "},
	   {"type":"mention","attrs":{"text":"@Tester"}},
	   {"type":"emoji","attrs":{"shortName":":smile:","text":"😄"}},
	   {"type":"inlineCard","attrs":{"url":"https://example.com/c"}}]},
	 {"type":"bulletList","content":[
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a"}]},
	     {"type":"bulletList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"a1"}]}]}]}]},
	   {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"b"}]}]}]},
	 {"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"x"}]}]},{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"y"}]}]}]},
	 {"type":"codeBlock","attrs":{"language":"sql"},"content":[{"type":"text","text":"select 1"}]},
	 {"type":"blockquote","content":[{"type":"paragraph","content":[{"type":"text","text":"quoted"}]}]},
	 {"type":"panel","content":[{"type":"paragraph","content":[{"type":"text","text":"in a panel"}]}]},
	 {"type":"rule"},
	 {"type":"table","content":[{"type":"tableRow","content":[
	   {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"k"}]}]},
	   {"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"v"}]}]}]}]},
	 {"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]}]}`
	got := adfToMarkdown(json.RawMessage(doc))
	want := "## Plan\n\nSee [docs](https://example.com/d), run `make`\n**now** @Tester😄https://example.com/c\n\n" +
		"- a\n  - a1\n- b\n\n1. x\n2. y\n\n```sql\nselect 1\n```\n\n> quoted\n\nin a panel\n\n---\n\n| k | v |"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// a plain string (API v2 shape, acli) is kept, null is empty
	if adfToMarkdown(json.RawMessage(`"plain text"`)) != "plain text" || adfToMarkdown(json.RawMessage(`null`)) != "" || adfToMarkdown(nil) != "" {
		t.Fatal("string / null")
	}
}

func TestRoundTrip(t *testing.T) {
	md := "Run finished.\n\n- tests green\n- see [PR 12](https://github.com/acme/app/pull/12)\n\n```go\nfmt.Println(1)\n```"
	raw, _ := json.Marshal(markdownToADF(md))
	if got := adfToMarkdown(raw); got != md {
		t.Fatalf("got\n%s", got)
	}
}

func TestHTMLText(t *testing.T) {
	if got := htmlText("<p>Rendered &amp; <b>shown</b></p><p>two<br/>lines</p>"); got != "Rendered & shown\n\ntwo\nlines" {
		t.Fatalf("%q", got)
	}
}
