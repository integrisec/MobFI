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

function makeResizable(table) {
  if (!table) return;
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
      };
      document.addEventListener("mousemove", onMove);
      document.addEventListener("mouseup", onUp);
      document.body.style.cursor = "col-resize";
    });
  });
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
}

function clearRows(tbodySel) {
  $(tbodySel).replaceChildren();
}

function emptyRow(tbodySel, cols, text) {
  const tr = el("tr", { className: "empty" });
  tr.append(el("td", { colSpan: cols, textContent: text }));
  $(tbodySel).append(tr);
}

// --- Devices (auto-refreshing) ---
const rootCache = new Map(); // deviceID -> "rooted"/"jailbroken"/...
let devicePollTimer = null;
let lastDevicesSig = null;

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
  const sig = JSON.stringify(devices.map((d) => [d.id, d.platform, d.transport, d.state, d.name]));
  if (!force && sig === lastDevicesSig) return; // nothing changed
  lastDevicesSig = sig;
  renderDevices(devices);
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
    const status = await gui().DeviceRoot(d.id, d.platform);
    rootCache.set(d.id, status);
    applyRootCell(cell, status);
  } catch (e) {
    applyRootCell(cell, "unknown");
  }
}

function renderDevices(devices) {
  clearRows("#devices-table tbody");
  if (devices.length === 0) {
    emptyRow("#devices-table tbody", 7, "no devices detected");
    return;
  }
  for (const d of devices) {
    const appsBtn = el("button", { textContent: "List apps" });
    appsBtn.addEventListener("click", () => loadApps(d.id));
    const useBtn = el("button", { textContent: "Use in Extract" });
    useBtn.addEventListener("click", () => {
      $("#ex-device").value = d.id;
      $("#ex-bundle").focus();
      showView("extract");
    });
    const actions = el("td", { className: "col-actions" }, appsBtn);
    actions.append(" ", useBtn);

    const rootCell = el("td", {});
    applyRootCell(rootCell, rootCache.get(d.id));

    $("#devices-table tbody").append(el("tr", {},
      el("td", { textContent: d.id }),
      el("td", { textContent: d.name }),
      el("td", { textContent: d.platform }),
      el("td", { textContent: d.transport }),
      el("td", { className: `state-${d.state}`, textContent: d.state }),
      rootCell,
      actions
    ));

    if (!rootCache.has(d.id)) fetchDeviceRoot(d, rootCell);
  }
}

