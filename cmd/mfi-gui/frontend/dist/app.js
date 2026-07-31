// Frontend for MobFI. Calls the Go bindings exposed by Wails at
// window.go.main.GUI.* (each returns a Promise). No build step: this is
// plain ES modules-free JavaScript served straight from frontend/dist.

const $ = (sel) => document.querySelector(sel);
const el = (tag, props = {}, ...children) => {
  const n = Object.assign(document.createElement(tag), props);
  for (const c of children) n.append(c);
  return n;
};

// makeResizable adds draggable column dividers to a table's headers. Header
// text is wrapped in a .th-label span so other code (e.g. sort arrows) can
// update the label without clobbering the resize handle.
// lockColumns freezes each column at its current natural width and switches
// the table to fixed layout, so subsequent drags resize predictably without
// the browser re-flowing every column.
function lockColumns(table) {
  if (table.classList.contains("cols-fixed")) return;
  table.querySelectorAll("thead th").forEach((th) => {
    th.style.width = th.offsetWidth + "px";
  });
  table.classList.add("cols-fixed");
}

// wrapScroll puts a table inside a bordered, horizontally-scrollable container
// so wide content scrolls within the table area instead of shifting the whole
// page (which would clip the section title), and every data table gets the same
// enclosing border. A wide table (including its action buttons) scrolls inside
// the box. Tables already inside a bordered scroll wrapper (.scroll -- e.g.
// Apps, Database) are left alone to avoid a double box.
function wrapScroll(table) {
  if (!table || table.closest(".scroll, .table-scroll")) return;
  const wrap = el("div", { className: "table-scroll" });
  table.parentNode.insertBefore(wrap, table);
  wrap.appendChild(table);
}

function makeResizable(table, storeKey) {
  if (!table) return;
  wrapScroll(table);
  const saveWidths = () => {
    if (!storeKey) return;
    const widths = [...table.querySelectorAll("thead th")].map((t) => t.offsetWidth);
    localStorage.setItem(storeKey, JSON.stringify(widths));
  };

  table.querySelectorAll("thead th").forEach((th) => {
    if (th.classList.contains("no-resize")) return; // icon / action columns
    if (!th.querySelector(".th-label")) {
      const label = el("span", { className: "th-label", textContent: th.textContent });
      th.textContent = "";
      th.appendChild(label);
    }
    if (th.querySelector(".col-resizer")) return;
    const res = el("span", { className: "col-resizer" });
    th.appendChild(res);
    res.addEventListener("click", (e) => e.stopPropagation());
    res.addEventListener("mousedown", (e) => {
      e.preventDefault();
      e.stopPropagation();
      lockColumns(table); // natural widths -> fixed, only on first drag
      const startX = e.pageX;
      const startW = th.offsetWidth;
      const onMove = (ev) => {
        th.style.width = Math.max(48, startW + (ev.pageX - startX)) + "px";
      };
      const onUp = () => {
        document.removeEventListener("mousemove", onMove);
        document.removeEventListener("mouseup", onUp);
        document.body.style.cursor = "";
        saveWidths();
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
      document.body.style.cursor = "col-resize";
    });
  });

  // Restore saved widths (only if the column count still matches).
  if (storeKey) {
    const saved = JSON.parse(localStorage.getItem(storeKey) || "null");
    const ths = table.querySelectorAll("thead th");
    if (Array.isArray(saved) && saved.length === ths.length) {
      ths.forEach((th, i) => {
        th.style.width = saved[i] + "px";
      });
      table.classList.add("cols-fixed");
    }
  }
}

// makeVResizer lets a divider drag the height of a target element (used for
// the splitter between the app list and the details panel). If storeKey is
// given, the height is saved to localStorage and restored on load.
function makeVResizer(divider, target, storeKey) {
  if (!divider || !target) return;
  if (storeKey) {
    const saved = parseInt(localStorage.getItem(storeKey), 10);
    if (saved > 0) {
      target.style.maxHeight = "none";
      target.style.height = saved + "px";
    }
  }
  divider.addEventListener("mousedown", (e) => {
    e.preventDefault();
    const startY = e.pageY;
    const startH = target.offsetHeight;
    target.style.maxHeight = "none"; // let the explicit height take over
    const onMove = (ev) => {
      target.style.height = Math.max(120, startH + (ev.pageY - startY)) + "px";
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      if (storeKey) localStorage.setItem(storeKey, target.offsetHeight);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
    document.body.style.cursor = "row-resize";
  });
}

function gui() {
  // window.go is injected by the Wails runtime once bindings are ready.
  if (!window.go || !window.go.main || !window.go.main.GUI) {
    throw new Error("Wails bindings not ready yet");
  }
  return window.go.main.GUI;
}

let toastTimer;
function toast(msg, ok = false) {
  const t = $("#toast");
  t.textContent = msg;
  t.style.background = ok ? "var(--ok)" : "var(--danger)";
  t.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), ok ? 1800 : 4000);
}

function fail(err) {
  toast(String(err && err.message ? err.message : err));
}

// --- tab switching ---
function showView(name) {
  document.querySelectorAll(".view").forEach((v) => v.classList.add("hidden"));
  $(`#view-${name}`).classList.remove("hidden");
  document.querySelectorAll(".tab").forEach((t) =>
    t.classList.toggle("active", t.dataset.view === name)
  );
  onViewChange(name);
}

document.querySelectorAll(".tab").forEach((t) =>
  t.addEventListener("click", () => showView(t.dataset.view))
);

// Poll for devices only while the Devices view is active.
function onViewChange(name) {
  if (name === "devices") startDevicePolling();
  else stopDevicePolling();
  if (name === "console") populateConsoleDevices();
  requestAnimationFrame(fitTableHeights);
}

// fitTableHeights sizes the visible data table so it ends at the bottom of the
// window instead of overflowing past it -- accounting for whatever sits above
// it (which varies, e.g. the Keys notes/limitations). The fixed 60vh cap can be
// too tall once that content pushes the table down.
function fitTableHeights() {
  document.querySelectorAll(".table-scroll").forEach((box) => {
    if (box.offsetParent === null) return; // in a hidden view
    const top = box.getBoundingClientRect().top;
    const avail = window.innerHeight - top - 18; // small bottom margin
    box.style.maxHeight = Math.max(180, avail) + "px";
  });
}
window.addEventListener("resize", fitTableHeights);

function clearRows(tbodySel) {
  $(tbodySel).replaceChildren();
}

function emptyRow(tbodySel, cols, text) {
  const tr = el("tr", { className: "empty" });
  tr.append(el("td", { colSpan: cols, textContent: text }));
  $(tbodySel).append(tr);
}

// --- Row actions: a primary button + a "…" overflow menu, plus right-click ---
// Each table row supplies a list of actions: [{ label, title?, onClick, primary? }].
// actionCell shows the primary action as a button and the rest behind "…";
// wireRowMenu makes a right-click anywhere on the row open the full list. This
// keeps rows compact no matter how many device-scoped features we add.
let openActionMenuEl = null;

function closeActionMenu() {
  if (!openActionMenuEl) return;
  openActionMenuEl.remove();
  openActionMenuEl = null;
  document.removeEventListener("mousedown", onActionMenuOutside, true);
  document.removeEventListener("keydown", onActionMenuKey, true);
  window.removeEventListener("blur", closeActionMenu);
}
function onActionMenuOutside(e) {
  if (openActionMenuEl && !openActionMenuEl.contains(e.target)) closeActionMenu();
}
function onActionMenuKey(e) {
  if (e.key === "Escape") closeActionMenu();
}
function openActionMenu(x, y, items) {
  closeActionMenu();
  const menu = el("div", { className: "ctx-menu" });
  for (const it of items) {
    if (!it) continue;
    const mi = el("div", { className: "ctx-item", textContent: it.label });
    if (it.title) mi.title = it.title;
    mi.addEventListener("click", (ev) => {
      ev.stopPropagation();
      closeActionMenu();
      it.onClick(ev);
    });
    menu.append(mi);
  }
  document.body.append(menu);
  const r = menu.getBoundingClientRect();
  let px = x;
  let py = y;
  if (px + r.width > window.innerWidth - 8) px = window.innerWidth - r.width - 8;
  if (py + r.height > window.innerHeight - 8) py = window.innerHeight - r.height - 8;
  menu.style.left = Math.max(8, px) + "px";
  menu.style.top = Math.max(8, py) + "px";
  openActionMenuEl = menu;
  setTimeout(() => {
    document.addEventListener("mousedown", onActionMenuOutside, true);
    document.addEventListener("keydown", onActionMenuKey, true);
    window.addEventListener("blur", closeActionMenu);
  }, 0);
}

function actionCell(items) {
  items = (items || []).filter(Boolean);
  const td = el("td", { className: "col-actions" });
  if (!items.length) return td;
  const primary = items.find((i) => i.primary) || items[0];
  const btn = el("button", { textContent: primary.label, title: primary.title || "" });
  btn.addEventListener("click", (e) => {
    e.stopPropagation();
    primary.onClick(e);
  });
  td.append(btn);
  const rest = items.filter((i) => i !== primary);
  if (rest.length) {
    const more = el("button", { className: "kebab", textContent: "…", title: "More actions" });
    more.addEventListener("click", (e) => {
      e.stopPropagation();
      const r = more.getBoundingClientRect();
      openActionMenu(r.left, r.bottom + 2, rest);
    });
    td.append(" ", more);
  }
  return td;
}

// wireRowMenu opens the full action list on a right-click anywhere in the row.
function wireRowMenu(tr, items) {
  items = (items || []).filter(Boolean);
  if (items.length) {
    tr.addEventListener("contextmenu", (e) => {
      e.preventDefault();
      openActionMenu(e.clientX, e.clientY, items);
    });
  }
  return tr;
}

// Suppress the webview's default context menu (Reload/Inspect) except where the
// native menu is useful: text fields, the terminal, and any time text is
// selected (so right-click "Copy" still works on rendered/output text). Wired
// table rows open their own action menu via wireRowMenu.
document.addEventListener("contextmenu", (e) => {
  if (e.target.closest("input, textarea, [contenteditable], .term, .xterm")) return;
  if (window.getSelection && window.getSelection().toString().trim()) return;
  e.preventDefault();
});

// loadingRow shows a spinner + message while an async table load is in flight.
// Reuses the shared .busy::before spinner.
function loadingRow(tbodySel, cols, text) {
  const label = el("span", { className: "busy", textContent: text || "Loading…" });
  const tr = el("tr", { className: "empty loading" }, el("td", { colSpan: cols }, label));
  $(tbodySel).replaceChildren(tr);
}

// --- Devices (auto-refreshing) ---
const rootCache = new Map(); // deviceID -> "rooted"/"jailbroken"/...
let devicePollTimer = null;
let lastDevicesSig = null;
let lastDevices = []; // most recent DetectDevices result, for form lookups
let pendingConsoleDeviceID = null; // device to auto-select next Console populate
let pendingConsoleConnect = false; // auto-connect after that selection

function startDevicePolling() {
  refreshDevices();
  if (!devicePollTimer) devicePollTimer = setInterval(refreshDevices, 2500);
}
function stopDevicePolling() {
  if (devicePollTimer) {
    clearInterval(devicePollTimer);
    devicePollTimer = null;
  }
}

async function refreshDevices(force) {
  let devices;
  try {
    devices = (await gui().DetectDevices()) || [];
  } catch (e) {
    if (force) fail(e); // stay silent during background polling
    return;
  }
  lastDevices = devices;
  updateExtractScope(); // a device's transport may have changed
  const sig = JSON.stringify(devices.map((d) => [d.id, d.platform, d.transport, d.state, d.name]));
  if (!force && sig === lastDevicesSig) return; // nothing changed
  lastDevicesSig = sig;
  renderDevices(devices);
}

// updateExtractScope shows the iOS AFC-scope selector only for a physical iOS
// device (the only case where container-vs-documents applies). It is hidden
// for Android and for iOS Simulators, whose extraction copies the whole data
// container regardless of scope. An unrecognised device id leaves it visible.
function updateExtractScope() {
  const label = $("#ex-scope-label");
  if (!label) return;
  const id = $("#ex-device").value.trim();
  const dev = lastDevices.find((d) => d.id === id);
  const physicalIOS = !dev || (dev.platform === "ios" && dev.transport !== "simulator");
  label.classList.toggle("hidden", !physicalIOS);
}

