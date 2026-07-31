let API_BASE = window.location.origin;
let currentRunId = null, activityItems = [];

function $(id) { return document.getElementById(id); }

async function apiFetch(url, opts) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 120000);
  try { return await fetch(url, { ...opts, signal: ctrl.signal }); }
  finally { clearTimeout(timer); }
}

async function apiJson(url, opts) { const r = await apiFetch(url, opts); return r.json(); }

// ── Worker Registry ──
const WORKERS = {
  vayu:    { icon: "\u279E", color: "#22d3ee", name: "Vayu", role: "router" },
  neo:     { icon: "\uD83E\uDDE0", color: "#a78bfa", name: "Neo", role: "reasoner" },
  forge:   { icon: "\u2699", color: "#fb923c", name: "Forge", role: "coder" },
  scout:   { icon: "\uD83D\uDD0D", color: "#34d399", name: "Scout", role: "researcher" },
  verdict: { icon: "\u2696", color: "#fbbf24", name: "Verdict", role: "judge" },
  prism:   { icon: "\u2705", color: "#10b981", name: "Prism", role: "verifier" },
  oracle:  { icon: "\uD83D\uDDFA", color: "#60a5fa", name: "Oracle", role: "planner" },
  titan:   { icon: "\uD83D\uDD28", color: "#f87171", name: "Titan", role: "builder" },
  sentinel:{ icon: "\uD83D\uDEE1", color: "#818cf8", name: "Sentinel", role: "reviewer" },
};

const TOOL_ICONS = {
  search: "\uD83D\uDD0D", web: "\uD83C\uDF10", read: "\uD83D\uDCD6", write: "\u270D",
  code: "\uD83D\uDCBB", execute: "\u25B6", analyze: "\uD83D\uDCCA", trade: "\uD83D\uDCC8",
  chart: "\uD83D\uDCC8", news: "\uD83D\uDCF0", mail: "\u2709", sms: "\uD83D\uDCF1",
  file: "\uD83D\uDCC4", image: "\uD83D\uDDBC", voice: "\uD83C\uDFA4", terminal: "\uD83D\uDDA5",
  brain: "\uD83E\uDDE0", eye: "\uD83D\uDC41", globe: "\uD83C\uDF10", clock: "\u23F0",
};

function workerInfo(name) {
  const key = (name || "").toLowerCase();
  return WORKERS[key] || { icon: "\u25C8", color: "#818cf8", name: name || "Friday", role: "assistant" };
}

function toolIcon(action) {
  const a = (action || "").toLowerCase();
  for (const [k, v] of Object.entries(TOOL_ICONS)) { if (a.includes(k)) return v; }
  return "\u2699";
}

// ── Chat ──
const input = $("input"), sendBtn = $("send-btn"), micBtn = $("mic-btn"), cancelBtn = $("cancel-btn");
const msgEl = $("messages"), activityEl = $("activity-feed"), statusEl = $("sb-status");

let thinkingWorker = null, toolChain = [];

