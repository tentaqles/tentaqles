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
  function busy() {
    if (typing()) return true;
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
  function connect() {
    if (!window.EventSource) return;
    var p = new URLSearchParams();
    if (hub.ws) p.set("ws", hub.ws);
    var log = $("#log");
    if (hub.run && log) { p.set("run", hub.run); p.set("from", log.getAttribute("data-from") || "0"); }
    var es = new EventSource("/events?" + p.toString());
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
        moveCard(f.getAttribute("data-card"), sel.value, { title: f.getAttribute("data-title") }, function (ok) { if (ok) refreshMain(); });
      });
    });
  }
  mainBinders.push(bindMoveForms);

  /* ---------- theme toggle and search ---------- */
  function bindTheme() {
    var btn = $("#themebtn");
    if (!btn) return;
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

  /* ---------- page sections: Tasks 3, 4 and 5 insert theirs above this line ---------- */

  function start() {
    var b = document.body;
    hub.ws = b.getAttribute("data-ws") || "";
    hub.run = b.getAttribute("data-run") || "";
    hub.nav = b.getAttribute("data-nav") || "";
    starters.forEach(function (fn) { fn(); });
    bindMain();
    connect();
  }
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