function applyRootCell(cell, status) {
  if (!status) {
    cell.textContent = "…";
    cell.className = "root-cell";
    return;
  }
  cell.textContent = status;
  const flagged = status === "rooted" || status === "jailbroken";
  cell.className = "root-cell " + (flagged ? "root-yes" : "root-no");
}

async function fetchDeviceRoot(d, cell) {
  try {
    const status = await gui().DeviceRoot(d.id, d.platform, d.transport);
    rootCache.set(d.id, status);
    applyRootCell(cell, status);
  } catch (e) {
    applyRootCell(cell, "unknown");
  }
}

function renderDevices(devices) {
  clearRows("#devices-table tbody");
  // No devices → show the branded hero; otherwise show the device list.
  $("#devices-hero").classList.toggle("hidden", devices.length > 0);
  $("#devices-table").classList.toggle("hidden", devices.length === 0);
  if (devices.length === 0) return;
  for (const d of devices) {
    deviceTransport.set(d.id, d.transport);
    // Actions in tab order: Apps, Extract, Keys, Console.
    const items = [
      { label: "List apps", primary: true, onClick: () => loadApps(d.id) },
      {
        label: "Use in Extract",
        onClick: () => {
          $("#ex-device").value = d.id;
          updateExtractScope();
          $("#ex-bundle").focus();
          showView("extract");
        },
      },
      { label: "Keys", onClick: () => useInKeys(d) },
      {
        label: "Console",
        onClick: () => {
          pendingConsoleDeviceID = d.id;
          pendingConsoleConnect = true; // auto-connect once selected
          showView("console");
        },
      },
    ];

    const rootCell = el("td", {});
    applyRootCell(rootCell, rootCache.get(d.id));

    const tr = wireRowMenu(el("tr", {},
      el("td", { textContent: d.id }),
      el("td", { textContent: d.name }),
      el("td", { textContent: d.platform }),
      el("td", { textContent: d.transport }),
      el("td", { className: `state-${d.state}`, textContent: d.state }),
      rootCell,
      actionCell(items)
    ), items);
    $("#devices-table tbody").append(tr);

    if (!rootCache.has(d.id)) fetchDeviceRoot(d, rootCell);
  }
  requestAnimationFrame(fitTableHeights); // the table's visibility just changed
}

// Manual Detect re-checks everything, including root/jailbreak status.
$("#btn-detect").addEventListener("click", () => {
  rootCache.clear();
  refreshDevices(true);
});

// --- Dependencies panel (external device tools) ---
$("#btn-deps").addEventListener("click", () => {
  const panel = $("#deps-panel");
  if (panel.classList.contains("hidden")) loadDeps();
  else panel.classList.add("hidden");
});

async function loadDeps() {
  const panel = $("#deps-panel");
  panel.classList.remove("hidden");
  panel.textContent = "Checking…";
  try {
    renderDeps((await gui().Doctor()) || []);
  } catch (e) {
    panel.classList.add("hidden");
    fail(e);
  }
}

function renderDeps(tools) {
  const panel = $("#deps-panel");
  const missing = tools.filter((t) => !t.found && !t.optional).map((t) => t.name);

  const summary = el("span", {
    className: "mime",
    textContent: missing.length ? `${missing.length} core tool(s) missing` : "all core tools present",
  });
  const closeBtn = el("button", { className: "details-close", textContent: "✕", title: "Close" });
  closeBtn.addEventListener("click", () => panel.classList.add("hidden"));
  const head = el("div", { className: "details-head" },
    el("h3", { textContent: "Dependencies" }), summary, closeBtn);

  const tbody = el("tbody");
  for (const t of tools) {
    const status = t.found ? "ok" : t.optional ? "optional" : "missing";
    const badge = el("span", { className: "dep-badge dep-" + status, textContent: status });

    let loc;
    if (t.found) {
      loc = el("span", { className: "dep-path", textContent: t.path });
    } else {
      const hint = t.hint || "(see README)";
      const copy = el("button", { className: "mini", textContent: "Copy", title: "Copy install command" });
      copy.addEventListener("click", () =>
        gui().Copy(hint).then(() => toast("copied", true)).catch(fail));
      loc = el("span", {}, el("code", { textContent: hint }), " ", copy);
    }

    tbody.append(el("tr", {},
      el("td", {}, badge),
      el("td", { textContent: t.name }),
      el("td", { textContent: t.purpose }),
      el("td", {}, loc)
    ));
  }
  const table = el("table", { className: "grid deps-table" },
    el("thead", {}, el("tr", {},
      el("th", { textContent: "" }),
      el("th", { textContent: "Tool" }),
      el("th", { textContent: "Purpose" }),
      el("th", { textContent: "Location / install" })
    )),
    tbody
  );
  panel.replaceChildren(head, table);
}

// Connect to an Android device over adb TCP (host:port).
async function connectTCP() {
  const addr = $("#tcp-addr").value.trim();
  if (!addr) return;
  try {
    const msg = await gui().ConnectTCP(addr);
    toast(msg || "connected to " + addr, true);
    refreshDevices(true);
  } catch (e) {
    fail(e);
  }
}
$("#btn-connect").addEventListener("click", connectTCP);
$("#tcp-addr").addEventListener("keydown", (e) => {
  if (e.key === "Enter") connectTCP();
});

// Pair with an Android 11+ device for wireless debugging (host:port + code).
async function pairTCP() {
  const addr = $("#pair-addr").value.trim();
  const code = $("#pair-code").value.trim();
  if (!addr || !code) {
    toast("enter the pairing host:port and code");
    return;
  }
  try {
    const msg = await gui().PairTCP(addr, code);
    toast(msg || "paired", true);
    $("#pair-code").value = "";
  } catch (e) {
    fail(e);
  }
}
$("#btn-pair").addEventListener("click", pairTCP);
$("#pair-code").addEventListener("keydown", (e) => {
  if (e.key === "Enter") pairTCP();
});

// --- Apps ---
let currentAppsDevice = null;
let currentApps = [];
let selectedBundle = null; // bundle id whose details panel is open

// Column index -> app field (null = not sortable: icon and action columns).
const APP_COLUMNS = [null, "bundle_id", "name", "version", "data_path", "install_path", null];
let appSortField = null;
let appSortDir = 1;

// A generated monogram avatar shown until the real icon loads.
function hashHue(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}
function avatar(a) {
  const label = (a.name || a.bundle_id || "?").trim();
  const letter = ((label.match(/[A-Za-z0-9]/) || ["?"])[0]).toUpperCase();
  const span = el("span", { className: "avatar", textContent: letter });
  span.style.background = `hsl(${hashHue(a.bundle_id || label)} 52% 42%)`;
  return span;
}

// A readable placeholder name from the bundle id until the real label loads.
function humanize(bundleID) {
  const seg = (bundleID || "").split(".").pop() || bundleID || "";
  return seg.split(/[_-]/).filter(Boolean).map((w) => w[0].toUpperCase() + w.slice(1)).join(" ");
}

// --- lazy real icon + label (resolved from the APK via aapt) ---
const appMetaCache = new Map(); // bundle_id -> { name, icon }
let metaInFlight = 0;
const metaQueue = [];
function pumpMeta() {
  while (metaInFlight < 2 && metaQueue.length) {
    const job = metaQueue.shift();
    metaInFlight++;
    job().finally(() => {
      metaInFlight--;
      pumpMeta();
    });
  }
}
const appMetaObserver = new IntersectionObserver(
  (entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      const row = e.target;
      appMetaObserver.unobserve(row);
      if (row._app) {
        metaQueue.push(() => fetchAppMeta(row, row._app));
        pumpMeta();
      }
    }
  },
  { root: document.querySelector("#view-apps .scroll"), rootMargin: "150px" }
);

async function fetchAppMeta(row, a) {
  if (appMetaCache.has(a.bundle_id)) {
    applyAppMeta(row, appMetaCache.get(a.bundle_id));
    return;
  }
  try {
    const meta = (await gui().AppMeta(currentAppsDevice, a.bundle_id, a.install_path)) || {};
    appMetaCache.set(a.bundle_id, meta);
    applyAppMeta(row, meta);
  } catch (e) {
    /* leave the placeholder in place */
  }
}

function applyAppMeta(row, meta) {
  if (!meta) return;
  if (meta.name) {
    const nameEl = row.querySelector(".app-name");
    if (nameEl) nameEl.textContent = meta.name;
    if (row._app) row._app.name = meta.name; // keep data in sync so sorting matches the display
  }
  if (meta.version) {
    const verEl = row.querySelector(".app-version");
    if (verEl) verEl.textContent = meta.version;
    if (row._app) row._app.version = meta.version;
  }
  if (meta.icon) {
    const iconEl = row.querySelector(".app-icon");
    if (iconEl) iconEl.replaceChildren(el("img", { className: "app-img", src: meta.icon, alt: "" }));
  }
}

async function loadApps(deviceID) {
  currentAppsDevice = deviceID;
  clearDetails(); // a fresh listing (incl. system-apps toggle) resets the panel
  showView("apps");
  $("#apps-device").textContent = deviceID;
  loadingRow("#apps-table tbody", 7, "Loading apps…");
  $("#apps-count").textContent = "loading…";
  try {
    currentApps = (await gui().ListApps(deviceID, $("#apps-system").checked)) || [];
  } catch (e) {
    currentApps = [];
    fail(e);
  }
  renderApps();
}

// appSortValue returns the value to sort an app by for column f, mirroring
// what the row actually displays. Name and Version are resolved lazily from
// the APK (Android's pm list gives neither), so prefer the resolved metadata
// (cached) and fall back to the same humanized name the row shows; otherwise
// the sort would compare empty strings and appear to do nothing.
function appSortValue(a, f) {
  const meta = appMetaCache.get(a.bundle_id);
  if (f === "name") return (meta && meta.name) || a.name || humanize(a.bundle_id);
  if (f === "version") return (meta && meta.version) || a.version || "";
  return a[f] ?? "";
}

function sortApps(rows) {
  if (!appSortField) return rows;
  const f = appSortField;
  return [...rows].sort((a, b) => {
    const av = appSortValue(a, f).toString();
    const bv = appSortValue(b, f).toString();
    const an = parseFloat(av);
    const bn = parseFloat(bv);
    const numeric =
      !isNaN(an) && !isNaN(bn) && String(an) === av.trim() && String(bn) === bv.trim();
    const cmp = numeric ? an - bn : av.toLowerCase().localeCompare(bv.toLowerCase());
    return cmp * appSortDir;
  });
}

function updateSortIndicators() {
  document.querySelectorAll("#apps-table thead th.sortable").forEach((th) => {
    const arrow = th.dataset.field === appSortField ? (appSortDir === 1 ? " ▲" : " ▼") : "";
    const labelEl = th.querySelector(".th-label");
    if (labelEl) labelEl.textContent = th.dataset.label + arrow;
    else th.textContent = th.dataset.label + arrow;
  });
}