async function sendMessage(text) {
  if (!text.trim()) return;
  appendMsg("user", text); input.value = "";
  cancelBtn.style.display = ""; sendBtn.style.display = "none";
  toolChain = []; thinkingWorker = null;
  if (activityEl) activityEl.classList.add("show");

  let attempts = 0, max = 3;
  while (attempts < max) {
    attempts++;
    try {
      const r = await apiFetch(`${API_BASE}/command/stream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text, lang: "en", client_id: "friday" }),
      });
      const reader = r.body.getReader(), decoder = new TextDecoder();
      let buffer = "", fullReply = "", sawFinal = false, typingEl = null, currentWorker = null, currentTools = [];

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() || "";
        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          try {
            const data = JSON.parse(line.slice(6));
            if (data.type === "run_id") { currentRunId = data.run_id; }
            else if (data.type === "worker_status") {
              const s = data.status || "idle", a = data.activity || "", wName = data.worker || "Friday";
              if (s === "thinking") { thinkingWorker = wName; showThinking(); }
              else if (s === "working") { thinkingWorker = wName; showThinking(); }
              else { hideThinking(); }
            } else if (data.type === "thought") {
              const wName = data.name || "Friday";
              currentWorker = wName;
              thinkingWorker = wName;
              showThinking(workerInfo(wName).icon + " " + (data.thought || "thinking..."));
              addActivity("thinking", wName + " " + (data.thought || "Thinking..."));
            } else if (data.type === "action") {
              const wName = data.name || "Friday";
              currentWorker = wName;
              currentTools.push(data.action || "tool");
              toolChain.push({ worker: wName, tool: data.action || "tool" });
              addActivity("tool", wName + " \u2192 " + (data.action || "tool"));
              showToolChip(wName, data.action || "tool");
              updateThinking("using " + (data.action || "tool"));
            } else if (data.type === "result") {
              const wName = data.name || "";
              addActivity("result", (wName ? wName + " \u2192 " : "") + (data.action || "tool") + " done");
            } else if (data.type === "final") {
              sawFinal = true; fullReply = data.reply || "";
              if (typingEl) typingEl.remove();
              hideThinking();
              const wName = data.name || currentWorker || "Friday";
              const finalEl = createPremiumMsg("assistant", fullReply, wName, currentTools);
              msgEl.appendChild(finalEl); scrollChat();
              currentTools = [];
              addActivity("done", "Done");
            } else if (data.type === "audio" && data.audio) { try { new Audio("data:audio/mp3;base64," + data.audio).play(); } catch (e) {} }
            else if (data.type === "confirm") {
              showConfirm(data.run_id, data.action, data.args);
              hideThinking();
            }
            else if (data.type === "error") {
              addActivity("error", data.message || "Error"); hideThinking();
              if (typingEl) typingEl.textContent = data.message || "Error";
              else typingEl = appendMsg("assistant", data.message || "Error");
              sawFinal = true;
            }
            else if (data.type === "cancelled") { sawFinal = true; hideThinking(); if (typingEl) typingEl.textContent = "Cancelled."; else appendMsg("assistant", "Cancelled."); }
          } catch (e) {}
        }
      }
      if (!sawFinal && !typingEl) appendMsg("assistant", "Done.");
      else if (!sawFinal && typingEl) typingEl.textContent = "Done.";
      hideThinking();
      statusEl.textContent = "online";
      sendBtn.style.display = ""; cancelBtn.style.display = "none";
      currentRunId = null;
      return;
    } catch (e) {
      if (attempts >= max) { statusEl.textContent = "offline"; appendMsg("assistant", "Connection error \u2014 try again."); addActivity("error", "Connection failed"); hideThinking(); sendBtn.style.display = ""; cancelBtn.style.display = "none"; }
      else await new Promise(r => setTimeout(r, 1500 * attempts));
    }
  }
}

// ── Premium Message Renderer ──
function createPremiumMsg(role, text, workerName, tools) {
  const div = document.createElement("div");
  div.className = "msg premium " + role;

  if (role === "assistant") {
    const info = workerInfo(workerName);
    const avatarColor = info.color;

    // Header with avatar
    const hdr = document.createElement("div");
    hdr.className = "msg-hdr";
    hdr.innerHTML = '<span class="msg-avatar" style="background:' + avatarColor + ';box-shadow:0 0 8px ' + avatarColor + '40">' + info.icon + '</span><span class="msg-worker">' + info.name + '</span><span class="msg-role">' + info.role + '</span><span class="msg-time">just now</span>';
    div.appendChild(hdr);

    // Content
    const body = document.createElement("div");
    body.className = "msg-body";
    body.textContent = text;
    div.appendChild(body);

    // Tool chips
    if (tools && tools.length > 0) {
      const tc = document.createElement("div");
      tc.className = "msg-tools";
      tc.innerHTML = tools.map(t => '<span class="tool-chip"><span class="tc-icon">' + toolIcon(t) + '</span>' + t + '</span>').join(" ");
      div.appendChild(tc);
    }
  } else {
    div.textContent = text;
  }

  return div;
}

function appendMsg(role, text, cls) {
  const div = document.createElement("div");
  if (role === "assistant" && !cls) {
    const premium = createPremiumMsg(role, text, "Friday", []);
    msgEl.appendChild(premium); scrollChat(); return premium;
  }
  div.className = "msg " + role + (cls ? " " + cls : "");
  if (role === "user") {
    const body = document.createElement("div");
    body.className = "msg-body";
    body.textContent = text;
    div.appendChild(body);
  } else {
    div.textContent = text;
  }
  msgEl.appendChild(div); scrollChat(); return div;
}

// ── Premium Thinking Indicator ──
function showThinking(text) {
  const existing = $("thinking-bar");
  if (!existing) {
    const bar = document.createElement("div");
    bar.id = "thinking-bar";
    bar.className = "thinking-bar";
    bar.innerHTML = '<div class="th-avatars"></div><div class="th-text" id="th-text">thinking...</div>';
    msgEl.appendChild(bar); scrollChat();
  }
  if (text) $("th-text").textContent = text;
}
function hideThinking() { const el = $("thinking-bar"); if (el) el.remove(); }
function updateThinking(text) { const t = $("th-text"); if (t) t.textContent = text; }

// ── Tool Chip (inline during streaming) ──
function showToolChip(workerName, tool) {
  const existing = $("tool-chain");
  let tc = existing;
  if (!tc) {
    tc = document.createElement("div");
    tc.id = "tool-chain";
    tc.className = "tool-chain";
    msgEl.appendChild(tc);
  }
  const chip = document.createElement("span");
  chip.className = "tool-chip live";
  chip.innerHTML = '<span class="tc-icon">' + toolIcon(tool) + '</span>' + tool;
  tc.appendChild(chip);
  scrollChat();
  // Remove after 2s if no more tools
  clearTimeout(tc._hideTimer);
  tc._hideTimer = setTimeout(() => { if (tc && !tc.querySelector(".tool-chip.live:not(.out)")) tc.remove(); }, 3000);
}

function scrollChat() { requestAnimationFrame(() => { const c = $("chat-area"); if (c) c.scrollTop = c.scrollHeight; }); }

let typingToken = 0;
function typewrite(el, text) {
  const token = ++typingToken; el.textContent = "";
  const chars = Array.from(text); let i = 0;
  function step() {
    if (token !== typingToken || i >= chars.length) return;
    el.textContent += chars[i];
    const c = chars[i]; let d = 12;
    if (/[.!?]/.test(c)) d = 120; else if (/[,;:]/.test(c)) d = 60;
    i++; scrollChat(); setTimeout(step, d);
  }
  step();
}

function addActivity(type, msg) {
  if (!activityEl) return;
  const item = document.createElement("div");
  item.className = "activity-item " + type;
  const icons = { thinking: "\u25B6", tool: "\u2699", result: "\u2713", error: "\u2716", done: "\u2714" };
  const w = msg.match(/^(\w+)/);
  let prefix = "";
  if (w && w[1]) {
    const info = workerInfo(w[1]);
    if (info.icon !== "\u25C8") prefix = info.icon + " ";
  }
  item.textContent = (icons[type] || "") + " " + prefix + msg.slice(0, 100);
  activityEl.appendChild(item); activityEl.scrollTop = activityEl.scrollHeight;
  activityEl.classList.add("show"); activityItems.push(item);
  if (activityItems.length > 30) activityItems.shift().remove();
}

function showConfirm(runId, action, args) {
  const box = document.createElement("div"); box.className = "msg confirm";
  box.innerHTML = '<span>Allow: ' + action + '?</span><button class="cy" data-id="' + runId + '">Yes</button><button class="cn" data-id="' + runId + '">No</button>';
  box.querySelector(".cy").onclick = async () => { box.remove(); await apiFetch(API_BASE + "/run/" + runId + "/approve", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ approved: true }) }); addActivity("result", "Approved"); };
  box.querySelector(".cn").onclick = async () => { box.remove(); await apiFetch(API_BASE + "/run/" + runId + "/approve", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ approved: false }) }); addActivity("result", "Declined"); };
  msgEl.appendChild(box); scrollChat();
}

// ── Voice ──
micBtn.addEventListener("click", async () => {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (SR) {
    const rec = new SR(); rec.lang = "en-US"; rec.interimResults = false;
    micBtn.classList.add("recording");
    try {
      const text = await new Promise((resolve, reject) => { rec.onresult = e => resolve(e.results[0][0].transcript.trim()); rec.onerror = e => reject(e.error); rec.start(); setTimeout(() => { try { rec.stop(); } catch (e) {} }, 8000); });
      if (text) sendMessage(text); else appendMsg("assistant", "Couldn't hear clearly.");
    } catch (e) { if (e !== "no-speech") appendMsg("assistant", "Voice error: " + e); }
    micBtn.classList.remove("recording"); return;
  }
  if (micBtn.classList.contains("recording")) return;
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const mr = new MediaRecorder(stream); const chunks = [];
    mr.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
    mr.onstop = async () => {
      micBtn.classList.remove("recording"); stream.getTracks().forEach(t => t.stop());
      appendMsg("user", "Voice message");
      try {
        const buf = await new Blob(chunks, { type: "audio/webm" }).arrayBuffer();
        const bytes = new Uint8Array(buf); let binary = "";
        for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
        const r = await apiFetch(API_BASE + "/voice", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ audio: btoa(binary), client_id: "friday" }) });
        const data = await r.json();
        typewrite(appendMsg("assistant", ""), data.response || "Done.");
        if (data.audio) try { new Audio("data:audio/mp3;base64," + data.audio).play(); } catch (e) {}
      } catch (e) { appendMsg("assistant", "Voice error."); }
    };
    mr.start(); micBtn.classList.add("recording");
    setTimeout(() => { if (micBtn.classList.contains("recording")) mr.stop(); }, 8000);
  } catch (e) { appendMsg("assistant", "Microphone access denied."); }
});

// ── Chat Events ──
input.addEventListener("keydown", e => { if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); sendMessage(input.value); } });
sendBtn.addEventListener("click", () => sendMessage(input.value));
cancelBtn.addEventListener("click", async () => { if (currentRunId) try { await apiFetch(API_BASE + "/run/" + currentRunId + "/cancel", { method: "POST" }); } catch (e) {} });
document.querySelectorAll(".sb-cmd[data-cmd]").forEach(btn => { btn.addEventListener("click", () => { if (btn.dataset.cmd) sendMessage(btn.dataset.cmd); }); });

// ── Sidebar Status Polling ──
async function pollStatus() {
  try {
    const r = await apiFetch(API_BASE + "/status");
    const d = await r.json();
    const label = d.status === "online" ? "online" : d.status === "degraded" ? "degraded" : "offline";
    statusEl.textContent = label; statusEl.className = "sb-status" + (label !== "online" ? " " + label : "");
    $("st-workers").textContent = (d.workers_active ? d.workers_active + "/9" : (d.tools_count || "?") + "/9");
    const up = d.uptime_seconds || 0;
    $("st-uptime").textContent = Math.floor(up / 60) + "m " + (up % 60) + "s";
    const chatDot = document.querySelector("#feat-chat .fd");
    if (chatDot) chatDot.className = "fd" + (d.no_key ? "" : " on");
    const voiceDot = $("fd-voice");
    if (voiceDot) voiceDot.className = "fd" + ((d.providers?.length || 0) > 0 ? " on" : " error");
    const visionDot = $("fd-vision");
    if (visionDot) visionDot.className = "fd" + (d.eye_active ? " on" : "");
    const tradeDot = $("fd-trade");
    if (tradeDot) tradeDot.className = "fd" + (d.trading?.running ? " on" : "");
    $("st-bots").textContent = (d.bots?.length || 0) + (d.trading?.running ? 1 : 0);
  } catch (e) { statusEl.textContent = "offline"; statusEl.className = "sb-status offline"; }
}

// ── CONTROL CENTER ──
$("btn-cc")?.addEventListener("click", toggleCC);
$("cc-close")?.addEventListener("click", toggleCC);

function toggleCC() {
  const cc = $("cc");
  cc.classList.toggle("hidden");
  if (!cc.classList.contains("hidden")) { loadCCDash(); loadCCWorkers(); loadCCBots(); loadCCTrading(); loadCCMT5(); loadCCDevices(); loadCCLogs(); loadCCHealer(); loadCCCompanion(); }
}

document.querySelectorAll(".ct").forEach(tab => {
  tab.addEventListener("click", () => {
    document.querySelectorAll(".ct").forEach(t => t.classList.remove("active"));
    tab.classList.add("active");
    document.querySelectorAll(".cc-v").forEach(v => v.classList.add("hidden"));
    const target = $("cc-" + tab.dataset.v);
    if (target) { target.classList.remove("hidden"); loadTab(tab.dataset.v); }
  });
});

function loadTab(name) {
  switch (name) {
    case "dash": loadCCDash(); break;
    case "workers": loadCCWorkers(); break;
    case "bots": loadCCBots(); break;
    case "trading": loadCCTrading(); break;
    case "mt5": loadCCMT5(); break;
    case "devices": loadCCDevices(); break;
    case "logs": loadCCLogs(); break;
    case "settings": loadCCSettings(); break;
    case "healer": loadCCHealer(); break;
    case "companion": loadCCCompanion(); break;
  }
}

async function loadCCDash() {
  try {
    const [status, earn] = await Promise.all([apiJson(API_BASE + "/status"), apiJson(API_BASE + "/bots/earnings")]);
    const d = status, e = earn;
    const up = d.uptime_seconds || 0;
    $("cc-d-workers").textContent = (d.workers_active ? d.workers_active + "/9" : (d.tools_count || "?") + "/9");
    $("cc-d-bots").textContent = (d.bots?.length || 0) + (d.trading?.running ? 1 : 0);
    const tradePnl = d.trading?.total_pnl || 0, botEarn = e.total_earnings || 0, total = tradePnl + botEarn;
    $("cc-d-earn").textContent = (total >= 0 ? "+" : "") + "$" + total.toFixed(2);
    $("cc-d-earn").style.color = total >= 0 ? "var(--green)" : "var(--red)";
    $("cc-d-up").textContent = Math.floor(up / 60) + "m " + (up % 60) + "s";
  } catch (e) {}
}

async function loadCCWorkers() {
  const grid = $("cc-w-grid"); if (!grid) return;
  try {
    const r = await apiJson(API_BASE + "/workers/status");
    const workers = r.workers || {};
    let html = "";
    for (const [name, info] of Object.entries(workers)) {
      const wi = workerInfo(name);
      const s = info.status || "idle";
      html += '<div class="worker-card' + (s !== "idle" ? " active" : "") + '"><div class="w-icon" style="color:' + wi.color + '">' + wi.icon + '</div><div class="w-name">' + name + '</div><div class="w-role">' + (info.role || wi.role) + '</div><div class="w-stat ' + s + '">' + s + '</div></div>';
    }
    grid.innerHTML = html;
  } catch (e) { grid.innerHTML = "<div style='color:var(--text3);padding:10px'>Could not load</div>"; }
}

async function loadCCBots() {
  const list = $("cc-bot-list"); if (!list) return;
  try {
    const r = await apiJson(API_BASE + "/bots");
    const bots = r.bots || [];
    if (!bots.length) { list.innerHTML = "<div style='color:var(--text3);padding:8px'>No bots running</div>"; return; }
    list.innerHTML = bots.map(b => '<div class="cc-bot-item"><span class="cc-bot-dot ' + (b.status === "running" || b.status === "starting" ? "running" : b.status === "failed" ? "error" : "stopped") + '"></span><div class="cc-bot-info"><div class="cc-bot-name">' + (b.name || b.id) + '</div><div class="cc-bot-meta">' + (b.type || "") + " \u00B7 " + b.status + '</div></div><button class="cc-bot-stop" data-id="' + b.id + '">Stop</button></div>').join("");
    list.querySelectorAll(".cc-bot-stop").forEach(btn => { btn.addEventListener("click", async () => { await apiFetch(API_BASE + "/bots/stop", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bot_id: btn.dataset.id }) }); loadCCBots(); }); });
  } catch (e) { list.innerHTML = "<div style='color:var(--text3)'>Error loading</div>"; }
}

$("cc-bot-create")?.addEventListener("click", async () => {
  const type = $("cc-bot-type")?.value, name = $("cc-bot-name")?.value || type;
  if (!type) return;
  try {
    const r = await apiFetch(API_BASE + "/bots/create", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ bot_type: type, name, config: {} }) });
    const data = await r.json();
    if (data.id) { $("cc-bot-name").value = ""; loadCCBots(); addActivity("result", "Bot created: " + name); } else addActivity("error", data.error || "Failed");
  } catch (e) { addActivity("error", "Bot creation error"); }
});

async function loadCCTrading() {
  try {
    const r = await apiJson(API_BASE + "/status");
    const t = r.trading || {};
    $("cc-t-bal").textContent = "$" + (t.balance || 0).toFixed(2);
    $("cc-t-eq").textContent = "$" + (t.equity || 0).toFixed(2);
    const pnl = t.total_pnl || 0;
    $("cc-t-pnl").textContent = (pnl >= 0 ? "+" : "") + "$" + pnl.toFixed(2);
    $("cc-t-pnl").style.color = pnl >= 0 ? "var(--green)" : "var(--red)";
    $("cc-t-pos").textContent = t.positions?.length || 0;
  } catch (e) {}
}

$("cc-t-start")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/trading/start", { method: "POST" }); addActivity("result", "Trading started"); loadCCTrading(); } catch (e) {}
});
$("cc-t-stop")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/trading/stop", { method: "POST" }); addActivity("result", "Trading stopped"); loadCCTrading(); } catch (e) {}
});

async function loadCCMT5() {
  try {
    const r = await apiJson(API_BASE + "/mt5/status");
    const stat = $("cc-mt5-stat");
    const conn = r.connected || false;
    stat.innerHTML = '<span class="fd ' + (conn ? "on" : "error") + '"></span>' + (conn ? "Connected" : "Disconnected");
    $("cc-mt5-lat").textContent = r.latency ? r.latency + "ms" : "\u2014";
    $("cc-mt5-acct").textContent = r.account || "\u2014";
  } catch (e) {}
}

$("cc-mt5-conn")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/mt5/connect", { method: "POST" }); addActivity("result", "MT5 connecting..."); loadCCMT5(); } catch (e) {}
});
$("cc-mt5-disc")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/mt5/disconnect", { method: "POST" }); addActivity("result", "MT5 disconnected"); loadCCMT5(); } catch (e) {}
});

async function loadCCDevices() {
  try {
    const r = await apiJson(API_BASE + "/devices/status");
    const devs = r.devices || {};
    updateDeviceUI("cc-android", devs.android || {});
    updateDeviceUI("cc-ios", devs.ios || {});
  } catch (e) {}
}

function updateDeviceUI(prefix, dev) {
  const stat = $(prefix + "-stat");
  if (stat) { stat.innerHTML = '<span class="fd ' + (dev.connected ? "on" : "") + '"></span>' + (dev.connected ? "Online" : "Offline"); }
  const ver = $(prefix + "-ver"); if (ver) ver.textContent = dev.version || "\u2014";
  const bat = $(prefix + "-bat"); if (bat) bat.textContent = dev.battery ? dev.battery + "%" : "\u2014";
  const last = $(prefix + "-last"); if (last) last.textContent = dev.last_seen ? new Date(Date.now() - dev.last_seen).toISOString().slice(11, 19) + " ago" : "\u2014";
}

$("cc-android-conn")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/devices/android/connect", { method: "POST" }); addActivity("result", "Android connecting..."); loadCCDevices(); } catch (e) {}
});
$("cc-android-test")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/devices/android/test-voice", { method: "POST" }); addActivity("result", "Test voice sent to Android"); } catch (e) {}
});
$("cc-ios-conn")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/devices/ios/connect", { method: "POST" }); addActivity("result", "iOS connecting..."); loadCCDevices(); } catch (e) {}
});
$("cc-ios-test")?.addEventListener("click", async () => {
  try { await apiFetch(API_BASE + "/devices/ios/test-voice", { method: "POST" }); addActivity("result", "Test voice sent to iOS"); } catch (e) {}
});

async function loadCCLogs() {
  const list = $("cc-log-list"); if (!list) return;
  try {
    const r = await apiJson(API_BASE + "/logs?limit=50");
    const logs = r.logs || [];
    list.innerHTML = logs.length ? logs.map(l => '<div><span class="t">' + (l.time || "") + '</span><span class="m">' + (l.message || "") + '</span></div>').join("") : "<div style='color:var(--text3)'>No logs</div>";
  } catch (e) { list.innerHTML = "<div style='color:var(--text3)'>Error loading</div>"; }
}

function loadCCSettings() {}

async function loadCCHealer() {
  const statusEl = $("cc-heal-status");
  const logEl = $("cc-heal-log");
  if (!statusEl || !logEl) return;

  try {
    const [status, log] = await Promise.all([
      apiJson(API_BASE + "/healer/status"),
      apiJson(API_BASE + "/healer/log"),
    ]);
    const health = status.health || {};
    statusEl.textContent = status.status || "unknown";
    statusEl.style.color = status.status === "healthy" ? "var(--green)" : "var(--red)";

    const repairs = log.repairs || [];
    if (repairs.length) {
      logEl.innerHTML = repairs.map(r => '<div><span class="t">' + (r.time || "") + '</span><span class="m">[' + r.action + '] ' + r.issue + (r.success ? " ✓" : " ✗") + '</span></div>').join("");
    } else {
      logEl.innerHTML = "<div style='color:var(--text3)'>No repairs recorded</div>";
    }
  } catch (e) {
    statusEl.textContent = "offline";
    statusEl.style.color = "var(--red)";
  }
}

function healerAction(action) {
  return async () => {
    try {
      const r = await apiFetch(API_BASE + "/healer/repair", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ issue: "manual", action, detail: "User requested" })
      });
      const data = await r.json();
      if (data.success) addActivity("result", "Healer: " + action);
      else addActivity("error", "Healer: " + action + " failed");
      loadCCHealer();
    } catch (e) { addActivity("error", "Healer action failed"); }
  };
}

$("cc-heal-refresh")?.addEventListener("click", loadCCHealer);
$("cc-heal-backup")?.addEventListener("click", healerAction("backup"));
$("cc-heal-rebuild")?.addEventListener("click", healerAction("rebuild"));
$("cc-heal-clean")?.addEventListener("click", healerAction("goclean"));

// ── Companion ──
async function loadCCCompanion() {
  try {
    const d = await apiJson(API_BASE + "/companion");
    $("cc-comp-since").textContent = "Together since " + (d.first_seen ? new Date(d.first_seen).toLocaleDateString() : "today");
    $("cc-comp-msgs").textContent = d.total_messages || 0;
    $("cc-comp-crashes").textContent = d.crash_count || 0;
    $("cc-comp-last").textContent = d.last_seen ? new Date(d.last_seen).toLocaleString() : "now";
    const prefs = d.preferences || {};
    const keys = Object.keys(prefs);
    const prefsEl = $("cc-comp-prefs");
    if (keys.length) {
      prefsEl.innerHTML = keys.map(k => '<div class="cs-row"><span>' + k + '</span><span>' + prefs[k] + '</span></div>').join("");
    } else {
      prefsEl.innerHTML = '<div style="color:var(--text3);font-size:10px">None yet</div>';
    }
  } catch (e) {}
}

$("cc-set-reset")?.addEventListener("click", async () => {
  if (!confirm("Reset all data?")) return;
  try { await apiFetch(API_BASE + "/admin/reset", { method: "POST" }); addActivity("result", "Data reset"); } catch (e) {}
});
$("cc-set-factory")?.addEventListener("click", async () => {
  if (!confirm("Factory reset? This removes everything.")) return;
  try { await apiFetch(API_BASE + "/admin/factory-reset", { method: "POST" }); addActivity("result", "Factory reset"); } catch (e) {}
});

// ── Kill Switch ──
$("cc-d-kill")?.addEventListener("click", async () => {
  if (!confirm("EMERGENCY KILL SWITCH \u2014 stop ALL activity? Type 'KILL' to confirm.")) return;
  const code = prompt('Type "KILL" to confirm:');
  if (code !== "KILL") return;
  try {
    await apiFetch(API_BASE + "/emergency/kill", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ confirmation: "KILL" }) });
    addActivity("result", "EMERGENCY KILL EXECUTED");
  } catch (e) { addActivity("error", "Kill switch failed"); }
});

$("btn-new")?.addEventListener("click", () => {
  msgEl.innerHTML = '<div class="welcome"><img src="/static/assets/friday-mark.svg" width="28" height="28" alt=""><div class="w-title">Friday</div><div class="w-sub">online \u2014 9 workers ready</div><div class="w-hint">type a message or use a quick command</div></div>';
  activityEl.innerHTML = ""; activityItems = []; activityEl.classList.remove("show");
});

pollStatus();
setInterval(pollStatus, 15000);
$("cc-d-refresh")?.addEventListener("click", () => { loadCCDash(); addActivity("result", "Refreshed"); });