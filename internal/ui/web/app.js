"use strict";

const $ = (s) => document.querySelector(s);

// Build DOM without innerHTML so project names, paths and logs can never inject markup.
function h(tag, props, ...kids) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (k === "class") el.className = v;
    else if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (v === true) el.setAttribute(k, "");
    else if (v !== false && v != null) el.setAttribute(k, v);
  }
  for (const kid of kids.flat()) {
    if (kid == null || kid === false) continue;
    el.append(kid.nodeType ? kid : document.createTextNode(String(kid)));
  }
  return el;
}

async function api(path, body) {
  const opt = body === undefined ? {} : {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Hyper": "1" },
    body: JSON.stringify(body),
  };
  const res = await fetch(path, opt);
  let data = null;
  try { data = await res.json(); } catch (_) { /* empty body */ }
  if (!res.ok) {
    const err = new Error((data && data.error) || res.statusText || "Request failed");
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

let state = null;
let health = [];
let jobTimer = null;
let busy = false;

function toast(msg) {
  const t = $("#toast");
  t.textContent = msg;
  t.hidden = false;
  clearTimeout(toast.t);
  toast.t = setTimeout(() => (t.hidden = true), 5000);
}

/* ---------- rendering ---------- */

function pill(text, kind) {
  return h("span", { class: "pill " + (kind || "") }, text);
}

function renderSummary() {
  const s = state;
  const online = s.sites.filter((x) => x.online).length;
  $("#summary").textContent = s.installed
    ? `${online} of ${s.sites.length} project${s.sites.length === 1 ? "" : "s"} online`
    : "Runtime not installed";
  $("#btn-stopall").disabled = busy || !s.any_running;
  $("#btn-add").disabled = busy || !s.installed;
  $("#btn-startall").disabled = busy || !s.installed || s.sites.length === 0;
  $("#btn-www").disabled = !s.installed;
  $("#www-path").textContent = s.www ? "Projects folder: " + s.www : "";
}

function renderBanner() {
  const b = $("#banner");
  b.replaceChildren();
  if (!state.installed) {
    b.append(h("div", { class: "banner warn" },
      h("div", {}, h("b", {}, "Welcome. Hyper Stack is not installed yet. "),
        "Choose a folder: PHP, MySQL, phpMyAdmin and the rest go inside it (about 300 MB), with a www folder for all your projects. Windows asks for administrator permission once."),
      h("button", { class: "btn primary", disabled: busy, onclick: installWizard }, "Choose folder and install")));
  } else if (!state.hosts_writable) {
    b.append(h("div", { class: "banner warn" },
      h("div", {}, h("b", {}, "Setup is incomplete. "), "Hyper Stack cannot edit the hosts file or the local certificate is not trusted."),
      h("button", { class: "btn primary", disabled: busy, onclick: () => start("/api/doctor-fix", {}) }, "Fix now")));
  }
}

// "Start at login": saved straight away; the project then starts whenever Hyper Stack starts at sign-in.
function autostartSwitch(p) {
  const box = h("input", { type: "checkbox", checked: p.autostart, disabled: !!p.error, onchange: async (ev) => {
    try {
      await api("/api/project-autostart", { path: p.root, enabled: ev.target.checked });
      toast(ev.target.checked ? p.name + " will start when you sign in" : p.name + " will no longer start at sign-in");
      refresh();
    } catch (e) { ev.target.checked = !ev.target.checked; toast(e.message); }
  } });
  if (p.autostart) box.setAttribute("checked", "");
  return h("label", { class: "switch small" }, box, h("span", {}, "Start at login"));
}

function discoveredCard(p) {
  return h("div", { class: "card project" },
    h("div", { class: "name" }, p.name, pill(p.error ? "Needs attention" : "Not started", p.error ? "bad" : "")),
    !p.error && h("div", { class: "url" }, p.domain),
    h("div", { class: "meta" },
      !p.error && h("span", { class: "tag" }, "PHP " + p.php),
      !p.error && h("span", { class: "tag" }, "DB " + p.database),
      !p.error && h("span", { class: "tag" }, p.auto ? "auto-detected" : "hyper.json")),
    p.error && h("div", { class: "why" }, p.error),
    h("div", { class: "path mono" }, p.root),
    autostartSwitch(p),
    h("div", { class: "btnrow" },
      h("button", { class: "btn primary", disabled: busy || !!p.error, onclick: () => start("/api/up", { path: p.root }) }, "Start")));
}

function projectCard(p) {
  if (p.discovered) return discoveredCard(p);
  const running = p.workers.filter((w) => w.running).length;
  const workersDown = p.online && p.workers.length > 0 && running < p.workers.length;
  return h("div", { class: "card project" },
    h("div", { class: "name" }, p.name,
      pill(p.online ? "Online" : "Offline", p.online ? "ok" : ""),
      workersDown && pill("Workers down", "warn"),
      !p.root_exists && pill("Folder missing", "bad")),
    h("a", { class: "url", href: p.url, target: "_blank", rel: "noopener" }, p.url),
    h("div", { class: "meta" },
      h("span", { class: "tag" }, "PHP " + p.php),
      h("span", { class: "tag" }, "DB " + p.database),
      p.workers.length > 0 && h("span", { class: "tag" }, `${running}/${p.workers.length} workers`)),
    h("div", { class: "path mono" }, p.root),
    autostartSwitch(p),
    h("div", { class: "btnrow" },
      workersDown && h("button", { class: "btn primary", disabled: busy, onclick: () => startProject(p.name) }, "Restart workers"),
      p.online
        ? h("button", { class: "btn", disabled: busy, onclick: () => start("/api/stop", { name: p.name }) }, "Stop")
        : h("button", { class: "btn primary", disabled: busy || !p.root_exists, onclick: () => startProject(p.name) }, "Start"),
      h("button", { class: "btn", disabled: !p.online, onclick: () => window.open(p.url, "_blank", "noopener") }, "Open site"),
      h("button", { class: "btn", disabled: busy, onclick: () => start("/api/db-open", { name: p.name }) }, "Database"),
      h("button", { class: "btn", disabled: busy, onclick: () => importDump(p) }, "Import SQL"),
      h("button", { class: "btn", onclick: () => showLogsFor(p) }, "Logs"),
      h("button", { class: "btn danger", disabled: busy, onclick: () => removeProject(p) }, "Remove")));
}

function renderProjects() {
  const box = $("#projects");
  box.replaceChildren();
  if (state.sites.length === 0) {
    box.append(h("div", { class: "empty" },
      state.installed ? "No projects yet. Put a folder inside your www folder (use Open www folder), then click Start. Every folder becomes <folder>.test."
        : "Install the runtime first, then add your projects."));
    return;
  }
  state.sites.forEach((p) => box.append(projectCard(p)));
}

function renderServices() {
  const tb = $("#services tbody");
  tb.replaceChildren();
  if (state.services.length === 0) {
    tb.append(h("tr", {}, h("td", { colspan: 4, class: "muted" }, "Nothing installed yet.")));
    return;
  }
  state.services.forEach((s) => tb.append(h("tr", {},
    h("td", {}, s.name),
    h("td", {}, pill(s.running ? "Running" : "Stopped", s.running ? "ok" : "")),
    h("td", { class: "mono" }, s.port),
    h("td", { class: "mono" }, s.running ? s.pid : "-"))));
}

function renderHealth() {
  const ul = $("#health");
  ul.replaceChildren();
  health.forEach((f) => ul.append(h("li", {},
    h("span", { class: "dot " + (f.ok ? "" : "bad") }),
    h("div", {}, h("div", {}, f.title),
      !f.ok && f.detail && h("div", { class: "why mono" }, f.detail),
      !f.ok && f.fix && h("div", { class: "why" }, "Fix: " + f.fix)))));
  $("#btn-fix").hidden = !state.installed || !health.some((f) => !f.ok && /doctor --fix/.test(f.fix || ""));
}

function renderLogs() {
  const sel = $("#log-file");
  const cur = sel.value;
  const names = state.log_files || [];
  if (names.join("|") !== [...sel.options].map((o) => o.value).join("|")) {
    sel.replaceChildren(...names.map((n) => h("option", { value: n }, n)));
    if (names.includes(cur)) sel.value = cur;
  }
}

function renderPorts() {
  const f = $("#ports");
  f.closest(".card").hidden = !state.installed; // before install the ports belong in the install wizard
  if (document.activeElement && f.contains(document.activeElement)) return; // don't overwrite typing
  f.http_port.value = state.settings.http_port;
  f.https_port.value = state.settings.https_port;
  f.mysql_port.value = state.settings.mysql_port;
  $("#datadir").textContent = "Data folder: " + state.data_dir;
}

function renderStartup() {
  const box = $("#login-autostart");
  if (document.activeElement !== box) box.checked = !!state.login_autostart;
  box.disabled = !state.installed;
  const n = state.sites.filter((s) => s.autostart).length;
  $("#autostart-projects").textContent = n === 0
    ? "No projects start at login yet. Use the Start at login switch on a project."
    : n + (n === 1 ? " project starts" : " projects start") + " at login: " +
      state.sites.filter((s) => s.autostart).map((s) => s.name).join(", ") + ".";
}

function render() {
  renderSummary();
  renderStartup();
  renderBanner();
  renderProjects();
  renderServices();
  renderLogs();
  renderPorts();
}

/* ---------- data ---------- */

async function refresh() {
  try {
    state = await api("/api/state");
    render();
  } catch (e) {
    $("#summary").textContent = "Dashboard lost its connection. Reopen it from the tray icon.";
  }
}

async function loadHealth() {
  try {
    health = await api("/api/doctor");
    renderHealth();
  } catch (e) {
    toast(e.message);
  }
}

/* ---------- jobs ---------- */

async function start(path, body) {
  try {
    const r = await api(path, body);
    watchJob(r.job);
  } catch (e) {
    if (e.status === 409 && e.data && e.data.root) return confirmSetup(e.data);
    toast(e.message);
  }
}

function watchJob(id) {
  clearInterval(jobTimer);
  busy = true;
  $("#job").hidden = false;
  render();
  const tick = async () => {
    try {
      const j = await api("/api/job?id=" + id);
      $("#job-title").textContent = j.title;
      const st = $("#job-status");
      st.textContent = j.status === "running" ? `Running… ${j.seconds}s` : j.status === "done" ? "Done" : "Failed";
      st.className = "pill " + (j.status === "done" ? "ok" : j.status === "failed" ? "bad" : "warn");
      const log = $("#job-log");
      const atEnd = log.scrollTop + log.clientHeight >= log.scrollHeight - 24;
      log.textContent = j.log + (j.error ? "\n\nError: " + j.error : "");
      if (atEnd) log.scrollTop = log.scrollHeight;
      if (j.status !== "running") {
        clearInterval(jobTimer);
        busy = false;
        await refresh();
        loadHealth();
        if (j.status === "failed") toast(j.error);
      }
    } catch (e) {
      clearInterval(jobTimer);
      busy = false;
      toast(e.message);
    }
  };
  tick();
  jobTimer = setInterval(tick, 700);
}

/* ---------- modals ---------- */

async function installWizard() {
  const input = h("input", { type: "text", id: "install-dir", value: state.default_dir || "C:/HyperStack", style: "flex:1;min-width:0" });
  const browse = h("button", { class: "btn small", onclick: async () => {
    try { const r = await api("/api/pick-folder", {}); if (r.path) input.value = r.path; } catch (e) { toast(e.message); }
  } }, "Browse...");
  const port = (id, v) => h("input", { type: "number", id, value: v, min: 1, max: 65535, style: "width:92px" });
  const http = port("p-http", 80), https = port("p-https", 443), mysql = port("p-mysql", 3306);
  const advanced = h("details", {},
    h("summary", { class: "muted" }, "Advanced: ports (change only if Laragon or XAMPP is running)"),
    h("div", { class: "row", style: "margin-top:8px" },
      h("label", { class: "muted" }, "HTTP ", http), h("label", { class: "muted" }, "HTTPS ", https), h("label", { class: "muted" }, "MySQL ", mysql)));
  modal("Where should Hyper Stack be installed?", [
    h("p", {}, "This one folder will contain PHP, MySQL, phpMyAdmin, nginx and a www folder for all your projects. Use a short path with plain English letters."),
    h("div", { class: "row" }, input, browse),
    h("p", { class: "muted" }, "Every folder you create inside www becomes https://<folder>.test automatically."),
    advanced,
  ], "Install", () => start("/api/install", {
    dir: input.value.trim(), http: +http.value || 0, https: +https.value || 0, mysql: +mysql.value || 0,
  }));
}

function modal(title, bodyNodes, okText, onOk) {
  $("#modal-title").textContent = title;
  $("#modal-body").replaceChildren(...bodyNodes);
  $("#modal-ok").textContent = okText;
  $("#modal-ok").hidden = !onOk;
  $("#modal-ok").onclick = () => { $("#modal").hidden = true; onOk && onOk(); };
  $("#modal-cancel").onclick = () => ($("#modal").hidden = true);
  $("#modal").hidden = false;
}

function cmdList(label, items) {
  if (!items || items.length === 0) return null;
  return h("div", {}, h("div", { class: "muted" }, label), items.map((c) => h("code", {}, c)));
}

function confirmSetup(ins) {
  modal("Review before running " + ins.name, [
    h("p", {}, "This project's hyper.json will run the commands below on your computer. Only continue if you trust the repository."),
    cmdList("First-time setup", ins.setup),
    cmdList("Background workers", ins.workers),
  ], "I trust it, start", () => start("/api/up", { path: ins.root, confirm: true }));
}

/* ---------- actions ---------- */

async function startProject(name) {
  start("/api/up", { name });
}

async function addProject() {
  try {
    const { path } = await api("/api/pick-folder", {});
    if (!path) return;
    const ins = await api("/api/inspect", { path });
    modal("Add " + ins.name, [
      h("div", { class: "meta" },
        h("span", { class: "tag" }, ins.domain), h("span", { class: "tag" }, "PHP " + ins.php),
        h("span", { class: "tag" }, "DB " + ins.database)),
      h("div", { class: "path mono" }, ins.root),
      ins.needs_confirm && h("p", {}, "It will run these setup commands once. Only continue if you trust the repository."),
      cmdList("First-time setup", ins.needs_confirm ? ins.setup : []),
      cmdList("Background workers", ins.workers),
    ], "Start project", () => start("/api/up", { path: ins.root, confirm: true }));
  } catch (e) {
    toast(e.message);
  }
}

async function importDump(p) {
  try {
    const { path } = await api("/api/pick-file", {});
    if (!path) return;
    modal("Import into " + p.database, [
      h("p", {}, "This replaces any tables in " + p.database + " that exist in the file."),
      h("div", { class: "path mono" }, path),
    ], "Import", () => start("/api/import", { name: p.name, path }));
  } catch (e) {
    toast(e.message);
  }
}

function removeProject(p) {
  modal("Remove " + p.name + "?", [
    h("p", {}, "It is stopped and removed from this list. Its folder and database are not deleted."),
  ], "Remove", () => start("/api/forget", { name: p.name }));
}

async function showLogs(file) {
  if (!file) return;
  try {
    const r = await api("/api/logs?file=" + encodeURIComponent(file));
    const v = $("#log-view");
    v.textContent = r.text || "(empty)";
    v.scrollTop = v.scrollHeight;
  } catch (e) {
    $("#log-view").textContent = e.message;
  }
}

function showLogsFor(p) {
  const wanted = (state.log_files || []).find((n) => n.startsWith(p.name + "-worker")) ||
    (state.log_files || []).find((n) => n === "nginx-error.log");
  if (wanted) { $("#log-file").value = wanted; showLogs(wanted); }
  $("#log-view").scrollIntoView({ behavior: "smooth", block: "center" });
}

/* ---------- wiring ---------- */

$("#btn-add").onclick = addProject;
$("#login-autostart").onchange = async (ev) => {
  try {
    await api("/api/autostart", { enabled: ev.target.checked });
    toast(ev.target.checked ? "Hyper Stack will start when you sign in to Windows" : "Hyper Stack will no longer start at sign-in");
  } catch (e) { ev.target.checked = !ev.target.checked; toast(e.message); }
  refresh();
};
$("#btn-www").onclick = async () => { try { await api("/api/open-www", {}); } catch (e) { toast(e.message); } };
$("#btn-startall").onclick = () => start("/api/up-all", {});
$("#btn-stopall").onclick = () => modal("Stop all services?", [
  h("p", {}, "nginx, PHP and MySQL stop, so every project goes offline. Your files and databases are kept."),
], "Stop all", () => start("/api/stop-all", {}));
$("#btn-recheck").onclick = loadHealth;
$("#btn-fix").onclick = () => start("/api/doctor-fix", {});
$("#btn-log").onclick = () => showLogs($("#log-file").value);
$("#log-file").onchange = () => showLogs($("#log-file").value);
$("#job-close").onclick = () => ($("#job").hidden = true);
$("#ports").onsubmit = async (ev) => {
  ev.preventDefault();
  const f = ev.target;
  try {
    for (const k of ["http_port", "https_port", "mysql_port"]) {
      await api("/api/config", { key: k, value: String(f[k].value) });
    }
    toast("Saved. Stop all services, then start a project to apply.");
    refresh();
  } catch (e) {
    toast(e.message);
  }
};

(async function init() {
  await refresh();
  loadHealth();
  try {
    const j = await api("/api/jobs/current");
    if (j && j.status === "running") watchJob(j.id);
  } catch (_) { /* no job */ }
  setInterval(() => { if (!document.hidden) refresh(); }, 3000);
})();
