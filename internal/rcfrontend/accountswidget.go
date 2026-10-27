// accountswidget.go — the top-right account switcher the web UI shows.
//
// # What it is
//
// The widget is the web UI's answer to the account switcher in the top
// right of antigravity.google.com, and the web UI's mirror of the
// TUI's /accounts panel: it lists the registered accounts, switches to
// another one, adds one and removes one.
//
// # How it is mounted
//
// It is *not* a patch of the bundle. The bundle is minified and its
// identifiers move on every release (docs/guide/reference-drift.md), so
// a rewrite of it breaks silently; the widget instead attaches itself
// to the DOM the bundle renders, using the stable test id the titlebar
// carries:
//
//	[data-testid="pane-titlebar-toolbar"]
//
// When that element is not there — a phone layout, a bundle that
// renamed the slot — the widget falls back to a fixed button in the
// top-right corner, so it is never simply missing.
//
// # What it talks to
//
// The endpoints in accountsapi.go, on the same origin as the page. The
// script carries the server's per-process token (see
// accountsWidgetHTML), because the endpoint mutates the credential.
//
// # The look
//
// The colours are the app's own CSS variables (--background,
// --foreground, --border, --muted), so the widget follows the app's
// theme instead of guessing one. The compiled Tailwind sheet only
// carries the classes the app itself uses, so the widget brings its
// own small stylesheet rather than borrowing class names that a
// rebuild would drop.
package rcfrontend

import "strings"

// accountsTokenPlaceholder is replaced with the server's token when
// the widget's script is rendered.
//
// The script is a template rather than a format string: it carries
// percent signs, and a format string would read every one of them as
// a verb.
const accountsTokenPlaceholder = "__AGY_ACCOUNTS_TOKEN__"

// accountsWidgetHTML returns the style and script blocks that render
// the account switcher.
//
// The token is written into the script, and the script is written into
// the page the server serves: that is what makes the API's token check
// mean "the caller loaded this server's page".
func accountsWidgetHTML(token string) string {
	script := strings.ReplaceAll(accountsWidgetScript, accountsTokenPlaceholder, token)
	return "<style>" + accountsWidgetCSS + "</style>\n<script>" + script + "</script>"
}