// Manual Detect re-checks everything, including root/jailbreak status.
$("#btn-detect").addEventListener("click", () => {
  rootCache.clear();
  refreshDevices(true);
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
  }
  if (meta.version) {
    const verEl = row.querySelector(".app-version");
    if (verEl) verEl.textContent = meta.version;
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
  clearRows("#apps-table tbody");
  try {
    currentApps = (await gui().ListApps(deviceID, $("#apps-system").checked)) || [];
  } catch (e) {
    currentApps = [];
    fail(e);
  }
  renderApps();
}

function sortApps(rows) {
  if (!appSortField) return rows;
  const f = appSortField;
  return [...rows].sort((a, b) => {
    const av = (a[f] ?? "").toString();
    const bv = (b[f] ?? "").toString();
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
    const copyBtn = el("button", { textContent: "Copy" });
    copyBtn.addEventListener("click", async () => {
      try {
        await gui().Copy(a.bundle_id);
        toast("copied " + a.bundle_id, true);
      } catch (e) {
        fail(e);
      }
    });
    const useBtn = el("button", { textContent: "Use in Extract" });
    useBtn.addEventListener("click", () => {
      $("#ex-device").value = currentAppsDevice;
      $("#ex-bundle").value = a.bundle_id;
      showView("extract");
    });
    const actions = el("td", { className: "col-actions" }, copyBtn);
    actions.append(" ", useBtn);

    const row = el("tr", {},
      el("td", { className: "col-icon app-icon" }, avatar(a)),
      el("td", { textContent: a.bundle_id }),
      el("td", { className: "app-name", textContent: a.name || humanize(a.bundle_id) }),
      el("td", { className: "app-version", textContent: a.version || "" }),
      el("td", { textContent: a.data_path }),
      el("td", { textContent: a.install_path }),
      actions
    );
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
makeResizable($("#apps-table")); // must precede sorting (creates .th-label)
setupAppsSorting();
$("#apps-search").addEventListener("input", renderApps);
$("#apps-system").addEventListener("change", () => {
  if (currentAppsDevice) loadApps(currentAppsDevice);
});

// --- Extract ---
$("#btn-extract").addEventListener("click", async () => {
  const out = $("#extract-out");
  const btn = $("#btn-extract");
  out.textContent = "Extracting… (starting)";
  btn.disabled = true;

  // Live progress from the backend.
  const off = window.runtime.EventsOn("extract:progress", (p) => {
    out.textContent =
      `Extracting… ${p.files} file(s), ${p.bytes.toLocaleString()} byte(s)\n${p.path}`;
  });

  try {
    const res = await gui().ExtractApp(
      $("#ex-device").value.trim(),
      $("#ex-bundle").value.trim(),
      $("#ex-dest").value.trim(),
      $("#ex-scope").value
    );
    let text = `Extracted ${res.file_count} file(s), ${res.byte_count.toLocaleString()} byte(s)\nto ${res.root}\n`;
    if (res.skipped && res.skipped.length) {
      text += `\nSkipped ${res.skipped.length} path(s):\n`;
      for (const s of res.skipped) text += `  ${s.path}: ${s.reason}\n`;
    }
    out.textContent = text;
  } catch (e) {
    out.textContent = "";
    fail(e);
  } finally {
    off();
    btn.disabled = false;
  }
});

// --- Scan ---
$("#btn-scan").addEventListener("click", async () => {
  clearRows("#scan-table tbody");
  try {
    const known = $("#sc-known").value.trim();
    if (known) await gui().AddKnownSecrets(known);
    const findings = await gui().ScanSecrets($("#sc-root").value.trim());
    if (!findings || findings.length === 0) {
      emptyRow("#scan-table tbody", 5, "no secrets found");
      return;
    }
    for (const f of findings) {
      const matchCell = el("td", { className: "revealable", textContent: f.match, title: "click to reveal" });
      let revealed = false;
      matchCell.addEventListener("click", () => {
        revealed = !revealed;
        matchCell.textContent = revealed ? (f.secret || f.match) : f.match;
      });
      const copyBtn = el("button", { textContent: "Copy" });
      copyBtn.addEventListener("click", async () => {
        try {
          await gui().Copy(f.secret || f.match);
          toast("copied secret", true);
        } catch (e) {
          fail(e);
        }
      });
      $("#scan-table tbody").append(el("tr", {},
        el("td", { textContent: f.rule_id }),
        el("td", { textContent: f.path }),
        el("td", { textContent: f.line }),
        matchCell,
        el("td", { className: "col-actions" }, copyBtn)
      ));
    }
  } catch (e) {
    fail(e);
  }
});

// --- Diff ---
$("#btn-diff").addEventListener("click", async () => {
  clearRows("#diff-table tbody");
  try {
    const res = await gui().Diff($("#df-a").value.trim(), $("#df-b").value.trim());
    if (!res.changes || res.changes.length === 0) {
      emptyRow("#diff-table tbody", 3, "no differences");
      return;
    }
    for (const c of res.changes) {
      $("#diff-table tbody").append(el("tr", {},
        el("td", {}, el("span", { className: `pill ${c.kind}`, textContent: c.kind })),
        el("td", { textContent: c.path }),
        el("td", { textContent: c.detail || "" })
      ));
    }
  } catch (e) {
    fail(e);
  }
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

// --- Render ---
$("#btn-render").addEventListener("click", async () => {
  try {
    const v = await gui().Render($("#rn-file").value.trim());
    $("#rn-mime").textContent = v.mime;
    $("#render-out").textContent = v.text;
  } catch (e) {
    $("#render-out").textContent = "";
    fail(e);
  }
});

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
wireBrowse("rn-file-browse", "rn-file", "file");

// Resizable columns on the remaining static-header tables.
["#devices-table", "#scan-table", "#diff-table"].forEach((sel) => makeResizable($(sel)));

// Draggable splitter between the app list and the details panel (persisted).
makeVResizer($("#apps-vsplit"), $("#apps-scroll"), "mobfi.apps.listHeight");

// Start on the Devices step of the wizard.
showView("devices");