function renderApps() {
  const q = ($("#apps-search").value || "").trim().toLowerCase();
  let rows = currentApps.filter((a) =>
    !q ||
    (a.bundle_id && a.bundle_id.toLowerCase().includes(q)) ||
    (a.name && a.name.toLowerCase().includes(q))
  );
  rows = sortApps(rows);

  const total = currentApps.length;
  $("#apps-count").textContent = !total
    ? ""
    : rows.length === total
      ? `${total} app(s)`
      : `${rows.length} of ${total} app(s) shown`;
  updateSortIndicators();

  clearRows("#apps-table tbody");
  if (rows.length === 0) {
    emptyRow("#apps-table tbody", 7, currentApps.length ? "no matches" : "no apps found");
    return;
  }
  for (const a of rows) {
    const items = [
      {
        label: "Use in Extract",
        primary: true,
        onClick: () => {
          $("#ex-device").value = currentAppsDevice;
          $("#ex-bundle").value = a.bundle_id;
          updateExtractScope();
          showView("extract");
        },
      },
      {
        label: "Copy bundle id",
        onClick: async () => {
          try {
            await gui().Copy(a.bundle_id);
            toast("copied " + a.bundle_id, true);
          } catch (e) {
            fail(e);
          }
        },
      },
    ];

    const row = wireRowMenu(el("tr", {},
      el("td", { className: "col-icon app-icon" }, avatar(a)),
      el("td", { textContent: a.bundle_id }),
      el("td", { className: "app-name", textContent: a.name || humanize(a.bundle_id) }),
      el("td", { className: "app-version", textContent: a.version || "" }),
      el("td", { textContent: a.data_path }),
      el("td", { textContent: a.install_path }),
      actionCell(items)
    ), items);
    row._app = a;
    row.addEventListener("click", (e) => {
      if (e.target.closest("button")) return; // let action buttons do their thing
      selectApp(row, a);
    });
    $("#apps-table tbody").append(row);

    // Lazily resolve the real icon + name from the APK.
    if (appMetaCache.has(a.bundle_id)) applyAppMeta(row, appMetaCache.get(a.bundle_id));
    else appMetaObserver.observe(row);

    if (a.bundle_id === selectedBundle) row.classList.add("selected");
  }

  // If the selected app was filtered out, close its details panel.
  if (selectedBundle && !rows.some((a) => a.bundle_id === selectedBundle)) {
    clearDetails();
  }
}

function setupAppsSorting() {
  document.querySelectorAll("#apps-table thead th").forEach((th, i) => {
    const field = APP_COLUMNS[i];
    if (!field) return;
    th.classList.add("sortable");
    th.dataset.field = field;
    const labelEl = th.querySelector(".th-label");
    th.dataset.label = labelEl ? labelEl.textContent : th.textContent;
    th.addEventListener("click", () => {
      if (appSortField === field) appSortDir = -appSortDir;
      else {
        appSortField = field;
        appSortDir = 1;
      }
      renderApps();
    });
  });
}

// clearDetails hides the details panel and drops the row highlight.
function clearDetails() {
  selectedBundle = null;
  document.querySelectorAll("#apps-table tbody tr.selected").forEach((r) => r.classList.remove("selected"));
  const panel = $("#app-details");
  panel.classList.add("hidden");
  panel.replaceChildren();
}

async function selectApp(row, a) {
  selectedBundle = a.bundle_id;
  document.querySelectorAll("#apps-table tbody tr.selected").forEach((r) => r.classList.remove("selected"));
  row.classList.add("selected");
  const panel = $("#app-details");
  panel.classList.remove("hidden");
  panel.textContent = "Loading details…";
  try {
    const d = await gui().AppDetails(currentAppsDevice, a.bundle_id, a.install_path, a.platform);
    if (selectedBundle === a.bundle_id) renderDetails(a, d); // ignore if selection changed
  } catch (e) {
    panel.textContent = "";
    fail(e);
  }
}

function renderDetails(a, d) {
  const panel = $("#app-details");
  const name = (appMetaCache.get(a.bundle_id) || {}).name || a.name || humanize(a.bundle_id);

  const dl = el("dl", { className: "kv" });
  dl.append(el("dt", { textContent: "Bundle id" }), el("dd", { textContent: a.bundle_id }));
  for (const f of d.fields || []) {
    if (!f.value) continue;
    dl.append(el("dt", { textContent: f.label }), el("dd", { textContent: f.value }));
  }

  const closeBtn = el("button", { className: "details-close", textContent: "✕", title: "Close" });
  closeBtn.addEventListener("click", clearDetails);

  panel.replaceChildren(
    el("div", { className: "details-head" },
      (appMetaCache.get(a.bundle_id) || {}).icon
        ? el("img", { className: "app-img", src: appMetaCache.get(a.bundle_id).icon, alt: "" })
        : avatar(a),
      el("h3", { textContent: name }),
      closeBtn
    ),
    dl
  );

  if (d.permissions && d.permissions.length) {
    panel.append(el("h4", { textContent: `Permissions (${d.permissions.length})` }));
    const ul = el("ul", { className: "perms" });
    for (const p of d.permissions) ul.append(el("li", { textContent: p }));
    panel.append(ul);
  }
}

// Filter as you type; re-list when the system-apps toggle changes.
makeResizable($("#apps-table"), "mobfi.cols.apps"); // must precede sorting (creates .th-label)
setupAppsSorting();
$("#apps-search").addEventListener("input", renderApps);
$("#apps-system").addEventListener("change", () => {
  if (currentAppsDevice) loadApps(currentAppsDevice);
});

// --- Extract ---
$("#ex-device").addEventListener("input", updateExtractScope);
updateExtractScope(); // set initial visibility

$("#btn-extract").addEventListener("click", async () => {
  const out = $("#extract-out");
  const btn = $("#btn-extract");
  const cancelBtn = $("#btn-extract-cancel");
  const device = $("#ex-device").value.trim();
  const bundle = $("#ex-bundle").value.trim();
  const dest = $("#ex-dest").value.trim();
  if (!device || !bundle || !dest) {
    fail("extract requires device, app and destination");
    return;
  }

  // Warn if the destination already contains files.
  let destWasEmpty = true;
  try {
    const st = await gui().DirStatus(dest);
    destWasEmpty = !st.exists || st.empty;
    if (st.exists && !st.empty) {
      const ok = await gui().Confirm(
        "Destination not empty",
        `${dest} already contains files. Extraction may overwrite or mix with them. Continue?`
      );
      if (!ok) return;
    }
  } catch (e) {
    /* if the pre-check fails, proceed anyway */
  }

  cancelling.extract = false;
  btn.disabled = true;
  cancelBtn.disabled = false; // re-enable (a prior cancel click disabled it)
  cancelBtn.classList.remove("hidden");
  out.textContent = "Extracting… (starting)";
  const off = window.runtime.EventsOn("extract:progress", (p) => {
    // Once Cancel is clicked, stop overwriting the "Cancelling…" message with
    // late progress events so the click has immediate, visible effect.
    if (cancelling.extract) return;
    // Show the file/byte count only once it is meaningful. iOS backup has a
    // long phase (the full-device backup) before any files are reconstructed,
    // where p.files/p.bytes are 0 and p.path carries the overall status.
    if (p.files > 0 || p.bytes > 0) {
      out.textContent = `Extracting… ${p.files} file(s), ${p.bytes.toLocaleString()} byte(s)\n${p.path}`;
    } else {
      out.textContent = `Extracting…\n${p.path}`;
    }
  });

  try {
    const res = await gui().ExtractApp(device, bundle, dest, $("#ex-scope").value);
    let text = `Extracted ${res.file_count} file(s), ${res.byte_count.toLocaleString()} byte(s)\nto ${res.root}\n`;
    if (res.skipped && res.skipped.length) {
      text += `\nSkipped ${res.skipped.length} path(s):\n`;
      for (const s of res.skipped) text += `  ${s.path}: ${s.reason}\n`;
    }
    out.textContent = text;
  } catch (e) {
    if (cancelling.extract || isCancelError(e)) {
      out.textContent = "Extraction cancelled.";
      if (destWasEmpty) {
        try {
          const clean = await gui().Confirm(
            "Clean up?",
            `Delete the files transferred so far under ${dest}?`
          );
          if (clean) {
            await gui().RemoveDir(dest);
            out.textContent = "Extraction cancelled; partial files removed.";
            toast("cleaned up", true);
          }
        } catch (err) {
          fail(err);
        }
      } else {
        out.textContent = "Extraction cancelled. (Destination pre-existed; partial files left in place.)";
      }
    } else {
      out.textContent = "";
      fail(e);
    }
  } finally {
    off();
    btn.disabled = false;
    cancelBtn.classList.add("hidden");
  }
});

// --- Scan ---
let currentFindings = null;
let scanSortField = null;
let scanSortDir = 1;
const SCAN_COLUMNS = ["rule_id", "path", "line", "match", null, null]; // ...Status, actions (not sortable)

// Cancel wiring for long operations. Clicking Cancel sets a flag (so the
// resulting rejection is treated as a cancel, not an error) and asks the
// backend to cancel the op's context.
const cancelling = { scan: false, diff: false, extract: false };
function bindCancel(btnId, op, statusId) {
  const b = document.getElementById(btnId);
  if (!b) return;
  b.addEventListener("click", () => {
    cancelling[op] = true;
    b.disabled = true; // prevent double-clicks; re-enabled when the op restarts
    if (statusId) {
      const s = document.getElementById(statusId);
      if (s) s.textContent = "Cancelling… (stopping and cleaning up)";
    }
    gui().CancelOp(op);
  });
}
bindCancel("btn-scan-cancel", "scan");
bindCancel("btn-diff-cancel", "diff");
bindCancel("btn-extract-cancel", "extract", "extract-out");
function isCancelError(e) {
  return String((e && e.message) || e).toLowerCase().includes("cancel");
}

// Export a report (HTML/JSON/text) at the selected scope — this tab's results
// or a combined scan+diff report. The backend builds it from its cached
// results and prompts for a destination.
function wireExport(btnId, scopeSelId, fmtSelId, rawId) {
  const btn = document.getElementById(btnId);
  if (!btn) return;
  btn.addEventListener("click", async () => {
    const scope = document.getElementById(scopeSelId).value;
    const fmt = document.getElementById(fmtSelId).value;
    const raw = !!(rawId && document.getElementById(rawId) && document.getElementById(rawId).checked);
    if (raw) {
      // Native dialog binding -- WKWebView does not implement window.confirm().
      let ok = false;
      try {
        ok = await gui().Confirm("Unredacted export", "Export UNREDACTED secrets?\n\nThe report will contain raw secret values in plain text. Only do this for authorized local analysis, and do not share the file.");
      } catch (e) { ok = false; }
      if (!ok) return;
    }
    btn.disabled = true;
    try {
      const path = await gui().ExportReport(scope, fmt, raw);
      if (path) toast("exported to " + shortPath(path), true);
    } catch (e) {
      fail(e); // e.g. "run a scan first"
    } finally {
      btn.disabled = false;
    }
  });
}
wireExport("btn-scan-export", "scan-export-scope", "scan-export-fmt", "scan-export-raw");
wireExport("btn-diff-export", "diff-export-scope", "diff-export-fmt", "diff-export-raw");

function shortPath(p, n = 64) {
  return p && p.length > n ? "…" + p.slice(-(n - 1)) : p || "";
}

$("#btn-scan").addEventListener("click", async () => {
  const btn = $("#btn-scan");
  const cancelBtn = $("#btn-scan-cancel");
  const status = $("#scan-status");
  cancelling.scan = false;
  btn.disabled = true;
  cancelBtn.classList.remove("hidden");
  status.classList.add("busy");
  status.textContent = "Scanning…";
  // Drop the previous scan's results so a new scan doesn't show stale findings.
  currentFindings = [];
  clearRows("#scan-table tbody");
  const off = window.runtime.EventsOn("scan:progress", (p) => {
    status.textContent = `Scanning… ${p.files.toLocaleString()} file(s) — ${shortPath(p.path)}`;
  });
  try {
    const known = $("#sc-known").value.trim();
    if (known) await gui().AddKnownSecrets(known);
    currentFindings = await gui().ScanSecrets($("#sc-root").value.trim());
    renderScan();
    status.classList.remove("busy");
    status.textContent = `${(currentFindings || []).length} finding(s)`;
    // Opt-in live verification: sends each matched secret to its service, so
    // confirm first.
    if ($("#sc-verify").checked && (currentFindings || []).length) {
      let ok = false;
      try {
        ok = await gui().Confirm("Live-verify secrets?", "This calls each service's API to check whether a found key is still active, which sends the matched secret to that service. Only do this for authorized testing.");
      } catch (e) { ok = false; }
      if (ok) {
        status.classList.add("busy");
        status.textContent = "Verifying findings against their services…";
        try {
          currentFindings = await gui().VerifyFindings();
          renderScan();
          const active = (currentFindings || []).filter((f) => f.verified === "active").length;
          status.textContent = `${currentFindings.length} finding(s) — ${active} active`;
        } catch (e) {
          if (!(cancelling.scan || isCancelError(e))) fail(e);
        } finally {
          status.classList.remove("busy");
        }
      }
    }
  } catch (e) {
    status.classList.remove("busy");
    if (cancelling.scan || isCancelError(e)) status.textContent = "cancelled";
    else { status.textContent = ""; fail(e); }
  } finally {
    off();
    btn.disabled = false;
    cancelBtn.classList.add("hidden");
  }
});