// accountsWidgetCSS is the widget's stylesheet.
//
// The colours are the app's own variables, so the widget follows the
// app's theme; the fallbacks are the app's dark values, for a page
// that has not loaded the sheet yet.
const accountsWidgetCSS = `
.agy-accounts-host{display:inline-flex;position:relative;align-items:center}
.agy-accounts-fixed{position:fixed;top:6px;right:10px;z-index:2147483000}
.agy-accounts-button{width:26px;height:26px;border-radius:999px;display:inline-flex;align-items:center;justify-content:center;border:1px solid var(--border,#191919);background:var(--muted,#151515);color:var(--foreground,#ccc);font:600 12px/1 system-ui,-apple-system,sans-serif;cursor:pointer;padding:0}
.agy-accounts-button:hover{background:var(--secondary,#222);color:var(--foreground,#ccc)}
.agy-accounts-panel{position:fixed;width:min(380px,80vw);padding:8px;border-radius:10px;border:1px solid var(--border,#191919);background:var(--background,#101010);color:var(--foreground,#ccc);box-shadow:0 12px 32px rgba(0,0,0,.45);z-index:2147483000;font:12px/1.45 system-ui,-apple-system,sans-serif;text-align:left}
.agy-accounts-head{padding:4px 6px 8px;color:var(--muted-foreground,#6e6e6e);font-size:11px;word-break:break-all}
/* The list scrolls on its own: the panel is fixed, so the page cannot
   scroll it, and the wheel must not reach the page when the list is
   already at its end (overscroll-behavior). */
.agy-accounts-list{display:flex;flex-direction:column;gap:2px;max-height:min(320px,50vh);overflow:auto;overscroll-behavior:contain}
.agy-accounts-row{display:flex;align-items:flex-start;gap:4px;border-radius:8px;padding:2px 4px}
.agy-accounts-row:hover{background:var(--muted,#151515)}
/* The active account is marked the way the app marks a selected item:
   the app's own selected surface (the token the app paints a selected
   conversation with) behind the address, and the full foreground colour
   for it. The other addresses sit at the muted colour, so the eye finds
   the active one without a badge, a border or a colour of its own. The
   surface covers the address alone: the quota below it is a reading, not
   a selection. */
.agy-accounts-row.is-active .agy-accounts-row-main{background:var(--sidebar-secondary,#2b2b2b);border-radius:6px}
.agy-accounts-row-body{flex:1;min-width:0;display:flex;flex-direction:column;gap:3px;align-items:stretch}
.agy-accounts-row-main{display:flex;flex-direction:column;gap:1px;align-items:flex-start;background:none;border:0;padding:4px 2px;color:inherit;font:inherit;cursor:pointer;text-align:left;min-width:0;width:100%}
.agy-accounts-email{max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted-foreground,#6e6e6e)}
.agy-accounts-row.is-active .agy-accounts-email{color:var(--foreground,#ccc)}
.agy-accounts-tier{font-size:10px;color:var(--primary,#007acc)}
.agy-accounts-quota{font-size:10px;color:var(--muted-foreground,#6e6e6e);font-variant-numeric:tabular-nums}
/* The quota: one column per window, side by side — the weekly limit on
   the left, the five hour limit on the right — so the left column reads
   the week's remainder and the right one the five hours'. The bar's
   colours are the app's own gauge thresholds (red under 10%, yellow
   under 25%, green above). */
.agy-accounts-quota-list{display:flex;flex-direction:column;gap:6px;margin:0 2px 4px}
.agy-accounts-quota-group{display:flex;flex-direction:column;gap:4px}
.agy-accounts-quota-row{display:flex;align-items:flex-start;gap:12px}
.agy-accounts-quota-item{flex:1 1 0;min-width:0;display:flex;flex-direction:column;gap:3px}
.agy-accounts-quota-label{font-size:10px;color:var(--muted-foreground,#6e6e6e);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.agy-accounts-quota-pct{font-size:13px;font-weight:600;line-height:1.1;color:var(--foreground,#ccc);font-variant-numeric:tabular-nums}
.agy-accounts-quota-bar{height:5px;border-radius:999px;background:var(--muted,#151515);overflow:hidden}
.agy-accounts-quota-fill{height:100%;border-radius:999px;background:#16a34a}
.agy-accounts-quota-fill.is-low{background:#ca8a04}
.agy-accounts-quota-fill.is-out{background:#dc2626}
.agy-accounts-quota-sub{font-size:10px;color:var(--muted-foreground,#6e6e6e);font-variant-numeric:tabular-nums}
.agy-accounts-group{font-size:9px;letter-spacing:.05em;text-transform:uppercase;color:var(--muted-foreground,#6e6e6e);padding:3px 0 0}
.agy-accounts-group-toggle{display:flex;align-items:baseline;justify-content:flex-start;gap:8px;width:100%;border:0;background:none;color:var(--muted-foreground,#6e6e6e);font:inherit;font-size:9px;letter-spacing:.05em;text-transform:uppercase;cursor:pointer;padding:3px 0 0;text-align:left}
.agy-accounts-group-toggle:hover{color:var(--foreground,#ccc)}
.agy-accounts-remove{flex:none;width:22px;height:22px;border-radius:6px;border:0;background:none;color:var(--muted-foreground,#6e6e6e);cursor:pointer;font-size:14px;line-height:1}
.agy-accounts-remove:hover{background:var(--muted,#151515);color:var(--foreground,#ccc)}
.agy-accounts-add-wrap{margin-top:6px;padding-top:6px;border-top:1px solid var(--border,#191919)}
.agy-accounts-add{width:100%;border:1px solid var(--border,#191919);border-radius:8px;background:var(--muted,#151515);color:var(--foreground,#ccc);padding:6px 8px;font:inherit;cursor:pointer}
.agy-accounts-add:hover{background:var(--secondary,#222)}
.agy-accounts-session{display:flex;flex-direction:column;gap:6px}
.agy-accounts-session-row{display:flex;gap:6px}
.agy-accounts-code-input,.agy-accounts-code{width:100%;box-sizing:border-box;border:1px solid var(--border,#191919);border-radius:6px;background:var(--background,#101010);color:var(--foreground,#ccc);padding:4px 6px;font:inherit}
.agy-accounts-code-input:focus{outline:1px solid var(--primary,#007acc)}
.agy-accounts-link{color:var(--primary,#007acc);text-decoration:none;word-break:break-all}
.agy-accounts-link:hover{text-decoration:underline}
.agy-accounts-code{font-size:11px;color:var(--muted-foreground,#6e6e6e);font-variant-numeric:tabular-nums}
.agy-accounts-note{margin-top:6px;padding:2px 6px;font-size:11px;color:var(--muted-foreground,#6e6e6e);word-break:break-word}
.agy-accounts-toast{position:fixed;top:44px;right:12px;z-index:2147483001;max-width:min(360px,80vw);padding:8px 12px;border-radius:10px;border:1px solid var(--border,#191919);background:var(--background,#101010);color:var(--foreground,#ccc);box-shadow:0 12px 32px rgba(0,0,0,.45);font:12px/1.45 system-ui,-apple-system,sans-serif}
.agy-accounts-toast.is-error{border-color:#7f1d1d;color:#fca5a5}
`

