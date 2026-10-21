(function () {
  "use strict";
  var body = document.body;
  var ws = body.dataset.ws || "";
  var run = body.dataset.run || "";
  document.documentElement.classList.add("js");

  function query(extra) {
    var p = new URLSearchParams();
    if (ws) p.set("ws", ws);
    Object.keys(extra || {}).forEach(function (k) { p.set(k, extra[k]); });
    return p.toString();
  }

  function typing() {
    var a = document.activeElement;
    return a && /^(TEXTAREA|INPUT|SELECT)$/.test(a.tagName) && a.closest("form");
  }

  var timer = null;
  function refresh() {
    if (timer) return;
    timer = setTimeout(function () {
      timer = null;
      if (typing()) return; // never clobber what the human is typing; the next change refreshes
      var u = new URL(location.href);
      u.searchParams.set("fragment", "main");
      u.searchParams.delete("flash");
      fetch(u, { credentials: "same-origin" })
        .then(function (r) { return r.ok ? r.text() : null; })
        .then(function (html) {
          var m = document.getElementById("main");
          if (html !== null && m) { m.innerHTML = html; bind(); }
        });
    }, 300);
  }

  function appendLog(text) {
    var pre = document.getElementById("log");
    if (!pre) return;
    pre.textContent += text;
    pre.scrollTop = pre.scrollHeight;
  }

  function connect() {
    if (!window.EventSource) return;
    var extra = {};
    if (run) {
      extra.run = run;
      var pre = document.getElementById("log");
      if (pre) extra.from = pre.dataset.from || "0";
    }
    var es = new EventSource("/events?" + query(extra));
    es.addEventListener("change", refresh);
    es.addEventListener("log", function (e) {
      try { appendLog(JSON.parse(e.data).text); } catch (_) { /* ignore a malformed frame */ }
    });
  }

  function move(card, lane, cancel) {
    var data = new URLSearchParams();
    data.set("lane", lane);
    if (cancel) data.set("cancel_run", "1");
    return fetch("/cards/" + encodeURIComponent(card) + "/move?" + query(), {
      method: "POST", body: data, credentials: "same-origin", headers: { "X-TQ-Hub-Fetch": "1" }
    }).then(function (r) {
      if (r.status === 409) {
        return r.text().then(function (msg) {
          if (window.confirm(msg + "\n\nCancel the run and move the card?")) return move(card, lane, true);
        });
      }
      if (!r.ok) return r.text().then(function (msg) { window.alert(msg); });
      refresh();
    });
  }

  function bind() {
    document.querySelectorAll(".card[draggable]").forEach(function (el) {
      el.addEventListener("dragstart", function (e) { e.dataTransfer.setData("text/plain", el.dataset.card); });
    });
    document.querySelectorAll(".lane").forEach(function (lane) {
      lane.addEventListener("dragover", function (e) { e.preventDefault(); lane.classList.add("over"); });
      lane.addEventListener("dragleave", function () { lane.classList.remove("over"); });
      lane.addEventListener("drop", function (e) {
        e.preventDefault();
        lane.classList.remove("over");
        var id = e.dataTransfer.getData("text/plain");
        if (id) move(id, lane.dataset.lane, false);
      });
    });
    document.querySelectorAll("form[data-move]").forEach(function (f) {
      f.addEventListener("submit", function (e) {
        e.preventDefault();
        move(f.dataset.card, f.querySelector("[name=lane]").value, false);
      });
    });
  }

  // the workspace switcher lives in the header, outside #main: bound once
  document.querySelectorAll("select[data-autosubmit]").forEach(function (sel) {
    sel.addEventListener("change", function () { sel.form.submit(); });
  });

  bind();
  connect();
})();