function scanRow(f) {
  const matchCell = el("td", { className: "revealable", textContent: f.match, title: "click to reveal" });
  let revealed = false;
  matchCell.addEventListener("click", () => {
    revealed = !revealed;
    matchCell.textContent = revealed ? f.secret || f.match : f.match;
  });
  const statusCell = el("td", {});
  if (f.verified && f.verified !== "unsupported") {
    statusCell.append(el("span", { className: "v-" + f.verified, textContent: f.verified }));
  }
  const items = [
    { label: "Render", primary: true, title: "Open this file in Render with the secret highlighted", onClick: () => sendToRender(f.path, f.secret) },
    { label: "Decode", title: "Send this value to the Decode tab (Base64 / hex / URL)", onClick: () => sendToDecode(f.secret || f.match) },
    {
      label: "Copy",
      onClick: async () => {
        try {
          await gui().Copy(f.secret || f.match);
          toast("copied secret", true);
        } catch (e) {
          fail(e);
        }
      },
    },
  ];
  return wireRowMenu(el("tr", {},
    el("td", { textContent: f.rule_id }),
    el("td", { textContent: f.path }),
    el("td", { textContent: f.line }),
    matchCell,
    statusCell,
    actionCell(items)
  ), items);
}

function renderScan() {
  clearRows("#scan-table tbody");
  if (!currentFindings) return;
  if (currentFindings.length === 0) {
    emptyRow("#scan-table tbody", 6, "no secrets found");
    return;
  }
  updateScanSortIndicators();
  for (const f of sortRows(currentFindings, scanSortField, scanSortDir)) {
    $("#scan-table tbody").append(scanRow(f));
  }
}

function updateScanSortIndicators() {
  document.querySelectorAll("#scan-table thead th.sortable").forEach((th) => {
    const arrow = th.dataset.field === scanSortField ? (scanSortDir === 1 ? " ▲" : " ▼") : "";
    const labelEl = th.querySelector(".th-label");
    if (labelEl) labelEl.textContent = th.dataset.label + arrow;
    else th.textContent = th.dataset.label + arrow;
  });
}

function setupScanSorting() {
  const saved = JSON.parse(localStorage.getItem("mobfi.scan.sort") || "null");
  if (saved && saved.field) {
    scanSortField = saved.field;
    scanSortDir = saved.dir;
  }
  document.querySelectorAll("#scan-table thead th").forEach((th, i) => {
    const field = SCAN_COLUMNS[i];
    if (!field) return;
    th.classList.add("sortable");
    th.dataset.field = field;
    const labelEl = th.querySelector(".th-label");
    th.dataset.label = labelEl ? labelEl.textContent : th.textContent;
    th.addEventListener("click", () => {
      if (scanSortField === field) scanSortDir = -scanSortDir;
      else {
        scanSortField = field;
        scanSortDir = 1;
      }
      localStorage.setItem("mobfi.scan.sort", JSON.stringify({ field: scanSortField, dir: scanSortDir }));
      renderScan();
    });
  });
  updateScanSortIndicators();
}

// --- Diff ---
function joinPath(root, rel) {
  if (!root) return rel;
  return root.endsWith("/") ? root + rel : root + "/" + rel;
}

// Send a file to the Render tab (single-file mode).
function sendToRender(path, highlight) {
  $("#render-tree").classList.add("hidden");
  $("#render-hsplit").classList.add("hidden");
  showView("render");
  renderInPane(path, highlight);
}

// highlightSecretInPane wraps every occurrence of `secret` in the rendered file
// in a <mark> and scrolls the first into view. It walks text nodes (safe: uses
// textContent, no HTML injection), so it works for both plain-text and
// syntax-highlighted (Chroma) output as long as the secret sits in one token.
function highlightSecretInPane(secret) {
  if (!secret) return;
  const root = document.querySelector("#render-pane .code-view, #render-pane .render-text");
  if (!root) return;
  const nodes = [];
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  while (walker.nextNode()) nodes.push(walker.currentNode);
  let first = null;
  for (const node of nodes) {
    const text = node.nodeValue;
    if (text.indexOf(secret) < 0) continue;
    const frag = document.createDocumentFragment();
    let pos = 0;
    for (let idx = text.indexOf(secret); idx >= 0; idx = text.indexOf(secret, pos)) {
      if (idx > pos) frag.append(document.createTextNode(text.slice(pos, idx)));
      const mark = el("mark", { className: "secret-hit", textContent: secret });
      if (!first) first = mark;
      frag.append(mark);
      pos = idx + secret.length;
    }
    if (pos < text.length) frag.append(document.createTextNode(text.slice(pos)));
    node.parentNode.replaceChild(frag, node);
  }
  if (first) first.scrollIntoView({ block: "center" });
}

// --- Find in rendered file --------------------------------------------------
// In-pane search for the Render tab: highlights every (case-insensitive) match
// of the query, reports "current / total", and steps through matches. It walks
// text nodes, so a match must fall within one node -- always true for the plain
// text and hex panes, and within a single token for syntax-highlighted code.
let findMatches = [];
let findIndex = -1;
let findQuery = "";
let findDebounce = null;

function findRoot() {
  return document.querySelector("#render-pane .code-view, #render-pane .render-text");
}

// clearFind removes our <mark>s and merges the text back so the next search
// sees whole strings. It leaves any secret-hit marks (from the Scan tab) alone.
function clearFind() {
  const root = findRoot();
  if (root) {
    root.querySelectorAll("mark.find-hit").forEach((m) => m.replaceWith(document.createTextNode(m.textContent)));
    root.normalize();
  }
  findMatches = [];
  findIndex = -1;
}

function runFind(query) {
  clearFind();
  findQuery = query;
  const root = findRoot();
  if (root && query) {
    const q = query.toLowerCase();
    const nodes = [];
    const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
    while (walker.nextNode()) nodes.push(walker.currentNode);
    for (const node of nodes) {
      const text = node.nodeValue;
      const lower = text.toLowerCase();
      if (lower.indexOf(q) < 0) continue;
      const frag = document.createDocumentFragment();
      let pos = 0;
      for (let idx = lower.indexOf(q); idx >= 0; idx = lower.indexOf(q, pos)) {
        if (idx > pos) frag.append(document.createTextNode(text.slice(pos, idx)));
        const mark = el("mark", { className: "find-hit", textContent: text.slice(idx, idx + q.length) });
        findMatches.push(mark);
        frag.append(mark);
        pos = idx + q.length;
      }
      if (pos < text.length) frag.append(document.createTextNode(text.slice(pos)));
      node.parentNode.replaceChild(frag, node);
    }
    findIndex = findMatches.length ? 0 : -1;
    setActiveFind(true);
  }
  updateFindCount();
}

function setActiveFind(scroll) {
  findMatches.forEach((m, i) => m.classList.toggle("find-current", i === findIndex));
  const cur = findMatches[findIndex];
  if (cur && scroll) cur.scrollIntoView({ block: "center", inline: "nearest" });
}

function stepFind(delta) {
  if (!findMatches.length) return;
  findIndex = (findIndex + delta + findMatches.length) % findMatches.length;
  setActiveFind(true);
  updateFindCount();
}

function updateFindCount() {
  const c = $("#rn-find-count");
  if (!c) return;
  if (findMatches.length) c.textContent = `${findIndex + 1} / ${findMatches.length}`;
  else c.textContent = $("#rn-find-input").value ? "No matches" : "";
  const none = findMatches.length === 0;
  $("#rn-find-prev").disabled = none;
  $("#rn-find-next").disabled = none;
}

// syncFindBar shows the find controls only when the current render is text-like
// (text/hex/code) and re-applies the active query after a re-render (e.g. the
// hex or prettify toggle rebuilds the pane and drops the marks).
function syncFindBar() {
  const bar = $("#rn-find");
  if (!bar) return;
  const searchable = !!findRoot();
  bar.classList.toggle("hidden", !searchable);
  if (searchable) runFind($("#rn-find-input").value);
  else { findMatches = []; findIndex = -1; findQuery = ""; updateFindCount(); }
}

let currentDiff = null;
let diffSortField = null;
let diffSortDir = 1;
const DIFF_COLUMNS = ["kind", "path", "detail", null]; // th index -> field

// Generic table sort (numeric when both values are numbers, else localeCompare).
function sortRows(rows, field, dir) {
  if (!field) return rows;
  return [...rows].sort((a, b) => {
    const av = (a[field] ?? "").toString();
    const bv = (b[field] ?? "").toString();
    const an = parseFloat(av);
    const bn = parseFloat(bv);
    const numeric = !isNaN(an) && !isNaN(bn) && String(an) === av.trim() && String(bn) === bv.trim();
    const cmp = numeric ? an - bn : av.toLowerCase().localeCompare(bv.toLowerCase());
    return cmp * dir;
  });
}

$("#btn-diff").addEventListener("click", async () => {
  const btn = $("#btn-diff");
  const cancelBtn = $("#btn-diff-cancel");
  const status = $("#diff-status");
  cancelling.diff = false;
  btn.disabled = true;
  cancelBtn.classList.remove("hidden");
  status.classList.add("busy");
  status.textContent = "Diffing…";
  const off = window.runtime.EventsOn("diff:progress", (p) => {
    status.textContent = `Comparing… ${p.files.toLocaleString()} file(s) — ${shortPath(p.path)}`;
  });
  try {
    currentDiff = await gui().Diff($("#df-a").value.trim(), $("#df-b").value.trim());
    renderDiff();
    status.classList.remove("busy");
    status.textContent = `${(currentDiff.changes || []).length} change(s)`;
  } catch (e) {
    status.classList.remove("busy");
    if (cancelling.diff || isCancelError(e)) status.textContent = "cancelled";
    else { status.textContent = ""; fail(e); }
  } finally {
    off();
    btn.disabled = false;
    cancelBtn.classList.add("hidden");
  }
});

function renderDiff() {
  clearRows("#diff-table tbody");
  if (!currentDiff) return;
  const changes = currentDiff.changes || [];
  if (changes.length === 0) {
    emptyRow("#diff-table tbody", 4, "no differences");
    return;
  }
  updateDiffSortIndicators();
  for (const c of sortRows(changes, diffSortField, diffSortDir)) {
    const aPath = joinPath(currentDiff.root_a, c.path);
    const bPath = joinPath(currentDiff.root_b, c.path);
    const target = c.kind === "removed" ? aPath : bPath; // the surviving side

    const items = [
      { label: "Render", primary: true, title: "Open in the Render tab", onClick: () => sendToRender(target) },
    ];
    if (c.kind === "modified") {
      items.push({ label: "Compare", onClick: () => openFileDiff(aPath, bPath, c.path) });
    }

    $("#diff-table tbody").append(wireRowMenu(el("tr", {},
      el("td", {}, el("span", { className: `pill ${c.kind}`, textContent: c.kind })),
      el("td", { textContent: c.path }),
      el("td", { textContent: c.detail || "" }),
      actionCell(items)
    ), items));
  }
}

function updateDiffSortIndicators() {
  document.querySelectorAll("#diff-table thead th.sortable").forEach((th) => {
    const arrow = th.dataset.field === diffSortField ? (diffSortDir === 1 ? " ▲" : " ▼") : "";
    const labelEl = th.querySelector(".th-label");
    if (labelEl) labelEl.textContent = th.dataset.label + arrow;
    else th.textContent = th.dataset.label + arrow;
  });
}

function setupDiffSorting() {
  const saved = JSON.parse(localStorage.getItem("mobfi.diff.sort") || "null");
  if (saved && saved.field) {
    diffSortField = saved.field;
    diffSortDir = saved.dir;
  }
  document.querySelectorAll("#diff-table thead th").forEach((th, i) => {
    const field = DIFF_COLUMNS[i];
    if (!field) return;
    th.classList.add("sortable");
    th.dataset.field = field;
    const labelEl = th.querySelector(".th-label");
    th.dataset.label = labelEl ? labelEl.textContent : th.textContent;
    th.addEventListener("click", () => {
      if (diffSortField === field) diffSortDir = -diffSortDir;
      else {
        diffSortField = field;
        diffSortDir = 1;
      }
      localStorage.setItem("mobfi.diff.sort", JSON.stringify({ field: diffSortField, dir: diffSortDir }));
      renderDiff();
    });
  });
  updateDiffSortIndicators();
}