// accountsWidgetScript is the widget's script.
//
// It is plain DOM work: no framework, no bundle identifier, nothing
// that a release of the app can move out from under it.
const accountsWidgetScript = `(function () {
  "use strict";
  var TOKEN = "__AGY_ACCOUNTS_TOKEN__";
  var API = "/__agy/api/accounts";
  var QUOTA = API + "/quota";
  var SWITCH = API + "/switch";
  var REMOVE = API + "/remove";
  var ADD = API + "/add";
  var HOST_ID = "agy-accounts-host";
  var state = {
    open: false, list: null, note: "", loaded: false, pendingRemove: null,
    session: null, usage: null, usageEmail: "", usageError: "",
    // expanded maps an account's address to whether its other quota
    // groups (Claude and GPT models) are open.
    expanded: {}
  };

  function el(tag, cls, text) {
    var node = document.createElement(tag);
    if (cls) { node.className = cls; }
    if (text !== undefined && text !== null) { node.textContent = text; }
    return node;
  }

  function request(url, payload) {
    return fetch(url, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json", "X-Agy-Accounts-Token": TOKEN },
      body: JSON.stringify(payload || {})
    }).then(function (res) {
      return res.json().catch(function () { return {}; }).then(function (data) {
        if (!res.ok) {
          throw new Error(data && data.error ? data.error : ("HTTP " + res.status));
        }
        return data;
      });
    });
  }

  function toolbar() {
    return document.querySelector('[data-testid="pane-titlebar-toolbar"]');
  }

  // A toast is the answer to "did it work?": the panel's note is gone
  // as soon as the panel closes, and the page can be reloaded between
  // the click and the answer.
  function toast(message, kind) {
    var old = document.getElementById("agy-accounts-toast");
    if (old) { old.remove(); }
    var node = el("div", "agy-accounts-toast" + (kind ? " is-" + kind : ""), message);
    node.id = "agy-accounts-toast";
    node.setAttribute("role", "status");
    document.body.appendChild(node);
    setTimeout(function () { if (node.isConnected) { node.remove(); } }, 8000);
  }

  // setElementText rewrites an element's own text node.
  //
  // The node is rewritten, not the element: React owns the node it
  // rendered, and replacing it would leave React removing a child that
  // is no longer there.
  function setElementText(node, text) {
    var child = node.firstChild;
    if (child && child.nodeType === 3) { child.nodeValue = text; return; }
    if (!child) { node.appendChild(document.createTextNode(text)); }
  }

  // emailTextNode returns the first text node under an element that
  // holds an email address.
  function emailTextNode(root) {
    var walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    var n;
    while ((n = walker.nextNode())) {
      if (n.nodeValue && n.nodeValue.indexOf("@") > 0) { return n; }
    }
    return null;
  }

  // syncAccountText puts the live account's address where the app
  // shows it.
  //
  // The app's Settings renders the address it was told at start-up: the
  // GetUserStatus RPC answers a snapshot of the account (that seam
  // lives in internal/remotecontrol), so after a switch the panel would
  // keep showing the old address. The widget puts the live one back, in
  // the two places the app renders it.
  function syncAccountText() {
    var email = state.list && state.list.active ? state.list.active : "";
    if (!email) { return; }
    var chip = document.querySelector('[data-testid="settings-nav-item-Account"]');
    if (chip) {
      var spans = chip.querySelectorAll("span");
      for (var i = 0; i < spans.length; i++) {
        var text = (spans[i].textContent || "").trim();
        if (text.indexOf("@") > 0 && text !== email) {
          setElementText(spans[i], email);
        }
      }
    }
    var labels = document.querySelectorAll("label");
    for (var j = 0; j < labels.length; j++) {
      var node = emailTextNode(labels[j]);
      if (!node) { continue; }
      var match = node.nodeValue.match(/[^\s@]+@[^\s@]+/);
      if (!match) { continue; }
      node.nodeValue = node.nodeValue.slice(0, match.index) + email +
        node.nodeValue.slice(match.index + match[0].length);
    }
  }

  // refreshCountdown renders the countdown to the next reset, the way
  // the TUI's /usage panel renders it: hours and minutes, and the word
  // "Refreshes".
  function refreshCountdown(resetAt) {
    if (!resetAt) { return ""; }
    var at = Date.parse(resetAt);
    if (!at) { return ""; }
    var diff = at - Date.now();
    if (diff <= 0) { return "Refreshes in <1m"; }
    var mins = Math.floor(diff / 60000);
    if (mins < 1) { return "Refreshes in <1m"; }
    if (mins < 60) { return "Refreshes in " + mins + "m"; }
    return "Refreshes in " + Math.floor(mins / 60) + "h " + (mins % 60) + "m";
  }

  // percentText renders the fraction that is left as a percentage.
  function percentText(fraction) {
    if (typeof fraction !== "number" || !isFinite(fraction)) { return ""; }
    return (Math.round(fraction * 1000) / 10).toFixed(1) + "%";
  }

  // quotaFillClass picks the bar's colour with the app's own gauge
  // thresholds: red under ten percent, yellow under a quarter, green
  // above.
  function quotaFillClass(fraction) {
    if (typeof fraction !== "number") { return ""; }
    if (fraction < 0.1) { return "is-out"; }
    if (fraction < 0.25) { return "is-low"; }
    return "is-ok";
  }

  // quotaWidth clamps the bar's width to the fraction that is left.
  function quotaWidth(fraction) {
    if (typeof fraction !== "number" || !isFinite(fraction)) { return "0%"; }
    var value = Math.max(0, Math.min(1, fraction));
    return (Math.round(value * 1000) / 10) + "%";
  }

  // usageFor returns an account's quota: the live read for the active
  // account, the registry's snapshot for the others.
  function usageFor(acc) {
    if (!acc) { return null; }
    if (state.list && acc.email === state.list.active && state.usage) {
      return state.usage;
    }
    return acc.usage || null;
  }

  // loadUsage reads an account's remaining quota.
  function loadUsage(email) {
    if (!email) { return; }
    if (state.usageEmail === email && (state.usage || state.usageError)) { return; }
    state.usageEmail = email;
    request(QUOTA, { email: email }).then(function (data) {
      state.usage = data.usage || null;
      state.usageError = data.error || "";
      render();
    }).catch(function (err) {
      state.usage = null;
      state.usageError = err.message;
      render();
    });
  }

  function ensureHost() {
    var host = document.getElementById(HOST_ID);
    var bar = toolbar();
    if (host && host.isConnected) {
      if (bar && !bar.contains(host)) {
        host.className = "agy-accounts-host";
        bar.insertBefore(host, bar.firstChild);
      }
      return host;
    }
    host = el("div", "agy-accounts-host");
    host.id = HOST_ID;
    if (bar) {
      bar.insertBefore(host, bar.firstChild);
    } else {
      host.className = "agy-accounts-host agy-accounts-fixed";
      document.body.appendChild(host);
    }
    return host;
  }

  function renderButton(host, payload) {
    var btn = host.querySelector(".agy-accounts-button");
    if (!btn) {
      btn = el("button", "agy-accounts-button");
      btn.type = "button";
      btn.setAttribute("aria-haspopup", "menu");
      btn.addEventListener("click", function (ev) {
        ev.stopPropagation();
        toggle();
      });
      host.appendChild(btn);
    }
    var active = payload && payload.active ? payload.active : "";
    var label = active ? ("Accounts - " + active) : "Accounts";
    var initial = active ? active.charAt(0).toUpperCase() : "?";
    if (btn.textContent !== initial) { btn.textContent = initial; }
    btn.title = label;
    btn.setAttribute("aria-label", label);
  }

  function toggle() {
    state.open = !state.open;
    state.pendingRemove = null;
    render();
    if (state.open && !state.loaded) { refresh(); }
  }

  // The panel lives in the body, not in the titlebar: the titlebar
  // clips its overflow, so a panel inside it would be cut off after
  // the first row.
  function ensurePanel() {
    var panel = document.getElementById("agy-accounts-panel");
    if (!panel) {
      panel = el("div", "agy-accounts-panel");
      panel.id = "agy-accounts-panel";
      panel.setAttribute("role", "menu");
      panel.addEventListener("click", function (ev) { ev.stopPropagation(); });
      document.body.appendChild(panel);
    }
    return panel;
  }

  function closePanel() {
    var panel = document.getElementById("agy-accounts-panel");
    if (panel) { panel.remove(); }
  }

  function positionPanel(panel, btn) {
    if (!btn) { return; }
    var r = btn.getBoundingClientRect();
    var w = panel.offsetWidth;
    var h = panel.offsetHeight;
    var left = r.right - w;
    if (left < 8) { left = 8; }
    if (left + w > window.innerWidth - 8) { left = Math.max(8, window.innerWidth - w - 8); }
    var top = r.bottom + 6;
    if (top + h > window.innerHeight - 8) { top = Math.max(8, window.innerHeight - h - 8); }
    panel.style.left = left + "px";
    panel.style.top = top + "px";
  }

  function render() {
    var host = ensureHost();
    renderButton(host, state.list);
    if (!state.open) { closePanel(); return; }
    var panel = ensurePanel();
    // The list keeps its scroll position across a re-render: the panel
    // is rebuilt from scratch, and a list that jumps back to the top
    // cannot be scrolled down (the scroll event itself triggers the
    // re-render).
    var oldList = panel.querySelector(".agy-accounts-list");
    var keepTop = oldList ? oldList.scrollTop : 0;
    panel.textContent = "";
    var payload = state.list;
    if (!payload) {
      panel.appendChild(el("div", "agy-accounts-note", "Loading..."));
    } else {
      panel.appendChild(el("div", "agy-accounts-head",
        payload.active ? ("Signed in as " + payload.active) : "Not signed in"));
      var list = el("div", "agy-accounts-list");
      var accounts = payload.accounts || [];
      if (!accounts.length) {
        list.appendChild(el("div", "agy-accounts-note", "No accounts yet."));
      }
      accounts.forEach(function (acc) { list.appendChild(renderRow(acc)); });
      panel.appendChild(list);
      panel.appendChild(renderAdd());
      if (keepTop) { list.scrollTop = keepTop; }
    }
    if (state.note) { panel.appendChild(el("div", "agy-accounts-note", state.note)); }
    positionPanel(panel, host.querySelector(".agy-accounts-button"));
  }

  // reposition moves the panel with its button.
  //
  // It does not rebuild the panel: a rebuild would drop the list's
  // scroll position, and the scroll event that moved the page would
  // land on a list that is back at its top.
  function reposition() {
    var panel = document.getElementById("agy-accounts-panel");
    if (!panel) { return; }
    var host = document.getElementById(HOST_ID);
    positionPanel(panel, host ? host.querySelector(".agy-accounts-button") : null);
  }

  function renderRow(acc) {
    var row = el("div", "agy-accounts-row" + (acc.active ? " is-active" : ""));
    var body = el("div", "agy-accounts-row-body");
    var main = el("button", "agy-accounts-row-main");
    main.type = "button";
    main.appendChild(el("span", "agy-accounts-email", acc.email));
    if (acc.planTier) { main.appendChild(el("span", "agy-accounts-tier", acc.planTier)); }
    main.addEventListener("click", function () { switchTo(acc.email); });
    body.appendChild(main);
    body.appendChild(renderUsage(acc));
    row.appendChild(body);
    var remove = el("button", "agy-accounts-remove", "×");
    remove.type = "button";
    remove.title = "Remove " + acc.email;
    remove.addEventListener("click", function () { removeAccount(acc.email); });
    row.appendChild(remove);
    return row;
  }

  // renderUsage draws an account's quota.
  //
  // The first group (the Gemini models) is open: its windows are two
  // columns side by side — the weekly limit on the left, the five hour
  // limit on the right — so the left column reads the week's remainder
  // and the right one the five hours'. The other groups (Claude and GPT
  // models) are behind a disclosure, so the list stays short when there
  // are many accounts; opening one lays its windows out the same way.
  function renderUsage(acc) {
    var usage = usageFor(acc);
    if (!usage || !usage.groups || !usage.groups.length) {
      if (acc.quota) { return el("div", "agy-accounts-quota", acc.quota); }
      return el("div");
    }
    var wrap = el("div", "agy-accounts-quota-list");
    var groups = usage.groups || [];
    var open = state.expanded[acc.email] === true;
    groups.forEach(function (group, index) {
      var expandable = index > 0;
      var block = el("div", "agy-accounts-quota-group");
      if (expandable) {
        block.appendChild(renderGroupToggle(acc, group, open));
      } else {
        block.appendChild(el("div", "agy-accounts-group", groupName(group)));
      }
      if (!expandable || open) {
        block.appendChild(renderBuckets(group));
      }
      wrap.appendChild(block);
    });
    return wrap;
  }

  // groupName renders a model's group name the way the app's own quota
  // panel does: in capitals.
  function groupName(group) {
    return (group.name || "Quota").toUpperCase();
  }

  // renderBuckets lays a group's windows out side by side: the weekly
  // limit in the left column, the five hour limit in the right one.
  function renderBuckets(group) {
    var row = el("div", "agy-accounts-quota-row");
    orderedBuckets(group).forEach(function (bucket) {
      row.appendChild(renderBucket(bucket));
    });
    return row;
  }

  // orderedBuckets orders the windows of a group: the weekly limit
  // first, the five hour limit second, so the columns read the same way
  // for every account. The windows the quota does not name come last,
  // in the order the server sent them.
  function orderedBuckets(group) {
    var rank = function (bucket) {
      if (bucket.window === "weekly") { return 0; }
      if (bucket.window === "5h") { return 1; }
      return 2;
    };
    return (group.buckets || []).slice().sort(function (a, b) {
      return rank(a) - rank(b);
    });
  }

  // renderGroupToggle draws the disclosure of a group that is not
  // open.
  //
  // The closed line carries no countdown: the bucket that resets first
  // is named in the group it belongs to, and the countdown is on the
  // bucket's own row once the group is open.
  function renderGroupToggle(acc, group, open) {
    var btn = el("button", "agy-accounts-group-toggle");
    btn.type = "button";
    btn.setAttribute("aria-expanded", open ? "true" : "false");
    var label = el("span", null, (open ? "▾ " : "▸ ") + groupName(group));
    btn.appendChild(label);
    btn.addEventListener("click", function (ev) {
      ev.stopPropagation();
      state.expanded[acc.email] = !state.expanded[acc.email];
      render();
    });
    return btn;
  }

  // renderBucket draws one quota window: the window's name, the
  // percentage that is left, the bar and the countdown.
  function renderBucket(bucket) {
    var item = el("div", "agy-accounts-quota-item");
    item.appendChild(el("div", "agy-accounts-quota-label",
      bucket.label || bucket.window || "Quota"));
    var pct = percentText(bucket.remainingFraction);
    if (pct) { item.appendChild(el("div", "agy-accounts-quota-pct", pct)); }
    var bar = el("div", "agy-accounts-quota-bar");
    var fill = el("div", "agy-accounts-quota-fill " + quotaFillClass(bucket.remainingFraction));
    fill.style.width = quotaWidth(bucket.remainingFraction);
    bar.appendChild(fill);
    item.appendChild(bar);
    var refresh = refreshCountdown(bucket.resetAt);
    if (refresh) { item.appendChild(el("div", "agy-accounts-quota-sub", refresh)); }
    return item;
  }

  function renderAdd() {
    var wrap = el("div", "agy-accounts-add-wrap");
    if (state.session) {
      wrap.appendChild(renderSession(state.session));
      return wrap;
    }
    var add = el("button", "agy-accounts-add", "Add account");
    add.type = "button";
    add.addEventListener("click", startSignin);
    wrap.appendChild(add);
    return wrap;
  }

  function renderSession(session) {
    var wrap = el("div", "agy-accounts-session");
    if (session.authUrl) {
      var link = el("a", "agy-accounts-link", "Open Google's sign-in page");
      link.href = session.authUrl;
      link.target = "_blank";
      link.rel = "noopener noreferrer";
      wrap.appendChild(link);
      wrap.appendChild(el("div", "agy-accounts-note",
        "Approve there, then paste the code Google shows you back here."));
    } else {
      wrap.appendChild(el("div", "agy-accounts-note", "Waiting for the sign-in page..."));
    }
    var input = el("input", "agy-accounts-code");
    input.type = "text";
    input.placeholder = "Paste the code";
    input.autocomplete = "off";
    input.spellcheck = false;
    wrap.appendChild(input);
    var row = el("div", "agy-accounts-session-row");
    var submit = el("button", "agy-accounts-add", "Sign in");
    submit.type = "button";
    submit.addEventListener("click", function () { finishSignin(input.value); });
    row.appendChild(submit);
    var cancel = el("button", "agy-accounts-add", "Cancel");
    cancel.type = "button";
    cancel.addEventListener("click", function () {
      state.session = null;
      render();
    });
    row.appendChild(cancel);
    wrap.appendChild(row);
    return wrap;
  }

  function switchTo(email) {
    state.note = "";
    state.pendingRemove = null;
    request(SWITCH, { email: email }).then(function (data) {
      state.list = data;
      state.note = "Now signed in as " + email + ".";
      // The new account's quota is not the old one's.
      state.usage = null;
      state.usageEmail = "";
      state.usageError = "";
      render();
      toast("Now signed in as " + email + ".");
      syncAccountText();
      loadUsage(email);
    }).catch(function (err) {
      state.note = err.message;
      render();
      toast(err.message, "error");
      refresh();
    });
  }

  function removeAccount(email) {
    if (state.pendingRemove !== email) {
      state.pendingRemove = email;
      state.note = "Remove " + email + "? Press remove again to confirm.";
      render();
      return;
    }
    state.pendingRemove = null;
    request(REMOVE, { email: email }).then(function (data) {
      state.list = data;
      state.note = "Removed " + email + ".";
      state.usage = null;
      state.usageEmail = "";
      state.usageError = "";
      render();
      toast(data.active ? ("Removed " + email + " — now signed in as " + data.active + ".") : ("Removed " + email + "."));
      syncAccountText();
    }).catch(function (err) {
      state.note = err.message;
      render();
      toast(err.message, "error");
    });
  }

  function startSignin() {
    state.note = "";
    request(ADD, {}).then(function (data) {
      state.session = data;
      render();
    }).catch(function (err) {
      state.note = err.message;
      render();
    });
  }

  // finishSignin hands the code Google gave back to the server, which
  // exchanges it and registers the account.
  function finishSignin(code) {
    code = (code || "").trim();
    if (!code) {
      state.note = "Paste the code Google showed you.";
      render();
      return;
    }
    state.note = "";
    request(ADD, { id: state.session ? state.session.id : "", code: code }).then(function (data) {
      if (data.status === "done") {
        state.session = null;
        state.note = "Signed in as " + (data.email || "the new account") + ".";
        state.usage = null;
        state.usageEmail = "";
        state.usageError = "";
        refresh();
        render();
        toast("Signed in as " + (data.email || "the new account") + ".");
      } else {
        state.note = data.error || "the sign-in failed.";
        render();
        toast(data.error || "The sign-in failed.", "error");
      }
    }).catch(function (err) {
      state.note = err.message;
      render();
      toast(err.message, "error");
    });
  }

  function refresh() {
    state.loaded = true;
    request(API, {}).then(function (data) {
      state.list = data;
      render();
    }).catch(function (err) {
      state.note = err.message;
      render();
    });
  }

  var pending = false;
  function scheduleMount() {
    if (pending) { return; }
    pending = true;
    setTimeout(function () { pending = false; mount(); }, 250);
  }

  function mount() {
    var host = ensureHost();
    renderButton(host, state.list);
    if (!state.loaded) { refresh(); }
    // The app re-renders its own chrome, so the fixup runs on every
    // change of the page: it is what makes a switch visible in the app
    // itself and not only in the widget's panel.
    //
    // The app's own quota panel is *not* touched: it is fed by the
    // RetrieveUserQuotaSummary route the clone's dispatcher answers
    // (internal/remotecontrol/dispatch_quota.go), so the app renders
    // its own rows and its own ring gauges.
    syncAccountText();
  }

  function start() {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", start);
      return;
    }
    mount();
    // The panel follows the button when the page moves, but it is only
    // moved: a re-render would drop the list's scroll position.
    window.addEventListener("resize", function () { if (state.open) { reposition(); } });
    window.addEventListener("scroll", function () { if (state.open) { reposition(); } }, true);
    if (window.MutationObserver) {
      var observer = new MutationObserver(function () { scheduleMount(); });
      observer.observe(document.documentElement, { childList: true, subtree: true });
    }
  }

  document.addEventListener("click", function () {
    if (!state.open) { return; }
    state.open = false;
    state.pendingRemove = null;
    render();
  });

  start();
})();`
