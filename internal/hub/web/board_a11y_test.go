package web

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/tentaqles/tentaqles/internal/hub/store"
)

// moveMenuWants is the keyboard path on a board tile: a menu button that names the card, its
// popup with one menu item per lane, hidden until the script opens it.
func moveMenuWants(id, title string) []string {
	return []string{
		`<div class="mv">`,
		`<button class="mvbtn" type="button" aria-haspopup="menu" aria-expanded="false" aria-controls="mv-` + id + `" aria-label="Move ` + title + ` to another lane" title="Move to another lane">`,
		`<ul class="mvlist" id="mv-` + id + `" role="menu" aria-label="Move ` + title + ` to" hidden>`,
		`<li role="none"><button type="button" role="menuitem" tabindex="-1" data-to="backlog">Backlog</button></li>`,
		`<li role="none"><button type="button" role="menuitem" tabindex="-1" data-to="todo">Todo</button></li>`,
		`<li role="none"><button type="button" role="menuitem" tabindex="-1" data-to="in progress">In progress</button></li>`,
		`<li role="none"><button type="button" role="menuitem" tabindex="-1" data-to="review">Review</button></li>`,
		`<li role="none"><button type="button" role="menuitem" tabindex="-1" data-to="done">Done</button></li>`,
	}
}

func TestBoardTileHasAKeyboardMoveMenu(t *testing.T) {
	e := newEnv(t, "acme")
	c, _ := e.svcs["acme"].Add(context.Background(), store.NewCard{Title: "Export invoices"}, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	for _, want := range moveMenuWants(c.ID, "Export invoices") {
		if !strings.Contains(body, want) {
			t.Errorf("board tile lacks %q", want)
		}
	}
	// the title link stays first in the tile: without script it is the way to the card's Move form
	if !regexp.MustCompile(`<a class="t" href="/cards/` + c.ID + `\?ws=acme">Export invoices</a>\s*<div class="mv">`).MatchString(body) {
		t.Error("the Move button follows the title link")
	}
	assertCSPSafe(t, "/", body)

	_, tile, _ := get(t, ts, "/cards/"+c.ID+"/tile?ws=acme", nil)
	for _, want := range moveMenuWants(c.ID, "Export invoices") {
		if !strings.Contains(tile, want) {
			t.Errorf("the tile fragment lacks %q", want)
		}
	}
}

func TestBoardMoveMenuEscapesTheTitle(t *testing.T) {
	e := newEnv(t, "acme")
	e.svcs["acme"].Add(context.Background(), store.NewCard{Title: `<b>"x"</b>`}, nil)
	ts := e.server(Options{})
	_, body, _ := get(t, ts, "/?ws=acme", nil)
	if strings.Contains(body, `aria-label="Move <b>`) || !strings.Contains(body, `aria-label="Move &lt;b&gt;&#34;x&#34;&lt;/b&gt; to another lane"`) {
		t.Error("the Move button's label escapes the title")
	}
}

func TestCSSMoveMenuIsScriptOnlyAndReachable(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	for _, want := range []string{
		".card .mv{display:none", ".js .card .mv{display:contents}",
		".card:hover .mvbtn,.card:focus-within .mvbtn", ".mvlist{", `.mvlist [role="menuitem"]:focus`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("hub.css lacks %q", want)
		}
	}
}

// TestCSSWorkspaceSwitcherStaysBelow900px: the narrow layout folds the sidebar into a bar; the
// switcher stays in it instead of being hidden.
func TestCSSWorkspaceSwitcherStaysBelow900px(t *testing.T) {
	css := readAsset(t, "static/hub.css")
	i := strings.Index(css, "@media (max-width:900px){")
	if i < 0 {
		t.Fatal("hub.css has no 900px block")
	}
	block := css[i:]
	if j := strings.Index(block, "\n}"); j > 0 {
		block = block[:j]
	}
	if regexp.MustCompile(`(^|[,{\s])\.ws\s*[,{][^}]*display:none`).MatchString(block) {
		t.Error("the 900px block hides the workspace switcher")
	}
	for _, want := range []string{".side .ws{", ".side .wslist{position:absolute"} {
		if !strings.Contains(block, want) {
			t.Errorf("the 900px block lacks %q", want)
		}
	}
}

func TestHubJSKeyboardMoveAndBFCache(t *testing.T) {
	js := readAsset(t, "static/hub.js")
	for _, want := range []string{
		`"pageshow"`, "e.persisted", ".reset()", `"aria-expanded"`, `"Escape"`, `"ArrowDown"`, `"ArrowUp"`, `"Home"`, `"End"`,
		"function moveTile(", ".mvbtn",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("hub.js lacks %q", want)
		}
	}
	if strings.Count(js, "moveCard(") < 3 { // the definition, the move form, moveTile (drop and the menu share it)
		t.Error("the board's moves go through moveCard")
	}
}