// --- side-by-side file diff overlay ---
let fileDiffHunks = [];
let fileDiffHunkIdx = -1;

function fdRow(r) {
  return el("div", { className: "fd-row type-" + r.type },
    el("div", { className: "fd-ln", textContent: r.left_num || "" }),
    el("div", { className: "fd-left", textContent: r.left }),
    el("div", { className: "fd-ln", textContent: r.right_num || "" }),
    el("div", { className: "fd-right", textContent: r.right })
  );
}

async function openFileDiff(aPath, bPath, title) {
  const scroll = $("#filediff-scroll");
  $("#filediff-title").textContent = title;
  $("#filediff-count").textContent = "";
  scroll.replaceChildren(el("div", { className: "render-empty", textContent: "Diffing…" }));
  $("#filediff").classList.remove("hidden");
  fileDiffHunks = [];
  fileDiffHunkIdx = -1;
  try {
    const res = await gui().FileDiff(aPath, bPath);
    scroll.replaceChildren();
    if (res.binary) {
      scroll.append(el("div", { className: "render-empty", textContent: "Binary file — cannot show a line diff." }));
      return;
    }
    if (res.too_large) {
      scroll.append(el("div", { className: "render-empty", textContent: "File too large to diff inline." }));
      return;
    }
    let inHunk = false;
    for (const r of res.rows || []) {
      const row = fdRow(r);
      if (r.type !== "same") {
        if (!inHunk) fileDiffHunks.push(row);
        inHunk = true;
      } else {
        inHunk = false;
      }
      scroll.append(row);
    }
    $("#filediff-count").textContent = `${fileDiffHunks.length} difference${fileDiffHunks.length === 1 ? "" : "s"}`;
    if (fileDiffHunks.length) gotoHunk(0);
  } catch (e) {
    scroll.replaceChildren();
    fail(e);
  }
}

function gotoHunk(i) {
  if (!fileDiffHunks.length) return;
  fileDiffHunkIdx = (i + fileDiffHunks.length) % fileDiffHunks.length;
  fileDiffHunks.forEach((h) => h.classList.remove("hunk-current"));
  const h = fileDiffHunks[fileDiffHunkIdx];
  h.classList.add("hunk-current");
  h.scrollIntoView({ block: "center" });
}

$("#filediff-next").addEventListener("click", () => gotoHunk(fileDiffHunkIdx + 1));
$("#filediff-prev").addEventListener("click", () => gotoHunk(fileDiffHunkIdx - 1));
$("#filediff-close").addEventListener("click", () => $("#filediff").classList.add("hidden"));
document.addEventListener("keydown", (e) => {
  if ($("#filediff").classList.contains("hidden")) return;
  if (e.key === "Escape") $("#filediff").classList.add("hidden");
  else if (e.key === "ArrowDown" || e.key === "n") { e.preventDefault(); gotoHunk(fileDiffHunkIdx + 1); }
  else if (e.key === "ArrowUp" || e.key === "p") { e.preventDefault(); gotoHunk(fileDiffHunkIdx - 1); }
});

// --- Database ---
async function loadTables() {
  try {
    const tables = await gui().DBTables($("#db-file").value.trim());
    const box = $("#db-tables");
    box.replaceChildren();
    if (!tables || tables.length === 0) {
      box.append(el("span", { className: "mime", textContent: "no tables" }));
      return;
    }
    for (const t of tables) {
      const chip = el("span", { className: "chip", textContent: t });
      chip.addEventListener("click", () => {
        $("#db-table").value = t;
        readTable();
      });
      box.append(chip);
    }
  } catch (e) {
    fail(e);
  }
}

async function readTable() {
  try {
    const limit = parseInt($("#db-limit").value, 10) || 100;
    const t = await gui().DBRead($("#db-file").value.trim(), $("#db-table").value.trim(), limit);
    const thead = $("#db-table-out thead");
    const tbody = $("#db-table-out tbody");
    thead.replaceChildren();
    tbody.replaceChildren();
    thead.append(el("tr", {}, ...t.columns.map((c) => el("th", { textContent: c }))));
    for (const row of t.rows || []) {
      tbody.append(el("tr", {}, ...row.map((cell) => el("td", { textContent: cell }))));
    }
    makeResizable($("#db-table-out"));
  } catch (e) {
    fail(e);
  }
}

$("#btn-db-tables").addEventListener("click", loadTables);
$("#btn-db-read").addEventListener("click", readTable);

// --- Render (file/folder explorer) ---
let currentRenderPath = null;

function renderMode() {
  return $("#rn-hex").checked ? "hex" : "auto";
}

function dataURLToBlobURL(dataURL) {
  const comma = dataURL.indexOf(",");
  const mime = dataURL.substring(5, dataURL.indexOf(";"));
  const bin = atob(dataURL.substring(comma + 1));
  const arr = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
  return URL.createObjectURL(new Blob([arr], { type: mime }));
}

