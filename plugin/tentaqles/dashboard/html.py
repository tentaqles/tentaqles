"""Embedded single-page dashboard HTML.

No external CDNs — consistent with the offline-first design used by
_vendor_vis_network.min.js. System fonts only; CSS grid layout.
"""

from __future__ import annotations

DASHBOARD_HTML: str = r"""<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Tentaqles Dashboard</title>
<style>
  :root {
    --bg: #0e1116;
    --panel: #151a22;
    --panel-2: #1b222c;
    --border: #232b36;
    --fg: #e6edf3;
    --fg-dim: #8b949e;
    --accent: #4dd0e1;
    --good: #5fd787;
    --warn: #ffb454;
    --bad: #ff6e6e;
  }
  * { box-sizing: border-box; }
  html, body {
    margin: 0;
    padding: 0;
    background: var(--bg);
    color: var(--fg);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto,
      "Helvetica Neue", Arial, "Noto Sans", sans-serif;
    font-size: 14px;
    line-height: 1.4;
  }
  header {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    padding: 18px 28px;
    border-bottom: 1px solid var(--border);
    background: var(--panel);
  }
  header h1 {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    letter-spacing: 0.3px;
  }
  header h1 .dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--accent);
    margin-right: 10px;
    vertical-align: middle;
    box-shadow: 0 0 8px var(--accent);
  }
  header .meta {
    font-size: 12px;
    color: var(--fg-dim);
    font-variant-numeric: tabular-nums;
  }
  main {
    padding: 24px 28px;
  }
  .grid {
    display: grid;
    gap: 18px;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
  }
  .card {
    background: var(--panel);
    border: 1px solid var(--border);
    border-radius: 8px;
    padding: 16px 18px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .card h2 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: var(--fg);
  }
  .card .root {
    font-size: 11px;
    color: var(--fg-dim);
    font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
    word-break: break-all;
  }
  .stats {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 6px;
  }
  .stat {
    background: var(--panel-2);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 8px 6px;
    text-align: center;
  }
  .stat .num {
    font-size: 18px;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    color: var(--fg);
  }
  .stat .label {
    font-size: 10px;
    color: var(--fg-dim);
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }
  .section-title {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.7px;
    color: var(--fg-dim);
    margin: 4px 0 2px;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  ul li {
    padding: 4px 0;
    border-bottom: 1px dashed var(--border);
    font-size: 12px;
    display: flex;
    justify-content: space-between;
    gap: 8px;
  }
  ul li:last-child { border-bottom: none; }
  .node-id {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--fg);
    font-family: "SFMono-Regular", Consolas, Menlo, monospace;
  }
  .score {
    color: var(--fg-dim);
    font-variant-numeric: tabular-nums;
  }
  .trend {
    display: inline-block;
    width: 14px;
    text-align: center;
  }
  .trend.rising { color: var(--good); }
  .trend.falling { color: var(--bad); }
  .trend.stable { color: var(--fg-dim); }
  .pri {
    display: inline-block;
    padding: 1px 6px;
    border-radius: 3px;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.4px;
  }
  .pri.critical { background: var(--bad); color: #000; }
  .pri.high { background: var(--warn); color: #000; }
  .pri.medium { background: var(--panel-2); color: var(--fg-dim); border: 1px solid var(--border); }
  .pri.low { background: transparent; color: var(--fg-dim); border: 1px solid var(--border); }
  .empty { color: var(--fg-dim); font-style: italic; font-size: 11px; }
  .badge {
    display: inline-block;
    padding: 1px 6px;
    background: var(--panel-2);
    border: 1px solid var(--border);
    border-radius: 10px;
    color: var(--fg-dim);
    font-size: 10px;
    margin-left: 6px;
  }
  #status.connected { color: var(--good); }
  #status.disconnected { color: var(--bad); }
</style>
</head>
<body>
  <header>
    <h1><span class="dot"></span>Tentaqles Dashboard</h1>
    <div class="meta">
      <span id="status" class="disconnected">connecting...</span>
      <span class="badge" id="updated">--</span>
    </div>
  </header>
  <main>
    <div id="grid" class="grid"></div>
  </main>

<script>
(function () {
  var TREND_ARROW = { rising: "\u2191", falling: "\u2193", stable: "\u2192" };
  var gridEl = document.getElementById("grid");
  var updatedEl = document.getElementById("updated");
  var statusEl = document.getElementById("status");

  function esc(s) {
    if (s === null || s === undefined) return "";
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function render(snap) {
    if (!snap) return;
    if (snap.generated_at) {
      var d = new Date(snap.generated_at);
      updatedEl.textContent = "updated " + d.toLocaleTimeString();
    }
    var ws = snap.workspaces || [];
    if (ws.length === 0) {
      gridEl.innerHTML = '<div class="card"><div class="empty">No workspaces registered yet.</div></div>';
      return;
    }
    var html = "";
    for (var i = 0; i < ws.length; i++) {
      var w = ws[i];
      var stats = w.stats || {};
      var hot = w.hot_nodes || [];
      var pending = w.open_pending || [];
      var last = w.last_active ? new Date(w.last_active).toLocaleString() : "never";

      html += '<div class="card">';
      html += '<h2>' + esc(w.display_name || w.id) + '</h2>';
      html += '<div class="root">' + esc(w.root_path) + '</div>';
      html += '<div class="stats">';
      html += '<div class="stat"><div class="num">' + (stats.sessions || 0) + '</div><div class="label">sessions</div></div>';
      html += '<div class="stat"><div class="num">' + (stats.touches || 0) + '</div><div class="label">touches</div></div>';
      html += '<div class="stat"><div class="num">' + (stats.active_decisions || 0) + '</div><div class="label">decisions</div></div>';
      html += '<div class="stat"><div class="num">' + (stats.open_pending || 0) + '</div><div class="label">pending</div></div>';
      html += '</div>';

      html += '<div><div class="section-title">Hot nodes</div>';
      if (hot.length === 0) {
        html += '<div class="empty">none</div>';
      } else {
        html += '<ul>';
        for (var j = 0; j < hot.length; j++) {
          var h = hot[j];
          var t = h.trend || "stable";
          var arrow = TREND_ARROW[t] || "\u2192";
          html += '<li>';
          html += '<span class="node-id">' + esc(h.node_id) + '</span>';
          html += '<span class="score"><span class="trend ' + esc(t) + '">' + arrow + '</span> ' + (h.score || 0).toFixed(2) + '</span>';
          html += '</li>';
        }
        html += '</ul>';
      }
      html += '</div>';

      html += '<div><div class="section-title">Open pending</div>';
      if (pending.length === 0) {
        html += '<div class="empty">none</div>';
      } else {
        html += '<ul>';
        for (var k = 0; k < pending.length; k++) {
          var p = pending[k];
          var pri = p.priority || "medium";
          html += '<li>';
          html += '<span class="node-id">' + esc(p.description) + '</span>';
          html += '<span class="pri ' + esc(pri) + '">' + esc(pri) + '</span>';
          html += '</li>';
        }
        html += '</ul>';
      }
      html += '</div>';

      html += '<div class="root">last active: ' + esc(last) + '</div>';
      html += '</div>';
    }
    gridEl.innerHTML = html;
  }

  function fetchSnapshot() {
    var xhr = new XMLHttpRequest();
    xhr.open("GET", "/api/snapshot", true);
    xhr.onreadystatechange = function () {
      if (xhr.readyState === 4 && xhr.status === 200) {
        try { render(JSON.parse(xhr.responseText)); } catch (e) {}
      }
    };
    xhr.send();
  }

  var es = null;
  var reconnectDelay = 1000;
  function connect() {
    try {
      es = new EventSource("/api/events");
    } catch (e) {
      statusEl.className = "disconnected";
      statusEl.textContent = "offline";
      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 15000);
      return;
    }
    es.onopen = function () {
      statusEl.className = "connected";
      statusEl.textContent = "live";
      reconnectDelay = 1000;
    };
    es.onmessage = function (ev) {
      try {
        var data = JSON.parse(ev.data);
        if (data && data.type === "shutdown") {
          statusEl.className = "disconnected";
          statusEl.textContent = "server stopped";
          es.close();
          return;
        }
        if (data && data.workspaces) {
          render(data);
        } else if (data && data.type === "snapshot" && data.snapshot) {
          render(data.snapshot);
        }
      } catch (e) {}
    };
    es.onerror = function () {
      statusEl.className = "disconnected";
      statusEl.textContent = "reconnecting...";
      try { es.close(); } catch (e) {}
      setTimeout(connect, reconnectDelay);
      reconnectDelay = Math.min(reconnectDelay * 2, 15000);
    };
  }

  fetchSnapshot();
  connect();
})();
</script>
</body>
</html>
"""
