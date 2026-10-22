package web

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func readAsset(t *testing.T, name string) string {
	t.Helper()
	b, err := assets.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const (
	rootOpen      = ":root{"
	lightAutoOpen = `@media (prefers-color-scheme: light){:root:not([data-theme="dark"]){`
	lightPickOpen = `:root[data-theme="light"]{`
)

// themeInvariant tokens are the same in both themes and are not redefined.
var themeInvariant = map[string]bool{"r": true, "sans": true, "mono": true}

var declRe = regexp.MustCompile(`--([a-z0-9-]+)\s*:\s*([^;]+);`)

// cssBlock is the text between opener and the next closing brace.
func cssBlock(t *testing.T, css, opener string) string {
	t.Helper()
	i := strings.Index(css, opener)
	if i < 0 {
		t.Fatalf("hub.css has no %q", opener)
	}
	rest := css[i+len(opener):]
	j := strings.Index(rest, "}")
	if j < 0 {
		t.Fatalf("%q is not closed", opener)
	}
	return rest[:j]
}

func cssTokens(block string) map[string]string {
	out := map[string]string{}
	for _, m := range declRe.FindAllStringSubmatch(block, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

func TestCSSTokensAreRedefinedInBothLightBlocks(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	root := cssTokens(cssBlock(t, css, rootOpen))
	if len(root) < 36 {
		t.Fatalf(":root defines %d tokens; the mockup's set plus the theme tokens is 40", len(root))
	}
	auto := cssTokens(cssBlock(t, css, lightAutoOpen))
	pick := cssTokens(cssBlock(t, css, lightPickOpen))
	for name := range root {
		if themeInvariant[name] {
			continue
		}
		if _, ok := auto[name]; !ok {
			t.Errorf("the system light block does not redefine --%s", name)
		}
		if _, ok := pick[name]; !ok {
			t.Errorf("the toggle light block does not redefine --%s", name)
		}
	}
	for name, v := range auto {
		if _, ok := root[name]; !ok {
			t.Errorf("--%s is defined for light but not in :root", name)
		}
		if pick[name] != v {
			t.Errorf("--%s differs between the two light blocks: %q vs %q", name, v, pick[name])
		}
	}
}

func hexRGB(t *testing.T, v string) [3]float64 {
	t.Helper()
	v = strings.TrimPrefix(strings.TrimSpace(v), "#")
	if len(v) != 6 {
		t.Fatalf("not a #rrggbb colour: %q", v)
	}
	var c [3]float64
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseUint(v[2*i:2*i+2], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		c[i] = float64(n) / 255
	}
	return c
}

func luminance(c [3]float64) float64 {
	lin := func(x float64) float64 {
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c[0]) + 0.7152*lin(c[1]) + 0.0722*lin(c[2])
}

func contrast(a, b [3]float64) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestCSSTextContrast: body text, secondary and tertiary text on every surface, every pill, the
// warm button and the log reach WCAG AA (4.5:1) in both themes. The primary button's gradient
// runs from --blue-d to --pri-end; its white text reaches 4.5:1 at both stops.
func TestCSSTextContrast(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	themes := map[string]map[string]string{
		"dark":  cssTokens(cssBlock(t, css, rootOpen)),
		"light": cssTokens(cssBlock(t, css, lightPickOpen)),
	}
	pairs := []struct {
		fg, bg string
		min    float64
	}{
		{"fg", "bg", 4.5}, {"fg", "s1", 4.5}, {"fg", "s2", 4.5}, {"fg", "s3", 4.5},
		{"fg-2", "bg", 4.5}, {"fg-2", "s1", 4.5}, {"fg-2", "s2", 4.5}, {"fg-2", "s3", 4.5},
		{"fg-3", "bg", 4.5}, {"fg-3", "s1", 4.5}, {"fg-3", "s2", 4.5}, {"fg-3", "s3", 4.5}, {"fg-3", "lane-bg", 4.5},
		{"sky", "blue-soft", 4.5}, {"orange-t", "orange-soft", 4.5}, {"teal", "teal-soft", 4.5},
		{"red", "red-soft", 4.5}, {"violet", "violet-soft", 4.5}, {"fg-2", "s3", 4.5},
		{"on-warm", "orange", 4.5}, {"on-mark", "teal", 4.5}, {"on-pri", "blue-d", 4.5}, {"on-pri", "pri-end", 4.5},
		{"log-fg", "log-bg", 4.5}, {"log-k", "log-bg", 4.5}, {"log-ok", "log-bg", 4.5},
	}
	for theme, tok := range themes {
		for _, p := range pairs {
			got := contrast(hexRGB(t, tok[p.fg]), hexRGB(t, tok[p.bg]))
			if got < p.min {
				t.Errorf("%s: --%s on --%s is %.2f:1, want %.1f:1", theme, p.fg, p.bg, got, p.min)
			}
		}
	}
}

func TestCSSHasTheMockupVocabularyAndNoNetwork(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	for _, want := range []string{
		"--r:8px;", ".p-queue{", ".p-run{", ".p-wait{", ".p-done{", ".p-fail{", ".p-budget{",
		"prefers-reduced-motion:reduce", ":focus-visible", ".card.hot{", ".slot.show{", ".card.settle{",
		".lane.over{", ".scrim:target", ".toast.on{", ".actbar{", ".verdict{", ".sw input:checked+i{",
		"@keyframes settle", "@keyframes pulse", "@keyframes dlgin",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("hub.css lacks %q", want)
		}
	}
	for _, bad := range []string{"@import", "url(", "fonts.googleapis", "http://", "https://"} {
		if strings.Contains(css, bad) {
			t.Errorf("hub.css carries %q: the hub must work offline and the export must stay self-contained", bad)
		}
	}
}

// TestHubJSBuildsNodesNotHTML: data reaches the page through textContent; the only HTML the
// script parses is fragment(), the hub's own html/template output.
func TestHubJSBuildsNodesNotHTML(t *testing.T) {
	js := readAsset(t, "static/hub.js")
	if n := strings.Count(js, "innerHTML"); n != 1 || !strings.Contains(js, "function fragment(html)") {
		t.Fatalf("hub.js uses innerHTML %d times; only fragment() may", n)
	}
	for _, bad := range []string{"outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function"} {
		if strings.Contains(js, bad) {
			t.Errorf("hub.js uses %s", bad)
		}
	}
	for _, want := range []string{`"tq-hub-theme"`, "localStorage", "prefers-color-scheme: light", "Switch to light mode", "Switch to dark mode", "#i-sun", "#i-moon", "EventSource"} {
		if !strings.Contains(js, want) {
			t.Errorf("hub.js lacks %q", want)
		}
	}
	if !regexp.MustCompile(`try\s*\{\s*var v = window\.localStorage\.getItem`).MatchString(js) ||
		!regexp.MustCompile(`try\s*\{\s*window\.localStorage\.setItem`).MatchString(js) {
		t.Error("every localStorage access sits inside try/catch")
	}
	if strings.Contains(js, "X-TQ-Hub-Agent") {
		t.Error("the page never marks itself as an agent")
	}
}
