// Frontend for MobFI. Calls the Go bindings exposed by Wails at
// window.go.main.GUI.* (each returns a Promise). No build step: this is
// plain ES modules-free JavaScript served straight from frontend/dist.

const $ = (sel) => document.querySelector(sel);
const el = (tag, props = {}, ...children) => {
  const n = Object.assign(document.createElement(tag), props);
  for (const c of children) n.append(c);
  return n;
};

function gui() {
  // window.go is injected by the Wails runtime once bindings are ready.
  if (!window.go || !window.go.main || !window.go.main.GUI) {
    throw new Error("Wails bindings not ready yet");
  }
  return window.go.main.GUI;
}

let toastTimer;
function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.classList.remove("hidden");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => t.classList.add("hidden"), 4000);
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
}

document.querySelectorAll(".tab").forEach((t) =>
  t.addEventListener("click", () => showView(t.dataset.view))
);

function clearRows(tbodySel) {
  $(tbodySel).replaceChildren();
}

function emptyRow(tbodySel, cols, text) {
  const tr = el("tr", { className: "empty" });
  tr.append(el("td", { colSpan: cols, textContent: text }));
  $(tbodySel).append(tr);
}

// --- Devices ---
$("#btn-detect").addEventListener("click", async () => {
  clearRows("#devices-table tbody");
  try {
    const devices = await gui().DetectDevices();
    if (!devices || devices.length === 0) {
      emptyRow("#devices-table tbody", 6, "no devices detected");
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
      const actions = el("td", {}, appsBtn);
      actions.append(" ", useBtn);
      const tr = el("tr", {},
        el("td", { textContent: d.id }),
        el("td", { textContent: d.name }),
        el("td", { textContent: d.platform }),
        el("td", { textContent: d.transport }),
        el("td", { className: `state-${d.state}`, textContent: d.state }),
        actions
      );
      $("#devices-table tbody").append(tr);
    }
  } catch (e) {
    fail(e);
  }
});

// --- Apps ---
async function loadApps(deviceID) {
  showView("apps");
  $("#apps-device").textContent = deviceID;
  clearRows("#apps-table tbody");
  try {
    const apps = await gui().ListApps(deviceID);
    if (!apps || apps.length === 0) {
      emptyRow("#apps-table tbody", 5, "no apps found");
      return;
    }
    for (const a of apps) {
      const useBtn = el("button", { textContent: "Use in Extract" });
      useBtn.addEventListener("click", () => {
        $("#ex-device").value = deviceID;
        $("#ex-bundle").value = a.bundle_id;
        showView("extract");
      });
      $("#apps-table tbody").append(el("tr", {},
        el("td", { textContent: a.bundle_id }),
        el("td", { textContent: a.name }),
        el("td", { textContent: a.data_path }),
        el("td", { textContent: a.install_path }),
        el("td", {}, useBtn)
      ));
    }
  } catch (e) {
    fail(e);
  }
}

// --- Extract ---
$("#btn-extract").addEventListener("click", async () => {
  const out = $("#extract-out");
  out.textContent = "extracting…";
  try {
    const res = await gui().ExtractApp(
      $("#ex-device").value.trim(),
      $("#ex-bundle").value.trim(),
      $("#ex-dest").value.trim(),
      $("#ex-scope").value
    );
    let text = `Extracted ${res.file_count} file(s), ${res.byte_count} byte(s)\nto ${res.root}\n`;
    if (res.skipped && res.skipped.length) {
      text += `\nSkipped ${res.skipped.length} path(s):\n`;
      for (const s of res.skipped) text += `  ${s.path}: ${s.reason}\n`;
    }
    out.textContent = text;
  } catch (e) {
    out.textContent = "";
    fail(e);
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
      emptyRow("#scan-table tbody", 4, "no secrets found");
      return;
    }
    for (const f of findings) {
      $("#scan-table tbody").append(el("tr", {},
        el("td", { textContent: f.rule_id }),
        el("td", { textContent: f.path }),
        el("td", { textContent: f.line }),
        el("td", { textContent: f.match })
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

// Start on the Devices step of the wizard.
showView("devices");