function humanSize(n) {
  if (!n) return "";
  const u = ["B", "KB", "MB", "GB", "TB"];
  let i = 0;
  let v = n;
  while (v >= 1024 && i < u.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(i && v < 10 ? 1 : 0)} ${u[i]}`;
}

function displayRender(res) {
  const pane = $("#render-pane");
  $("#rn-mime").textContent = [res.mime, humanSize(res.size)].filter(Boolean).join(" · ");
  $("#btn-render-external").disabled = !currentRenderPath;
  pane.classList.toggle("wrap", $("#rn-wrap").checked);
  pane.replaceChildren();

  // A database file: offer opening it in the Database tab.
  if (res.mime === "application/vnd.sqlite3" && currentRenderPath) {
    const openDb = el("button", { className: "primary", textContent: "Open in Database →" });
    openDb.addEventListener("click", () => {
      $("#db-file").value = currentRenderPath;
      showView("db");
      loadTables();
    });
    pane.append(el("div", { className: "render-actions" }, openDb));
  }

  switch (res.kind) {
    case "image":
      pane.append(el("img", { className: "render-img", src: res.data_url, alt: res.name }));
      break;
    case "pdf":
      pane.append(el("iframe", { className: "render-pdf", src: dataURLToBlobURL(res.data_url) }));
      break;
    case "code": {
      const box = el("div", { className: "code-view" });
      box.innerHTML = res.html; // Chroma output for a local file
      pane.append(box);
      break;
    }
    case "toolarge":
      pane.append(el("div", { className: "render-empty" },
        el("div", { textContent: `${res.name} — ${humanSize(res.size)} is too large to preview inline.` }),
        el("div", { className: "hint", textContent: "Use the Hex view toggle, or Open externally." })
      ));
      break;
    default: // text | hex | error
      pane.append(el("pre", { className: "render-text", textContent: res.text }));
  }

  syncFindBar();
  showRenderDetails(res);
}

function clearRenderDetails() {
  const panel = $("#render-details");
  panel.classList.add("hidden");
  panel.replaceChildren();
}

// showRenderDetails fills the panel under the render window with the selected
// file's metadata (path, size, type, dates, permissions), mirroring the Apps
// details panel. The rendered content type (res.mime) is combined with the
// filesystem metadata from FileStat.
async function showRenderDetails(res) {
  const path = currentRenderPath;
  if (!path) { clearRenderDetails(); return; }
  let meta;
  try {
    meta = await gui().FileStat(path);
  } catch (e) {
    clearRenderDetails();
    return;
  }
  if (currentRenderPath !== path) return; // selection changed while awaiting

  const typeLabel =
    res.mime || (meta.ext ? meta.ext.replace(".", "").toUpperCase() + " file" : "file");
  const dl = el("dl", { className: "kv" });
  const add = (k, v) => {
    if (v) dl.append(el("dt", { textContent: k }), el("dd", { textContent: v }));
  };
  add("Name", meta.name);
  add("Full path", meta.path);
  add("Type", typeLabel);
  add("Size", `${humanSize(meta.size) || "0 B"} (${meta.size.toLocaleString()} bytes)`);
  add("Modified", meta.modified);
  add("Created", meta.created);
  add("Permissions", meta.mode);

  const panel = $("#render-details");
  panel.replaceChildren(el("h4", { textContent: "File details" }), dl);
  panel.classList.remove("hidden");
}

async function renderInPane(path, highlight) {
  currentRenderPath = path;
  $("#btn-render-external").disabled = false;
  const pane = $("#render-pane");
  pane.replaceChildren(el("div", { className: "render-empty", textContent: "Rendering…" }));
  try {
    displayRender(await gui().RenderPath(path, renderMode(), $("#rn-pretty").checked));
    if (highlight) highlightSecretInPane(highlight);
  } catch (e) {
    pane.replaceChildren();
    clearRenderDetails();
    fail(e);
  }
}

function treeNode(entry, depth) {
  const row = el("div", { className: "tree-row" });
  row.style.paddingLeft = 8 + depth * 14 + "px";
  const caret = el("span", { className: "tree-caret", textContent: entry.dir ? "▸" : "" });
  const icon = el("span", { className: "tree-icon", textContent: entry.dir ? "📁" : "📄" });
  row.append(caret, icon, el("span", { className: "tree-label", textContent: entry.name }));

  if (entry.dir) {
    const children = el("div", { className: "tree-children hidden" });
    let loaded = false;
    row.addEventListener("click", async (e) => {
      e.stopPropagation();
      const nowHidden = children.classList.toggle("hidden");
      caret.textContent = nowHidden ? "▸" : "▾";
      if (!loaded && !nowHidden) {
        loaded = true;
        try {
          for (const k of (await gui().ListDir(entry.path)) || []) children.append(treeNode(k, depth + 1));
        } catch (err) {
          fail(err);
        }
      }
    });
    return el("div", {}, row, children);
  }

  row.addEventListener("click", (e) => {
    e.stopPropagation();
    document.querySelectorAll("#render-tree .tree-row.selected").forEach((r) => r.classList.remove("selected"));
    row.classList.add("selected");
    renderInPane(entry.path);
  });
  return row;
}

async function openRenderFolder(root) {
  currentRenderPath = null;
  clearRenderDetails();
  $("#btn-render-external").disabled = true;
  $("#render-tree").classList.remove("hidden");
  $("#render-hsplit").classList.remove("hidden");
  const tree = $("#render-tree");
  tree.replaceChildren(el("div", { className: "render-empty", textContent: "Loading…" }));
  try {
    const entries = (await gui().ListDir(root)) || [];
    tree.replaceChildren(el("div", { className: "tree-root", textContent: root }));
    for (const e of entries) tree.append(treeNode(e, 0));
  } catch (err) {
    fail(err);
  }
  $("#render-pane").replaceChildren(el("div", { className: "render-empty", textContent: "Select a file from the tree." }));
}

$("#btn-render-file").addEventListener("click", async () => {
  try {
    const p = await gui().PickFile();
    if (!p) return;
    $("#render-tree").classList.add("hidden");
    $("#render-hsplit").classList.add("hidden");
    renderInPane(p);
  } catch (e) {
    fail(e);
  }
});
$("#btn-render-folder").addEventListener("click", async () => {
  try {
    const p = await gui().PickDirectory();
    if (p) openRenderFolder(p);
  } catch (e) {
    fail(e);
  }
});
$("#rn-hex").checked = localStorage.getItem("mobfi.render.hex") === "1";
$("#rn-hex").addEventListener("change", () => {
  localStorage.setItem("mobfi.render.hex", $("#rn-hex").checked ? "1" : "0");
  if (currentRenderPath) renderInPane(currentRenderPath);
});

// Wrap: pure CSS toggle, no re-render needed. Prettify: needs a re-render.
$("#rn-wrap").checked = localStorage.getItem("mobfi.render.wrap") === "1";
$("#rn-pretty").checked = localStorage.getItem("mobfi.render.pretty") !== "0"; // default on
$("#rn-wrap").addEventListener("change", () => {
  localStorage.setItem("mobfi.render.wrap", $("#rn-wrap").checked ? "1" : "0");
  $("#render-pane").classList.toggle("wrap", $("#rn-wrap").checked);
});
$("#rn-pretty").addEventListener("change", () => {
  localStorage.setItem("mobfi.render.pretty", $("#rn-pretty").checked ? "1" : "0");
  if (currentRenderPath) renderInPane(currentRenderPath);
});

// Find-in-file controls. Typing searches (debounced); Enter jumps to the first
// match of a new query and steps to the next on repeat; Shift+Enter goes back.
$("#rn-find-input").addEventListener("input", () => {
  clearTimeout(findDebounce);
  findDebounce = setTimeout(() => runFind($("#rn-find-input").value), 90);
});
$("#rn-find-input").addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    e.preventDefault();
    clearTimeout(findDebounce);
    const v = $("#rn-find-input").value;
    if (v !== findQuery) runFind(v); // first Enter: land on match 1
    else stepFind(e.shiftKey ? -1 : 1);
  } else if (e.key === "Escape") {
    e.preventDefault();
    $("#rn-find-input").value = "";
    runFind("");
  }
});
$("#rn-find-prev").addEventListener("click", () => stepFind(-1));
$("#rn-find-next").addEventListener("click", () => stepFind(1));

// Cmd/Ctrl+F focuses the find box while the Render tab shows a searchable file.
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && (e.key === "f" || e.key === "F")) {
    const rv = document.getElementById("view-render");
    if (rv && !rv.classList.contains("hidden") && findRoot()) {
      e.preventDefault();
      const inp = $("#rn-find-input");
      inp.focus();
      inp.select();
    }
  }
});

$("#btn-render-external").addEventListener("click", async () => {
  if (!currentRenderPath) return;
  try {
    await gui().OpenExternally(currentRenderPath);
  } catch (e) {
    fail(e);
  }
});

// --- Decode (Base64 / ASCII hex / URL string decoder) -----------------------
let decodeDebounce = null;

async function runDecode() {
  const box = $("#dec-results");
  const input = $("#dec-input").value;
  if (!input.trim()) {
    box.replaceChildren();
    return;
  }
  let results;
  try {
    results = await gui().Decode(input);
  } catch (e) {
    fail(e);
    return;
  }
  const pretty = $("#dec-pretty").checked;
  const cards = [];
  for (const r of results || []) cards.push(await decodeCard(r, pretty));
  box.replaceChildren(...cards);
}

function decodeCopyBtn(value) {
  const b = el("button", { className: "decode-copy", textContent: "Copy" });
  b.addEventListener("click", async () => {
    try {
      await gui().Copy(value);
      toast("copied", true);
    } catch (e) {
      fail(e);
    }
  });
  return b;
}

async function decodeCard(r, pretty) {
  const card = el("div", { className: "decode-card" + (r.ok ? "" : " empty") });
  const head = el("div", { className: "decode-head" });
  head.append(el("span", { className: "decode-name", textContent: r.name }));

  if (!r.ok) {
    card.append(head, el("div", { className: "decode-error", textContent: r.error || "no result" }));
    return card;
  }

  // Binary result: no syntax highlighting; show a best-effort value + hex.
  if (r.binary) {
    head.append(el("span", { className: "decode-badge", textContent: "binary" }));
    head.append(decodeCopyBtn(r.value));
    card.append(head);
    card.append(el("pre", { className: "decode-value", textContent: r.value }));
    if (r.hex) card.append(el("pre", { className: "decode-hex", textContent: r.hex }));
    return card;
  }

  // Text result: detect syntax and highlight (prettifying JSON/XML when on).
  let view = null;
  try {
    view = await gui().HighlightValue(r.value, pretty);
  } catch (e) {
    view = null;
  }
  if (view && view.lang) head.append(el("span", { className: "decode-badge", textContent: view.lang }));
  head.append(decodeCopyBtn(r.value));
  card.append(head);
  if (view && view.html) {
    const code = el("div", { className: "code-view decode-code" });
    code.innerHTML = view.html;
    card.append(code);
  } else {
    card.append(el("pre", { className: "decode-value", textContent: r.value }));
  }
  return card;
}

// sendToDecode fills the Decode tab with a string (from a scan finding, a file,
// or a database cell) and shows its decodings.
function sendToDecode(text) {
  showView("decode");
  $("#dec-input").value = text || "";
  runDecode();
  $("#dec-input").focus();
}

$("#dec-input").addEventListener("input", () => {
  clearTimeout(decodeDebounce);
  decodeDebounce = setTimeout(runDecode, 120);
});
$("#btn-decode-clear").addEventListener("click", () => {
  $("#dec-input").value = "";
  $("#dec-results").replaceChildren();
  $("#dec-input").focus();
});
$("#btn-decode-paste").addEventListener("click", async () => {
  try {
    const t = await gui().ClipboardGet();
    if (t) {
      $("#dec-input").value = t;
      runDecode();
    }
  } catch (e) {
    fail(e);
  }
});
// Prettify: persisted, defaults on (only an explicit "0" turns it off).
$("#dec-pretty").checked = localStorage.getItem("mobfi.decode.pretty") !== "0";
$("#dec-pretty").addEventListener("change", () => {
  localStorage.setItem("mobfi.decode.pretty", $("#dec-pretty").checked ? "1" : "0");
  runDecode();
});

// --- Keys (Keychain / Keystore dump) ----------------------------------------
const deviceTransport = new Map(); // device id -> transport, for state detection

function updateKeysPlatformUI() {
  const ios = $("#ky-platform").value === "ios";
  $("#ky-ios-backup").classList.toggle("hidden", !ios);
}

// useInKeys prefills the Keys tab from a detected device and switches to it.
function useInKeys(d) {
  $("#ky-device").value = d.id;
  $("#ky-platform").value = d.platform === "android" ? "android" : "ios";
  deviceTransport.set(d.id, d.transport);
  updateKeysPlatformUI();
  showView("keys");
  detectKeysState();
}

async function detectKeysState() {
  const id = $("#ky-device").value.trim();
  const platform = $("#ky-platform").value;
  const out = $("#ky-state-out");
  if (!id) {
    out.textContent = "";
    return;
  }
  out.textContent = "checking…";
  try {
    const state = await gui().DeviceRoot(id, platform, deviceTransport.get(id) || "");
    out.textContent = "state: " + state;
    out.dataset.state = state;
  } catch (e) {
    out.textContent = "state: unknown";
    out.dataset.state = "";
  }
}

let keysSortField = null;
let keysSortDir = 1;
let currentKeyItems = [];
const KEYS_COLUMNS = ["class", "service", "account", "accessible", "value", null]; // th index -> field

function renderKeys(res) {
  const notes = $("#keys-notes");
  notes.replaceChildren();
  currentKeyItems = [];
  clearRows("#keys-table tbody");
  if (!res) return;
  const head = el("div", { className: "keys-method" },
    el("strong", { textContent: "Method: " }), document.createTextNode(res.method || "—"));
  if (res.degraded) head.append(el("span", { className: "keys-degraded", textContent: "degraded" }));
  notes.append(head);
  const addList = (title, arr, cls) => {
    if (!arr || !arr.length) return;
    const box = el("div", { className: "keys-list " + cls });
    box.append(el("div", { className: "keys-list-title", textContent: title }));
    const ul = el("ul", {});
    for (const s of arr) ul.append(el("li", { textContent: s }));
    box.append(ul);
    notes.append(box);
  };
  addList("Notes", res.notes, "notes");
  addList("Limitations", res.limitations, "limits");

  currentKeyItems = res.items || [];
  renderKeysTable();
  requestAnimationFrame(fitTableHeights); // notes above changed the table's top
}

function renderKeysTable() {
  clearRows("#keys-table tbody");
  if (!currentKeyItems.length) {
    emptyRow("#keys-table tbody", 6, "no items recovered");
    return;
  }
  updateKeysSortIndicators();
  for (const it of sortRows(currentKeyItems, keysSortField, keysSortDir)) {
    $("#keys-table tbody").append(keysRow(it));
  }
}

function updateKeysSortIndicators() {
  document.querySelectorAll("#keys-table thead th.sortable").forEach((th) => {
    const arrow = th.dataset.field === keysSortField ? (keysSortDir === 1 ? " ▲" : " ▼") : "";
    const labelEl = th.querySelector(".th-label");
    if (labelEl) labelEl.textContent = th.dataset.label + arrow;
    else th.textContent = th.dataset.label + arrow;
  });
}

function setupKeysSorting() {
  const saved = JSON.parse(localStorage.getItem("mobfi.keys.sort") || "null");
  if (saved && saved.field) {
    keysSortField = saved.field;
    keysSortDir = saved.dir;
  }
  document.querySelectorAll("#keys-table thead th").forEach((th, i) => {
    const field = KEYS_COLUMNS[i];
    if (!field) return;
    th.classList.add("sortable");
    th.dataset.field = field;
    const labelEl = th.querySelector(".th-label");
    th.dataset.label = labelEl ? labelEl.textContent : th.textContent;
    th.addEventListener("click", () => {
      if (keysSortField === field) keysSortDir = -keysSortDir;
      else {
        keysSortField = field;
        keysSortDir = 1;
      }
      localStorage.setItem("mobfi.keys.sort", JSON.stringify({ field: keysSortField, dir: keysSortDir }));
      renderKeysTable();
    });
  });
  updateKeysSortIndicators();
}

function keysRow(it) {
  const extra = it.extra
    ? Object.entries(it.extra).map(([k, v]) => `${k}=${v}`).join(" · ")
    : "";
  const valCell = el("td", {});
  valCell.append(el("span", { className: it.binary ? "keys-binary" : "", textContent: it.value || "" }));

  const items = [
    {
      label: "Copy",
      primary: true,
      onClick: async () => {
        try {
          await gui().Copy(it.value || "");
          toast("copied", true);
        } catch (e) {
          fail(e);
        }
      },
    },
    { label: "Decode", title: "Send this value to the Decode tab", onClick: () => sendToDecode(it.value || "") },
  ];

  return wireRowMenu(el("tr", {},
    el("td", { textContent: it.class || "" }),
    el("td", { textContent: it.service || "" }),
    el("td", { textContent: [it.account, extra].filter(Boolean).join(" · ") }),
    el("td", { textContent: it.accessible || "" }),
    valCell,
    actionCell(items)
  ), items);
}

$("#ky-platform").addEventListener("change", () => {
  updateKeysPlatformUI();
  detectKeysState();
});
$("#ky-device").addEventListener("input", () => { $("#ky-state-out").textContent = ""; });
$("#ky-detect-state").addEventListener("click", detectKeysState);
wireBrowse("ky-backup-browse", "ky-backup", "dir");

$("#btn-keys-dump").addEventListener("click", async () => {
  const platform = $("#ky-platform").value;
  const device = $("#ky-device").value.trim();
  const state = $("#ky-state-out").dataset.state || "";
  const backup = $("#ky-backup").value.trim();
  const password = $("#ky-password").value;
  const reveal = $("#ky-reveal").checked;
  if (platform === "ios" && !backup && state !== "jailbroken") {
    toast("For a non-jailbroken iPhone, provide an encrypted backup directory + password.");
  }
  if (reveal) {
    let ok = false;
    try {
      ok = await gui().Confirm("Reveal secret values?", "This includes raw keychain/keystore secret values in the results. Only do this for authorized local analysis.");
    } catch (e) { ok = false; }
    if (!ok) return;
  }
  const btn = $("#btn-keys-dump");
  const status = $("#keys-status");
  btn.disabled = true;
  status.classList.add("busy");
  status.textContent = "Dumping…";
  // Drop the previous dump's results so a new dump doesn't show stale data.
  $("#keys-notes").replaceChildren();
  clearRows("#keys-table tbody");
  try {
    const res = await gui().DumpKeys(platform, device, deviceTransport.get(device) || "", state, backup, password, reveal);
    renderKeys(res);
    status.classList.remove("busy");
    const n = (res && res.items) ? res.items.length : 0;
    status.textContent = `${n} item(s)` + (res && res.degraded ? " (degraded)" : "");
  } catch (e) {
    status.classList.remove("busy");
    status.textContent = "";
    fail(e);
  } finally {
    btn.disabled = false;
  }
});

function makeHResizer(divider, target, storeKey) {
  if (!divider || !target) return;
  if (storeKey) {
    const saved = parseInt(localStorage.getItem(storeKey), 10);
    if (saved > 0) target.style.width = saved + "px";
  }
  divider.addEventListener("mousedown", (e) => {
    e.preventDefault();
    const startX = e.pageX;
    const startW = target.offsetWidth;
    const onMove = (ev) => {
      target.style.width = Math.max(140, startW + (ev.pageX - startX)) + "px";
    };
    const onUp = () => {
      document.removeEventListener("mousemove", onMove);
      document.removeEventListener("mouseup", onUp);
      document.body.style.cursor = "";
      if (storeKey) localStorage.setItem(storeKey, target.offsetWidth);
    };
    document.addEventListener("mousemove", onMove);
    document.addEventListener("mouseup", onUp);
    document.body.style.cursor = "col-resize";
  });
}
makeHResizer($("#render-hsplit"), $("#render-tree"), "mobfi.render.treeWidth");

// --- native path pickers ---
function wireBrowse(btnId, inputId, kind) {
  const btn = document.getElementById(btnId);
  if (!btn) return;
  btn.addEventListener("click", async () => {
    try {
      const p = kind === "dir" ? await gui().PickDirectory() : await gui().PickFile();
      if (p) document.getElementById(inputId).value = p;
    } catch (e) {
      fail(e);
    }
  });
}
wireBrowse("ex-dest-browse", "ex-dest", "dir");
wireBrowse("sc-root-browse", "sc-root", "dir");
wireBrowse("sc-known-browse", "sc-known", "file");
wireBrowse("df-a-browse", "df-a", "dir");
wireBrowse("df-b-browse", "df-b", "dir");
wireBrowse("db-file-browse", "db-file", "file");

// Resizable columns on the remaining static-header tables (widths persisted).
makeResizable($("#devices-table"), "mobfi.cols.devices");
makeResizable($("#scan-table"), "mobfi.cols.scan");
makeResizable($("#diff-table"), "mobfi.cols.diff");
makeResizable($("#keys-table"), "mobfi.cols.keys");
updateKeysPlatformUI();
setupScanSorting(); // after makeResizable, which creates the .th-label spans
setupDiffSorting();
setupKeysSorting();

// Draggable splitter between the app list and the details panel (persisted).
makeVResizer($("#apps-vsplit"), $("#apps-scroll"), "mobfi.apps.listHeight");

// Same, between the Render file view and its details panel (persisted).
makeVResizer($("#render-vsplit"), $(".render-split"), "mobfi.render.viewHeight");

// Persisted wrap toggle for a grid table (soft-wraps long cell contents).
function wireWrapToggle(checkboxId, tableSel, storeKey) {
  const cb = document.getElementById(checkboxId);
  const table = $(tableSel);
  if (!cb || !table) return;
  const apply = () => table.classList.toggle("wrap", cb.checked);
  cb.checked = localStorage.getItem(storeKey) === "1";
  apply();
  cb.addEventListener("change", () => {
    localStorage.setItem(storeKey, cb.checked ? "1" : "0");
    apply();
  });
}
wireWrapToggle("sc-wrap", "#scan-table", "mobfi.scan.wrap");
wireWrapToggle("df-wrap", "#diff-table", "mobfi.diff.wrap");

// Persist a simple checkbox setting across sessions.
function wirePersistentToggle(checkboxId, storeKey) {
  const cb = document.getElementById(checkboxId);
  if (!cb) return;
  cb.checked = localStorage.getItem(storeKey) === "1";
  cb.addEventListener("change", () => {
    localStorage.setItem(storeKey, cb.checked ? "1" : "0");
  });
}
wirePersistentToggle("sc-verify", "mobfi.scan.verify");
wirePersistentToggle("ky-reveal", "mobfi.keys.reveal");

// --- Console (adb shell / SSH via a PTY, rendered with xterm.js) ---
let consoleDevices = [];
let conId = null;
let conTerm = null;
let conFit = null;
let conOff = null;
let conExitOff = null;
let conBaseStatus = "";
let conFontSize = parseInt(localStorage.getItem("mobfi.console.fontSize"), 10) || 13;

function setConsoleStatus(text, busy = false) {
  const s = $("#con-status");
  s.classList.toggle("busy", !!busy);
  s.textContent = text || "";
}
function applyConsoleFont() {
  localStorage.setItem("mobfi.console.fontSize", String(conFontSize));
  if (conTerm) {
    conTerm.options.fontSize = conFontSize;
    if (conFit) conFit.fit();
  }
}
$("#con-clear").addEventListener("click", () => { if (conTerm) conTerm.clear(); });
$("#con-font-dec").addEventListener("click", () => { conFontSize = Math.max(8, conFontSize - 1); applyConsoleFont(); });
$("#con-font-inc").addEventListener("click", () => { conFontSize = Math.min(28, conFontSize + 1); applyConsoleFont(); });

// Copy the terminal selection / paste into the PTY.
$("#con-copy").addEventListener("click", async () => {
  const sel = conTerm && conTerm.getSelection();
  if (!sel) { toast("nothing selected"); return; }
  try { await gui().Copy(sel); toast("copied", true); } catch (e) { fail(e); }
});
$("#con-paste").addEventListener("click", async () => {
  if (!conId) return;
  try {
    const text = await gui().ClipboardGet();
    if (text) await gui().ConsoleWrite(conId, text);
    if (conTerm) conTerm.focus();
  } catch (e) { fail(e); }
});

// Command history (reconstructed from typed lines; persisted, deduped).
let conHistory = JSON.parse(localStorage.getItem("mobfi.console.history") || "[]");
let conLineBuf = "";
function trackHistory(data) {
  for (const ch of data) {
    if (ch === "\r" || ch === "\n") {
      const line = conLineBuf.trim();
      if (line) addHistory(line);
      conLineBuf = "";
    } else if (ch === "\x7f" || ch === "\b") {
      conLineBuf = conLineBuf.slice(0, -1);
    } else if (ch === "\x1b" || ch === "\x03" || ch === "\x15") {
      conLineBuf = ""; // escape sequence / Ctrl-C / Ctrl-U — abandon the line
    } else if (ch >= " ") {
      conLineBuf += ch;
    }
  }
}
function addHistory(cmd) {
  conHistory = conHistory.filter((c) => c !== cmd);
  conHistory.unshift(cmd);
  if (conHistory.length > 50) conHistory.length = 50;
  localStorage.setItem("mobfi.console.history", JSON.stringify(conHistory));
  refreshHistoryDropdown();
}
function refreshHistoryDropdown() {
  const sel = $("#con-history");
  sel.replaceChildren(el("option", { value: "", textContent: "History…" }));
  for (const c of conHistory) {
    sel.append(el("option", { value: c, textContent: c.length > 60 ? c.slice(0, 57) + "…" : c }));
  }
}
$("#con-history").addEventListener("change", () => {
  const cmd = $("#con-history").value;
  if (cmd && conId) {
    gui().ConsoleWrite(conId, cmd);
    if (conTerm) conTerm.focus();
  }
  $("#con-history").value = "";
});
refreshHistoryDropdown();

async function populateConsoleDevices() {
  const sel = $("#con-device");
  const prev = sel.value;
  // Arriving from the Devices tab's Console button: show the connecting state
  // up front so there's feedback during device detection too, not only once
  // startConsole runs.
  const wantAutoConnect = !!(pendingConsoleDeviceID && pendingConsoleConnect);
  if (wantAutoConnect) {
    $("#con-connect").disabled = true;
    $("#con-connect").textContent = "Connecting…";
    setConsoleStatus("connecting…", true);
  }
  try {
    consoleDevices = (await gui().DetectDevices()) || [];
  } catch (e) {
    consoleDevices = [];
  }
  sel.replaceChildren();
  if (!consoleDevices.length) {
    sel.append(el("option", { value: "", textContent: "(no devices — Detect first)" }));
  } else {
    consoleDevices.forEach((d, i) =>
      sel.append(el("option", { value: String(i), textContent: `${d.name || d.id} — ${d.platform}` }))
    );
    if (prev && sel.querySelector(`option[value="${prev}"]`)) sel.value = prev;
  }
  // Honour a device chosen via the Devices tab's "Console" button.
  let autoConnect = false;
  if (pendingConsoleDeviceID) {
    const i = consoleDevices.findIndex((d) => d.id === pendingConsoleDeviceID);
    if (i >= 0) {
      sel.value = String(i);
      autoConnect = pendingConsoleConnect;
    }
    pendingConsoleDeviceID = null;
    pendingConsoleConnect = false;
  }
  updateConsoleSsh();
  // Auto-connect from the Devices tab; on failure the device stays selected
  // (startConsole reports the reason) so the user can just press Connect.
  if (autoConnect) {
    startConsole({ quiet: true });
  } else if (wantAutoConnect) {
    // Wanted to auto-connect but the device isn't in the list; undo the
    // optimistic connecting state.
    $("#con-connect").textContent = "Connect";
    $("#con-connect").disabled = false;
    setConsoleStatus("");
  }
}

function selectedConsoleDevice() {
  const i = parseInt($("#con-device").value, 10);
  return isNaN(i) ? null : consoleDevices[i];
}

function updateConsoleSsh() {
  const d = selectedConsoleDevice();
  $("#con-ssh").classList.toggle("hidden", !(d && d.platform === "ios"));
}
$("#con-device").addEventListener("change", updateConsoleSsh);

function setConsoleConnected(on) {
  $("#con-connect").disabled = on;
  $("#con-disconnect").disabled = !on;
}

async function stopConsole() {
  if (conOff) { conOff(); conOff = null; }
  if (conExitOff) { conExitOff(); conExitOff = null; }
  if (conId) {
    try { await gui().ConsoleClose(conId); } catch (e) { /* already gone */ }
    conId = null;
  }
  if (conTerm) { conTerm.dispose(); conTerm = null; conFit = null; }
  setConsoleStatus("");
  setConsoleConnected(false);
}

async function startConsole(opts = {}) {
  const d = selectedConsoleDevice();
  if (!d) { toast("select a device"); return; }
  await stopConsole();

  // Show a connecting state: relabel + disable Connect and report progress
  // (with a spinner) while the adb shell / SSH session is being established.
  const connectBtn = $("#con-connect");
  connectBtn.disabled = true;
  connectBtn.textContent = "Connecting…";
  setConsoleStatus("connecting…", true);

  let logPath = "";
  if ($("#con-log").checked) {
    try { logPath = await gui().PickSaveFile("mobfi-console.log"); } catch (e) { logPath = ""; }
  }

  let info;
  try {
    info = await gui().ConsoleStart(d.id, d.platform,
      $("#con-user").value.trim(), $("#con-host").value.trim(), $("#con-port").value.trim(), logPath);
  } catch (e) {
    connectBtn.textContent = "Connect";
    setConsoleConnected(false);       // re-enable Connect for a retry
    setConsoleStatus("not connected"); // clears the connecting spinner
    if (opts.quiet) {
      toast("couldn't connect — device selected; press Connect to retry");
    } else {
      fail(e);
    }
    return;
  }
  const id = info.id;
  conId = id;
  conBaseStatus = info.status || "";
  setConsoleStatus(conBaseStatus + " · connected");

  const container = $("#con-term");
  container.replaceChildren();
  conTerm = new Terminal({
    fontSize: conFontSize,
    fontFamily: 'ui-monospace, "SF Mono", Menlo, monospace',
    cursorBlink: true,
    theme: { background: "#0b0f14", foreground: "#d7dee7" },
  });
  conFit = new FitAddon.FitAddon();
  conTerm.loadAddon(conFit);
  conTerm.open(container);
  conFit.fit();
  conTerm.focus();
  conTerm.onData((data) => { trackHistory(data); gui().ConsoleWrite(id, data); });
  conTerm.onResize(({ rows, cols }) => gui().ConsoleResize(id, rows, cols));
  gui().ConsoleResize(id, conTerm.rows, conTerm.cols);
  conOff = window.runtime.EventsOn("console:data:" + id, (data) => { if (conTerm) conTerm.write(data); });
  conExitOff = window.runtime.EventsOn("console:exit:" + id, () => {
    if (conTerm) conTerm.write("\r\n\x1b[90m[session ended]\x1b[0m\r\n");
    setConsoleStatus(conBaseStatus + " · session ended");
    setConsoleConnected(false);
  });
  connectBtn.textContent = "Connect";
  setConsoleConnected(true);
}

$("#con-connect").addEventListener("click", () => startConsole());
$("#con-disconnect").addEventListener("click", stopConsole);
window.addEventListener("resize", () => { if (conFit) conFit.fit(); });

// Persist window geometry shortly after a resize settles, so it's restored on
// next launch. The Go side reads the authoritative size/position and saves it.
// (Pure moves don't fire a resize event; those are captured on shutdown.)
let persistGeomTimer = null;
window.addEventListener("resize", () => {
  clearTimeout(persistGeomTimer);
  persistGeomTimer = setTimeout(() => {
    try { gui().PersistWindow(); } catch (e) { /* bindings not ready */ }
  }, 500);
});

// Scroll-to-top/bottom buttons for the main content area.
const mainEl = document.querySelector("main");
function updateJumpers() {
  const scrollable = mainEl.scrollHeight > mainEl.clientHeight + 24;
  $("#jumpers").classList.toggle("hidden", !scrollable);
}
$("#jump-top").addEventListener("click", () => mainEl.scrollTo({ top: 0, behavior: "smooth" }));
$("#jump-bottom").addEventListener("click", () => mainEl.scrollTo({ top: mainEl.scrollHeight, behavior: "smooth" }));
mainEl.addEventListener("scroll", updateJumpers);
// Re-check whenever the content changes size (view switches, list renders, …).
new ResizeObserver(updateJumpers).observe(mainEl);
new MutationObserver(updateJumpers).observe(mainEl, { childList: true, subtree: true });

// Start on the Devices step of the wizard.
showView("devices");

// Show the app version in the top bar (best-effort; bindings may lag).
(function showVersion() {
  const el = document.getElementById("app-version");
  if (!el) return;
  try {
    gui().Version().then((v) => { if (v) el.textContent = v; }).catch(() => {});
  } catch (e) { /* bindings not ready yet */ }
})();

// If a self-update ran just before this launch, toast its result once.
(function updateResult() {
  const run = (attempt) => {
    let p;
    try { p = gui().TakeUpdateResult(); }
    catch (e) { if (attempt < 10) setTimeout(() => run(attempt + 1), 500); return; }
    p.then((st) => { if (st && st.message) toast(st.message, !!st.ok); }).catch(() => {});
  };
  run(0);
})();

// --- Update progress overlay (shown while an update runs in-process) ---
function showUpdateOverlay() {
  const ov = document.getElementById("update-overlay");
  const log = document.getElementById("update-log");
  const fin = document.getElementById("update-final");
  if (!ov) return;
  if (log) log.textContent = "";
  if (fin) { fin.textContent = ""; fin.className = "update-final"; }
  ov.classList.remove("hidden");
}
// --- ANSI terminal colouring for the update log -----------------------------
// Rebuild tools (wails/pterm, git) emit ANSI escape codes. Render the colour
// codes as styled spans and strip the rest (cursor moves, spinner redraws) so
// they don't show up as raw escape gibberish in the overlay.
const ANSI_16 = [
  "#2e3436", "#c0392b", "#27ae60", "#d4a017", "#3465a4", "#8e44ad", "#16a085", "#c7c7c7",
  "#7f8c8d", "#e74c3c", "#2ecc71", "#f1c40f", "#5da8ff", "#9b59b6", "#1abc9c", "#ffffff",
];
function ansi256(n) {
  n = n | 0;
  if (n < 16) return ANSI_16[n];
  if (n < 232) {
    n -= 16;
    const c = (v) => (v ? 55 + v * 40 : 0);
    return `rgb(${c(Math.floor(n / 36))},${c(Math.floor((n % 36) / 6))},${c(n % 6)})`;
  }
  const v = 8 + (n - 232) * 10;
  return `rgb(${v},${v},${v})`;
}
function escHtml(s) {
  return s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
}
function applyAnsiCodes(st, codes) {
  for (let i = 0; i < codes.length; i++) {
    const c = codes[i];
    if (c === 0) { st.fg = null; st.bg = null; st.bold = false; st.italic = false; st.underline = false; }
    else if (c === 1) st.bold = true;
    else if (c === 22) st.bold = false;
    else if (c === 3) st.italic = true;
    else if (c === 23) st.italic = false;
    else if (c === 4) st.underline = true;
    else if (c === 24) st.underline = false;
    else if (c >= 30 && c <= 37) st.fg = ANSI_16[c - 30];
    else if (c >= 90 && c <= 97) st.fg = ANSI_16[c - 90 + 8];
    else if (c === 39) st.fg = null;
    else if (c >= 40 && c <= 47) st.bg = ANSI_16[c - 40];
    else if (c >= 100 && c <= 107) st.bg = ANSI_16[c - 100 + 8];
    else if (c === 49) st.bg = null;
    else if (c === 38 || c === 48) {
      const which = c === 38 ? "fg" : "bg";
      if (codes[i + 1] === 5) { st[which] = ansi256(codes[i + 2]); i += 2; }
      else if (codes[i + 1] === 2) { st[which] = `rgb(${codes[i + 2] | 0},${codes[i + 3] | 0},${codes[i + 4] | 0})`; i += 4; }
    }
  }
}
function ansiStyle(st) {
  const s = [];
  if (st.fg) s.push("color:" + st.fg);
  if (st.bg) s.push("background:" + st.bg);
  if (st.bold) s.push("font-weight:600");
  if (st.italic) s.push("font-style:italic");
  if (st.underline) s.push("text-decoration:underline");
  return s.join(";");
}
function ansiToHtml(line) {
  // Drop OSC sequences (e.g. window-title) and any CSI that isn't an SGR (final
  // byte 'm', 0x6d) -- cursor moves, line erases, spinner redraws.
  line = line.replace(/\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "");
  line = line.replace(/\x1b\[[0-9;?]*[\x40-\x6c\x6e-\x7e]/g, "");
  const st = { fg: null, bg: null, bold: false, italic: false, underline: false };
  const parts = line.split(/\x1b\[([0-9;]*)m/); // [text, codes, text, codes, ...]
  let html = "";
  for (let i = 0; i < parts.length; i++) {
    if (i % 2 === 1) {
      const codes = parts[i] === "" ? [0] : parts[i].split(";").map((x) => parseInt(x, 10) || 0);
      applyAnsiCodes(st, codes);
    } else if (parts[i]) {
      const style = ansiStyle(st);
      html += style ? `<span style="${style}">${escHtml(parts[i])}</span>` : escHtml(parts[i]);
    }
  }
  return html;
}
function appendUpdateLog(line) {
  const log = document.getElementById("update-log");
  if (!log) return;
  const div = document.createElement("div");
  div.innerHTML = ansiToHtml(line);
  log.appendChild(div);
  log.scrollTop = log.scrollHeight;
}
function finishUpdateOverlay(st) {
  const fin = document.getElementById("update-final");
  if (!fin) return;
  fin.className = "update-final " + (st && st.ok ? "ok" : "err");
  fin.textContent = (st && st.message) || (st && st.ok ? "Update complete." : "Update failed.");
  if (!st || !st.ok) {
    // On failure the app stays open; let the user dismiss the overlay.
    const close = el("button", { textContent: "Close" });
    close.addEventListener("click", () => document.getElementById("update-overlay").classList.add("hidden"));
    fin.append(close);
  } else {
    fin.append(" ", el("span", { className: "update-note", textContent: "Reopening…" }));
  }
}
if (window.runtime && window.runtime.EventsOn) {
  window.runtime.EventsOn("update:progress", (line) => appendUpdateLog(String(line)));
  window.runtime.EventsOn("update:done", (st) => finishUpdateOverlay(st));
}

// Check for a newer release (or a git checkout behind upstream) once at launch
// and show a dismissable banner. Purely advisory -- it opens the release page
// but never changes anything on disk. Fails silently when offline.
(function checkUpdate() {
  const banner = document.getElementById("update-banner");
  const msg = document.getElementById("update-msg");
  const viewBtn = document.getElementById("update-view");
  const applyBtn = document.getElementById("update-apply");
  const dismissBtn = document.getElementById("update-dismiss");
  if (!banner || !msg) return;

  const CHECK_INTERVAL_MS = 6 * 60 * 60 * 1000; // re-check every 6 hours
  let lastCheck = 0;
  let currentKey = null; // identifies the currently-offered update
  let dismissedKey = null; // the update the user dismissed (don't re-nag for it)

  dismissBtn.addEventListener("click", () => {
    banner.classList.add("hidden");
    dismissedKey = currentKey; // stay quiet until a newer update appears
  });

  // Run the update out-of-process: MobFI closes, a detached worker performs the
  // update (git pull + rebuild, or binary swap), then reopens the app. The
  // outcome is toasted on relaunch (see the update-result handler below).
  function wireApply(info) {
    if (!info.canApply) { applyBtn.classList.add("hidden"); return; }
    applyBtn.classList.remove("hidden");
    applyBtn.onclick = async () => {
      // Use the native dialog binding, not window.confirm() -- the Wails
      // webview (WKWebView on macOS) does not implement JS confirm/alert.
      let ok = false;
      try {
        ok = await gui().Confirm("Update MobFI", "Update MobFI now?\n\nMobFI will close, update, and reopen automatically. This can take a minute.");
      } catch (e) { ok = false; }
      if (!ok) return;
      showUpdateOverlay();
      try {
        await gui().StartUpdate();
      } catch (e) {
        finishUpdateOverlay({ ok: false, message: String((e && e.message) || e) });
      }
    };
  }

  let attempts = 0;
  const run = () => {
    lastCheck = Date.now();
    let p;
    try {
      p = gui().CheckForUpdate();
    } catch (e) {
      if (attempts++ < 10) setTimeout(run, 500); // bindings not ready yet
      return;
    }
    p.then((info) => {
      if (!info) return;
      const parts = [];
      if (info.available && info.latest) {
        parts.push(
          "MobFI <strong>v" + info.latest + "</strong> is available (you have v" +
            info.current + ")."
        );
      }
      if (info.gitCheckout && info.gitBehind > 0) {
        parts.push(
          "Your checkout is <strong>" + info.gitBehind + "</strong> commit" +
            (info.gitBehind === 1 ? "" : "s") + " behind " +
            (info.gitBranch ? info.gitBranch + "'s upstream" : "upstream") +
            " -- git pull to update."
        );
      }
      if (!parts.length) return; // up to date

      // Don't re-open a banner the user already dismissed for this same update;
      // a newer version (different key) will show again.
      currentKey = (info.latest || "") + "|" + (info.gitBehind || 0);
      if (currentKey === dismissedKey) return;

      msg.innerHTML = parts.join(" ");
      if (info.releaseUrl) {
        viewBtn.classList.remove("hidden");
        viewBtn.onclick = () => { try { gui().OpenURL(info.releaseUrl); } catch (e) {} };
      } else {
        viewBtn.classList.add("hidden");
      }
      wireApply(info);
      banner.classList.remove("hidden");
    }).catch(() => { /* offline or rate-limited: stay quiet */ });
  };
  run();
  // Keep checking in the background while the app runs, and when the window
  // regains focus after a while -- so a release published mid-session is noticed
  // without a restart.
  setInterval(run, CHECK_INTERVAL_MS);
  window.addEventListener("focus", () => {
    if (Date.now() - lastCheck > CHECK_INTERVAL_MS) run();
  });
})();

// Launch splash: fade out shortly after load, or on click / any key.
(function splash() {
  const s = document.getElementById("splash");
  if (!s) return;
  let done = false;
  const dismiss = () => {
    if (done) return;
    done = true;
    s.classList.add("hide");
    setTimeout(() => s.remove(), 600);
    window.removeEventListener("keydown", dismiss);
  };
  setTimeout(dismiss, 1500);
  s.addEventListener("click", dismiss);
  window.addEventListener("keydown", dismiss);
})();
