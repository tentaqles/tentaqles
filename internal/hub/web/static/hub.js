/* tq hub page script: one module, plain DOM, no framework.
 *
 * CSP is script-src 'self': nothing inline runs, so every behaviour is bound here. The file loads
 * in <head> so the saved theme applies before the first paint; the rest starts on DOMContentLoaded.
 * Text that came from data is always set with textContent. The only HTML parsing is fragment(),
 * which reads HTML this hub rendered through html/template (a #main refresh or a board tile). Every
 * visible change follows a server answer: an API response or an SSE change frame. The page never
 * sends the agent header; the server's same-origin guard covers every write it makes.
 */
(function () {
  "use strict";

  var root = document.documentElement;
  root.classList.add("js");

  /* ---------- theme: dark first, follows the system until the toggle picks one ---------- */
  var THEME_KEY = "tq-hub-theme";
  function storedTheme() {
    try { var v = window.localStorage.getItem(THEME_KEY); return v === "light" || v === "dark" ? v : ""; }
    catch (_) { return ""; } // storage blocked (private window, policy): follow the system
  }
  function storeTheme(v) {
    try { window.localStorage.setItem(THEME_KEY, v); } catch (_) { /* kept for this page only */ }
  }
  function isDark() {
    var t = root.getAttribute("data-theme");
    if (t) return t === "dark";
    return !(window.matchMedia && window.matchMedia("(prefers-color-scheme: light)").matches);
  }
  var savedTheme = storedTheme();
  if (savedTheme) root.setAttribute("data-theme", savedTheme);

  /* ---------- registries: page sections add to these ---------- */
  var starters = [];       // run once, after the DOM is ready
  var mainBinders = [];    // run on the first render and after every #main refresh
  var changeHandlers = []; // fn(change) → true when it handled an SSE change frame itself
  var busyChecks = [];     // fn() → true while a refresh would get in the human's way
  var hub = { ws: "", run: "", nav: "", applySearch: null };

  /* ---------- DOM helpers ---------- */
  function $(sel, from) { return (from || document).querySelector(sel); }
  function $$(sel, from) { return Array.prototype.slice.call((from || document).querySelectorAll(sel)); }
  function el(tag, cls, text) {
    var e = document.createElement(tag);
    if (cls) e.className = cls;
    if (text !== undefined && text !== null) e.textContent = String(text);
    return e;
  }
  function clear(node) { while (node && node.firstChild) node.removeChild(node.firstChild); }
  function on(node, type, fn) { if (node) node.addEventListener(type, fn); }
  function fragment(html) {
    var t = document.createElement("template");
    t.innerHTML = html; // html/template output of this hub's own pages, never data built here
    return t.content;
  }
  function cssq(v) { return window.CSS && CSS.escape ? CSS.escape(String(v)) : String(v).replace(/["\\]/g, "\\$&"); }
  function byData(attr, value, from) { return $("[" + attr + '="' + cssq(value) + '"]', from); }
  function laneTitle(name) { return name ? name.charAt(0).toUpperCase() + name.slice(1) : ""; }
  // typing: the human is editing a form field; a refresh now would throw their input away.
  function typing() {
    var a = document.activeElement;
    return !!(a && /^(TEXTAREA|INPUT|SELECT)$/.test(a.tagName) && a.closest("form"));
  }

  var PILL = { queued: "p-queue", running: "p-run", waiting: "p-wait", finished: "p-done", done: "p-done",
    failed: "p-fail", cancelled: "p-fail", "over-budget": "p-budget" };
  var LABEL = { waiting: "waiting on you", "over-budget": "over budget" };
  function pill(status, label) {
    return el("span", "pill " + (PILL[status] || "p-queue") + " st-" + status, label || LABEL[status] || status);
  }

  /* ---------- toast: built from nodes; actions are [label, fn] pairs ---------- */
  var toastTimer = null;
  function hideToast() {
    var t = $("#toast");
    if (!t) return;
    t.classList.remove("on");
    setTimeout(function () { if (!t.classList.contains("on")) clear(t); }, 250); // no focusable leftovers
  }
  function toast(parts, opts) {
    opts = opts || {};
    var t = $("#toast");
    if (!t) return;
    clear(t);
    parts.forEach(function (p) { t.appendChild(typeof p === "string" ? el("span", null, p) : p); });
    var actions = opts.actions || [];
    actions.forEach(function (a, i) {
      var b = el("button", "btn sm" + (i === 0 ? " warm" : ""), a[0]);
      b.type = "button";
      on(b, "click", function () { hideToast(); a[1](); });
      t.appendChild(b);
    });
    t.classList.add("on");
    clearTimeout(toastTimer);
    toastTimer = setTimeout(hideToast, opts.ms || (actions.length ? 12000 : 3400));
    if (actions.length) $("button", t).focus();
  }

  /* ---------- server calls ---------- */
  function withWS(path) {
    if (!hub.ws) return path;
    return path + (path.indexOf("?") < 0 ? "?" : "&") + "ws=" + encodeURIComponent(hub.ws);
  }
  // api always resolves: {ok, status, data}; data is the JSON body ({} when there is none).
  function api(method, path, body) {
    var opts = { method: method, credentials: "same-origin", headers: { Accept: "application/json" } };
    if (body !== undefined) { opts.headers["Content-Type"] = "application/json"; opts.body = JSON.stringify(body); }
    return fetch(withWS(path), opts).then(function (r) {
      return r.text().then(function (txt) {
        var data;
        try { data = txt ? JSON.parse(txt) : {}; } catch (_) { data = { error: txt }; }
        return { ok: r.ok, status: r.status, data: data || {} };
      });
    }, function () {
      return { ok: false, status: 0, data: { error: "The hub is not reachable. Is tq hub serve still running?" } };
    });
  }
  function errText(res) { return (res.data && res.data.error) || "The hub answered " + res.status + "."; }
  function getHTML(path) {
    return fetch(withWS(path), { credentials: "same-origin" }).then(function (r) {
      if (!r.ok) return { status: r.status, html: null };
      return r.text().then(function (t) { return { status: r.status, html: t }; });
    }, function () { return { status: 0, html: null }; });
  }

  /* ---------- live refresh ---------- */
  var refreshTimer = null, stale = false;
  // dirty: a control whose value differs from what the server rendered. A refresh would replace
  // it with the stored value, so an unsaved edit blocks it even after focus leaves the field.
  function dirty(c) {
    if (c.disabled) return false;
    if (c.type === "checkbox" || c.type === "radio") return c.checked !== c.defaultChecked;
    if (c.tagName === "SELECT") {
      var o = c.options, marked = false, i;
      for (i = 0; i < o.length; i++) if (o[i].defaultSelected) marked = true;
      if (!marked) return c.multiple ? c.selectedOptions.length > 0 : c.selectedIndex > 0;
      for (i = 0; i < o.length; i++) if (o[i].selected !== o[i].defaultSelected) return true;
      return false;
    }
    if (c.tagName === "TEXTAREA" || c.tagName === "INPUT") return c.value !== c.defaultValue;
    return false;
  }
  function unsavedEdits() {
    return $$("#main form").some(function (f) { return Array.prototype.some.call(f.elements, dirty); });
  }
  function busy() {
    if (typing() || unsavedEdits()) return true;
    for (var i = 0; i < busyChecks.length; i++) if (busyChecks[i]()) return true;
    return false;
  }
  function bindMain() { mainBinders.forEach(function (fn) { fn(); }); }
  // refreshMain re-renders #main from the server (?fragment=main), never while the human is busy;
  // a skipped refresh runs once they leave the field. The live log keeps its own node.
  function refreshMain() {
    if (refreshTimer) return;
    refreshTimer = setTimeout(function () {
      refreshTimer = null;
      if (busy()) { stale = true; return; }
      stale = false;
      var u = new URL(location.href);
      u.searchParams.set("fragment", "main");
      u.searchParams.delete("flash");
      fetch(u.toString(), { credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : null; })
        .then(function (html) {
          var m = $("#main");
          if (html === null || !m) return;
          var oldLog = $("#log"), y = window.scrollY;
          clear(m);
          m.appendChild(fragment(html));
          var newLog = $("#log");
          if (oldLog && newLog && oldLog.getAttribute("data-run") === newLog.getAttribute("data-run")) newLog.replaceWith(oldLog);
          window.scrollTo(0, y);
          bindMain();
          followLog();
        }, function () { stale = true; });
    }, 300);
  }
  on(document, "focusout", function () { setTimeout(function () { if (stale) refreshMain(); }, 50); });

  var countTimer = null;
  function refreshWaitingCount() {
    clearTimeout(countTimer);
    countTimer = setTimeout(function () {
      var n = $("#waiting-count");
      if (!n) return;
      api("GET", "/api/waiting").then(function (res) {
        if (!res.ok || !Array.isArray(res.data)) return;
        n.textContent = String(res.data.length);
        n.hidden = res.data.length === 0;
      });
    }, 400);
  }
  function appendLog(text) {
    var pre = $("#log");
    if (!pre || !text) return;
    var stick = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 8;
    pre.appendChild(document.createTextNode(text));
    if (stick) pre.scrollTop = pre.scrollHeight;
  }
  var source = null;
  function connect() {
    if (!window.EventSource) return;
    if (source) source.close();
    var p = new URLSearchParams();
    if (hub.ws) p.set("ws", hub.ws);
    var log = $("#log");
    if (hub.run && log) { p.set("run", hub.run); p.set("from", log.getAttribute("data-from") || "0"); }
    var es = source = new EventSource("/events?" + p.toString());
    es.addEventListener("change", function (e) {
      var c = {};
      try { c = JSON.parse(e.data) || {}; } catch (_) { /* a malformed frame still means "something changed" */ }
      refreshWaitingCount();
      for (var i = 0; i < changeHandlers.length; i++) if (changeHandlers[i](c)) return;
      refreshMain();
    });
    es.addEventListener("log", function (e) {
      try { appendLog(JSON.parse(e.data).text); } catch (_) { /* ignore a malformed frame */ }
    });
  }

  // followLog: after a #main refresh the live log may belong to a run that started (or ended)
  // since the stream opened; reopen it at that run's offset, or without a run once the log is gone.
  function followLog() {
    var log = $("#log"), run = log ? log.getAttribute("data-run") || "" : "";
    if (run === hub.run) return;
    hub.run = run;
    connect();
  }

  /* ---------- moving a card (board drop and the card page's Move form) ---------- */
  function moveToast(out, meta, lane) {
    if (out.enqueued) toast([pill("queued"), el("span", null, meta.title + ": a run starts on the next tick")]);
    else if (out.cancelled) toast([pill("cancelled"), el("span", null, meta.title + ": the run was cancelled and its lock released")]);
    else if (meta.active && !meta.ready) toast([el("span", null, "Moved to " + laneTitle(lane) + ". Add the agent-ready tag to start a run.")]);
    else toast([el("span", null, "Moved to " + laneTitle(lane) + ".")]);
  }
  // moveCard asks the server to move a card. A live run answers 409: the toast asks before the run
  // is cancelled. done(ok) runs once per server answer; ok false means the caller undoes its preview.
  function moveCard(id, lane, meta, done) {
    meta = meta || {};
    meta.title = meta.title || "The card";
    done = done || function () {};
    var path = "/api/cards/" + encodeURIComponent(id) + "/move";
    api("POST", path, { lane: lane, cancel_run: false }).then(function (res) {
      if (res.ok) { moveToast(res.data, meta, lane); done(true); return; }
      done(false);
      if (res.status === 409 && res.data.code === "run_active") {
        var run = res.data.run || {};
        toast([pill(run.status || "running"), el("span", null, meta.title + " has a " + (run.status || "live") + " run. Moving it cancels the run.")], {
          actions: [
            ["Cancel the run and move", function () {
              api("POST", path, { lane: lane, cancel_run: true }).then(function (r2) {
                if (r2.ok) { moveToast(r2.data, meta, lane); done(true); } else toast([el("span", null, errText(r2))]);
              });
            }],
            ["Keep it running", function () {}]
          ]
        });
        return;
      }
      toast([el("span", null, errText(res))]);
    });
  }
  function bindMoveForms() {
    $$("form[data-move]").forEach(function (f) {
      on(f, "submit", function (e) {
        e.preventDefault();
        var sel = $('[name="lane"]', f);
        var lane = sel.value;
        moveCard(f.getAttribute("data-card"), lane, { title: f.getAttribute("data-title") }, function (ok) {
          // the select's default is the card's stored lane: moved (also after a confirmed cancel),
          // the chosen lane becomes it; refused or declined, the select goes back to it. Either way
          // the move form is clean for unsavedEdits(), so live refresh is not blocked.
          Array.prototype.forEach.call(sel.options, function (o) {
            if (ok) { o.defaultSelected = o.value === lane; o.selected = o.defaultSelected; } else o.selected = o.defaultSelected;
          });
          if (ok) refreshMain();
        });
      });
    });
  }
  mainBinders.push(bindMoveForms);

  /* ---------- theme toggle and search ---------- */
  function bindTheme() {
    var btn = $("#themebtn");
    if (!btn) return;
    btn.hidden = false; // the markup hides it so a page without script never shows a dead toggle
    var use = $("use", btn);
    function paint() {
      var dark = isDark(), label = dark ? "Switch to light mode" : "Switch to dark mode";
      if (use) use.setAttribute("href", dark ? "#i-sun" : "#i-moon");
      btn.setAttribute("aria-label", label);
      btn.title = label;
    }
    on(btn, "click", function () {
      var v = isDark() ? "light" : "dark";
      root.setAttribute("data-theme", v);
      storeTheme(v);
      paint();
    });
    if (window.matchMedia) {
      var mq = window.matchMedia("(prefers-color-scheme: light)");
      if (mq.addEventListener) mq.addEventListener("change", paint);
    }
    paint();
  }
  function bindSearch() {
    var input = $("#search");
    if (!input) return;
    function apply() {
      var q = input.value.trim().toLowerCase();
      $$(".card[data-title]").forEach(function (c) {
        c.hidden = q !== "" && c.getAttribute("data-title").toLowerCase().indexOf(q) < 0;
      });
    }
    hub.applySearch = apply;
    on(input, "input", apply);
    on(input, "keydown", function (e) { if (e.key === "Escape") { input.value = ""; apply(); input.blur(); } });
    on(document, "keydown", function (e) {
      if (e.key === "/" && !typing() && document.activeElement !== input && !e.ctrlKey && !e.metaKey && !e.altKey) {
        e.preventDefault();
        input.focus();
      }
    });
  }
  starters.push(bindTheme, bindSearch);
  mainBinders.push(function () { if (hub.applySearch) hub.applySearch(); });

  /* ---------- board: drag and drop, live tiles ---------- */
  var drag = null;       // { card, from, next } while a card is dragged
  var slot = el("div", "slot");
  var heldTiles = {};    // tile refreshes that arrived mid-drag
  var tileTimers = {};
  var quietUntil = {};   // card id → until when a status change does not toast (the move already did)

  function laneOf(node) { return node && node.closest ? node.closest(".lane[data-lane]") : null; }
  function cardsIn(lane) { return $$(".card[data-card]", lane); }
  function laneCounts() {
    $$(".lane[data-lane]").forEach(function (lane) {
      // a capped done lane leaves data-hidden cards out of the page; its count is still the total
      var n = cardsIn(lane).length + (Number(lane.getAttribute("data-hidden")) || 0), c = $("header .c", lane), empty = $(".empty", lane);
      if (c) c.textContent = String(n);
      if (empty) empty.hidden = n > 0;
    });
  }
  function settle(card) {
    card.classList.remove("settle");
    void card.offsetWidth; // restart the animation
    card.classList.add("settle");
    setTimeout(function () { card.classList.remove("settle"); }, 400);
  }
  function placeSlot(lane, y) {
    var after = null;
    cardsIn(lane).some(function (c) {
      if (drag && c === drag.card) return false;
      var r = c.getBoundingClientRect();
      if (y < r.top + r.height / 2) { after = c; return true; }
      return false;
    });
    if (after) lane.insertBefore(slot, after); else lane.appendChild(slot);
    slot.classList.add("show");
  }
  function endDrag() {
    if (!drag) return;
    drag.card.classList.remove("dragging");
    slot.classList.remove("show");
    if (slot.parentNode) slot.parentNode.removeChild(slot);
    $$(".lane.over").forEach(function (l) { l.classList.remove("over"); });
    drag = null;
    var ids = Object.keys(heldTiles);
    heldTiles = {};
    ids.forEach(refreshTile);
  }
  function statusToast(card) {
    var st = card.getAttribute("data-status");
    if (!st) return;
    var text = card.getAttribute("data-status-text");
    toast([pill(st), el("span", null, card.getAttribute("data-title") + (text ? ": " + text : ""))]);
  }
  // refreshTile re-reads one card and puts it in the lane the server names; a 404 removes it.
  function refreshTile(id) {
    if (drag) { heldTiles[id] = true; return; }
    getHTML("/cards/" + encodeURIComponent(id) + "/tile").then(function (res) {
      var old = byData("data-card", id, $(".lanes"));
      if (res.status === 404) { if (old) old.remove(); laneCounts(); return; }
      if (!res.html) return;
      var node = fragment(res.html).firstElementChild;
      if (!node) return;
      var lane = $('.lane[data-lane="' + cssq(node.getAttribute("data-lane")) + '"]');
      if (!lane) { refreshMain(); return; }
      if (!old && lane.hasAttribute("data-hidden")) return; // most likely a done card the cap leaves out
      var before = old ? old.getAttribute("data-status") : null;
      var moved = !old || laneOf(old) !== lane;
      if (old && !moved) old.replaceWith(node);
      else {
        if (old) old.remove();
        var first = $(".card[data-card]", lane);
        if (first) lane.insertBefore(node, first); else lane.appendChild(node);
      }
      var after = node.getAttribute("data-status");
      if (moved || before !== after) settle(node);
      if (old && before !== after && Date.now() > (quietUntil[id] || 0)) statusToast(node);
      laneCounts();
      if (hub.applySearch) hub.applySearch();
    });
  }
  function bindBoard() {
    if (hub.nav !== "board") return;
    on(document, "dragstart", function (e) {
      var card = e.target.closest ? e.target.closest(".card[data-card]") : null;
      if (!card) return;
      drag = { card: card, from: laneOf(card), next: card.nextElementSibling };
      card.classList.add("dragging");
      e.dataTransfer.effectAllowed = "move";
      try { e.dataTransfer.setData("text/plain", card.getAttribute("data-card")); } catch (_) { /* some browsers refuse custom data */ }
    });
    on(document, "dragover", function (e) {
      if (!drag) return;
      var lane = laneOf(e.target);
      if (!lane) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      $$(".lane.over").forEach(function (l) { if (l !== lane) l.classList.remove("over"); });
      lane.classList.add("over");
      placeSlot(lane, e.clientY);
    });
    on(document, "dragleave", function (e) {
      var lane = laneOf(e.target);
      if (lane && !lane.contains(e.relatedTarget)) lane.classList.remove("over");
    });
    on(document, "dragend", endDrag);
    on(document, "drop", function (e) {
      if (!drag) return;
      var lane = laneOf(e.target);
      if (!lane) return;
      e.preventDefault();
      var d = drag, card = d.card, id = card.getAttribute("data-card");
      function putBack() {
        if (d.next && d.next.parentNode === d.from) d.from.insertBefore(card, d.next); else d.from.appendChild(card);
        settle(card);
        laneCounts();
      }
      if (slot.parentNode === lane) lane.insertBefore(card, slot); else lane.appendChild(card);
      delete heldTiles[id]; // the move's own answer refreshes this tile; a held GET now could pull it back
      endDrag();
      if (lane === d.from) { putBack(); return; } // the order inside a lane is not stored
      settle(card);
      laneCounts();
      quietUntil[id] = Date.now() + 2500;
      moveCard(id, lane.getAttribute("data-lane"), {
        title: card.getAttribute("data-title"),
        active: lane.getAttribute("data-kind") === "active",
        ready: card.hasAttribute("data-ready")
      }, function (ok) {
        if (!ok) { putBack(); return; }
        quietUntil[id] = Date.now() + 2500; // a confirmed cancel-and-move answers later; its toast already spoke
        refreshTile(id);
      });
    });
  }
  starters.push(bindBoard);
  changeHandlers.push(function (c) {
    if (hub.nav !== "board" || !c.card || !$(".lanes")) return false;
    clearTimeout(tileTimers[c.card]);
    tileTimers[c.card] = setTimeout(function () { delete tileTimers[c.card]; refreshTile(c.card); }, 150);
    return true;
  });
  busyChecks.push(function () { return !!drag; });

  /* ---------- tag editor (the dialog here, the card page in Task 5) ---------- */
  function tagsOf(box) { return $$(".tag[data-tag]", box).map(function (t) { return t.getAttribute("data-tag"); }); }
  // wireTagbox: Enter or comma adds the typed tag, × removes one, Backspace in the empty input
  // removes the last. onEdit(tags) runs after each change the human makes; set() does not call it.
  function wireTagbox(box, onEdit) {
    var input = $("[data-tag-input]", box);
    function changed() { if (onEdit) onEdit(tagsOf(box)); }
    function chip(name) {
      var t = el("span", "tag" + (name === "agent-ready" ? " ready" : ""), name);
      t.setAttribute("data-tag", name);
      var x = el("button", "x", "×");
      x.type = "button";
      x.setAttribute("aria-label", "Remove tag " + name);
      t.appendChild(x);
      return t;
    }
    // add takes one tag or several separated by commas ("needs-spec, security" is two tags).
    function add(raw) {
      var added = false;
      String(raw).split(",").forEach(function (part) {
        var v = part.trim();
        if (!v || tagsOf(box).indexOf(v) >= 0) return;
        box.insertBefore(chip(v), input);
        added = true;
      });
      return added;
    }
    if (input) input.removeAttribute("name"); // with script the chips are the list
    on(box, "click", function (e) {
      var x = e.target.closest ? e.target.closest(".tag[data-tag] .x") : null;
      if (x) { e.preventDefault(); x.parentNode.remove(); changed(); if (input) input.focus(); return; }
      if (e.target === box && input) input.focus();
    });
    on(input, "input", function () {
      var v = input.value, cut = v.lastIndexOf(",");
      if (cut < 0) return;
      if (add(v.slice(0, cut))) changed();
      input.value = v.slice(cut + 1).replace(/^\s+/, "");
    });
    on(input, "keydown", function (e) {
      if (e.isComposing || e.keyCode === 229) return; // the IME owns Enter and comma while composing
      if (e.key === "Enter" || e.key === ",") {
        e.preventDefault(); // never submit the form from the tag input
        if (add(input.value)) changed();
        input.value = "";
      } else if (e.key === "Backspace" && !input.value) {
        var chips = $$(".tag[data-tag]", box);
        if (chips.length) { chips[chips.length - 1].remove(); changed(); }
      }
    });
    return {
      flush: function () { if (input && input.value.trim()) { if (add(input.value)) changed(); input.value = ""; } },
      set: function (tags) { // stored tags are authoritative: one chip each, never split
        $$(".tag[data-tag]", box).forEach(function (t) { t.remove(); });
        tags.forEach(function (name) { if (name) box.insertBefore(chip(name), input); });
      },
      tags: function () { return tagsOf(box); }
    };
  }

  /* ---------- new card dialog ---------- */
  var dlg = { scrim: null, opener: null, tags: null };
  function dialogOpen() { return !!(dlg.scrim && dlg.scrim.classList.contains("on")); }
  function setDialogError(msg) { var e = $("#nc-error"); if (e) { e.textContent = msg; e.hidden = !msg; } }
  function openNewCard(lane, opener) {
    if (!dlg.scrim) return;
    dlg.opener = opener || document.activeElement;
    var sel = $("#nc-lane", dlg.scrim), title = $("#nc-title", dlg.scrim);
    if (sel && sel.options.length) {
      var known = !!lane && Array.prototype.some.call(sel.options, function (o) { return o.value === lane; });
      if (known) sel.value = lane; else sel.selectedIndex = 0;
    }
    if (title) title.removeAttribute("aria-invalid");
    setDialogError("");
    dlg.scrim.classList.add("on");
    setTimeout(function () { var t = $("#nc-title", dlg.scrim); if (t) t.focus(); }, 30);
  }
  function closeNewCard() {
    if (!dlg.scrim) return;
    dlg.scrim.classList.remove("on");
    if (location.hash === "#new-card") history.replaceState(null, "", location.pathname + location.search);
    if (dlg.opener && dlg.opener.focus) dlg.opener.focus();
  }
  function trapTab(e, scope) {
    var f = $$("a[href],button:not([disabled]),input:not([type=hidden]),select,textarea", scope).filter(function (n) { return n.offsetParent !== null; });
    if (!f.length) return;
    var first = f[0], last = f[f.length - 1];
    if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last.focus(); }
    else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first.focus(); }
  }
  function flagField(input) {
    input.classList.add("bad");
    input.focus();
    setTimeout(function () { input.classList.remove("bad"); }, 900);
  }
  function bindNewCard() {
    var s = $("#new-card");
    if (!s) return;
    dlg.scrim = s;
    var form = $("#nc-form", s), ready = $("#nc-ready", s), create = $("#nc-create", s);
    form.noValidate = true; // with script the dialog checks the title itself (flagField); without, `required` does
    dlg.tags = wireTagbox($("[data-tagbox]", s), function (tags) { if (tags.indexOf("agent-ready") >= 0) ready.checked = true; });
    on(document, "click", function (e) {
      var opener = e.target.closest ? e.target.closest('a[href="#new-card"]') : null;
      if (!opener) return;
      e.preventDefault();
      openNewCard(opener.getAttribute("data-new"), opener);
    });
    $$("[data-close]", s).forEach(function (a) { on(a, "click", function (e) { e.preventDefault(); closeNewCard(); }); });
    on(s, "click", function (e) { if (e.target === s) closeNewCard(); });
    on(document, "keydown", function (e) {
      if (!dialogOpen()) return;
      if (e.key === "Escape") { e.preventDefault(); closeNewCard(); }
      else if (e.key === "Tab") trapTab(e, s);
    });
    on($("#nc-title", s), "input", function () { this.removeAttribute("aria-invalid"); setDialogError(""); });
    on(form, "submit", function (e) {
      e.preventDefault();
      dlg.tags.flush();
      var title = $("#nc-title", s).value.trim();
      if (!title) {
        var ti = $("#nc-title", s);
        ti.setAttribute("aria-invalid", "true");
        setDialogError("Title is required.");
        flagField(ti);
        return;
      }
      var tags = dlg.tags.tags();
      if (ready.checked && tags.indexOf("agent-ready") < 0) tags.push("agent-ready");
      var card = { title: title, body: $("#nc-body", s).value, kind: $("#nc-kind", s).value, lane: $("#nc-lane", s).value,
        project_dir: $("#nc-project", s).value.trim(), parent_id: $("#nc-parent", s).value, tags: tags };
      create.disabled = true;
      setDialogError("");
      api("POST", "/api/cards", { card: card }).then(function (res) {
        create.disabled = false;
        if (!res.ok) { setDialogError(errText(res)); return; }
        var c = res.data;
        form.reset();
        dlg.tags.set([]);
        closeNewCard();
        if (hub.nav === "board" && $(".lanes")) {
          refreshTile(c.id);
          if (c.locked_by_run) toast([pill("queued"), el("span", null, c.title + ": a run starts on the next tick")]);
          else toast([el("span", null, "Created in " + laneTitle(c.lane) + ".")]);
        } else {
          location.href = "/cards/" + encodeURIComponent(c.id) + "?ws=" + encodeURIComponent(hub.ws) + "&flash=added";
        }
      });
    });
    if (location.hash === "#new-card") openNewCard();
  }
  starters.push(bindNewCard);
  busyChecks.push(dialogOpen);

  /* ---------- card page: tags and comments ---------- */
  function clockNow() { var d = new Date(); return ("0" + d.getHours()).slice(-2) + ":" + ("0" + d.getMinutes()).slice(-2); }
  function bindTags() {
    var form = $("form[data-tags-card]");
    if (!form) return;
    var id = form.getAttribute("data-tags-card"), hidden = $('input[name="tags"]', form);
    var confirmed = tagsOf($("[data-tagbox]", form)), seq = 0; // the last list the server confirmed
    var tb = wireTagbox($("[data-tagbox]", form), function (tags) {
      var mine = ++seq;
      hidden.value = tags.join(",");
      api("POST", "/api/cards/" + encodeURIComponent(id), { tags: tags }).then(function (res) {
        if (mine !== seq) return; // a newer edit is in flight: only its response counts
        if (!res.ok) {
          hidden.value = confirmed.join(",");
          tb.set(confirmed);
          toast([el("span", null, errText(res))]);
          return;
        }
        confirmed = res.data.tags || [];
        hidden.value = confirmed.join(",");
        tb.set(confirmed); // the stored, masked spelling wins
        toast([el("span", null, "Tags saved.")]);
      });
    });
    on(form, "submit", function (e) { e.preventDefault(); tb.flush(); });
  }
  function appendComment(c) {
    var list = $("#comments");
    if (!list) return;
    var none = $("#no-comments");
    if (none) none.remove();
    var one = el("div", "one new"), who = el("div", "who");
    who.appendChild(el("b", null, c.author));
    who.appendChild(el("span", "k", c.author_kind));
    who.appendChild(el("span", null, clockNow()));
    one.appendChild(who);
    one.appendChild(el("div", "txt", c.body)); // plain text until the next refresh renders the markdown
    list.appendChild(one);
  }
  function bindComposer() {
    var form = $("form[data-comment-card]");
    if (!form) return;
    var id = form.getAttribute("data-comment-card"), ta = $("textarea", form), btn = $('button[type="submit"]', form);
    function post() {
      var body = ta.value.trim();
      if (!body) { ta.focus(); return; }
      btn.disabled = true;
      api("POST", "/api/cards/" + encodeURIComponent(id) + "/comments", { body: body }).then(function (res) {
        btn.disabled = false;
        if (!res.ok) { toast([el("span", null, errText(res))]); return; }
        appendComment(res.data);
        ta.value = "";
        toast([el("span", null, "Comment posted and masked for secrets.")]);
      });
    }
    on(form, "submit", function (e) { e.preventDefault(); post(); });
    on(ta, "keydown", function (e) { if ((e.ctrlKey || e.metaKey) && e.key === "Enter") { e.preventDefault(); post(); } });
    on($("[data-clear]", form), "click", function (e) { e.preventDefault(); ta.value = ""; ta.focus(); });
  }
  mainBinders.push(bindTags, bindComposer);
  // an unsent comment or a half-typed tag survives a live refresh through unsavedEdits()

  /* ---------- page sections: Tasks 3, 4 and 5 insert theirs above this line ---------- */

  function start() {
    var b = document.body;
    hub.ws = b.getAttribute("data-ws") || "";
    hub.run = b.getAttribute("data-run") || "";
    hub.nav = b.getAttribute("data-nav") || "";
    if (window.history && history.replaceState && /[?&]flash=/.test(location.search)) {
      var u = new URL(location.href);
      u.searchParams.delete("flash");
      history.replaceState(history.state, "", u.pathname + u.search + u.hash);
    }
    starters.forEach(function (fn) { fn(); });
    bindMain();
    connect();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
