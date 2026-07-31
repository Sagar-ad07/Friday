// Friday Control Center — app logic
// Connects to /ws/activity for the live stream, polls the engine (8001)
// for account cards, and hosts a chatbox that talks to /chat.

const API = (p) => `/api${p}`;
const ENG = "http://localhost:8001";
let streamEl, accountsEl, systemEl, botEl, clockEl, healthEl, devicesEl, teamEl;
let ws;
let lastEventId = "";
let chatOpen = true;
let chatHistory = JSON.parse(localStorage.getItem("friday_chat") || "[]");

const KIND_ICON = {
  think: "💭", tool: "🔧", worker: "⚙️", chat: "💬",
  trade: "📈", system: "🛡️", monitor: "📡",
};

function $(id) { return document.getElementById(id); }

function init() {
  streamEl = $("stream");
  accountsEl = $("accounts");
  systemEl = $("system");
  botEl = $("bot");
  clockEl = $("clock");
  healthEl = $("health");
  devicesEl = $("devices");
  teamEl = $("team");

  tickClock();
  setInterval(tickClock, 1000);

  setupChat();
  setupWS();
  setInterval(pollStatus, 4000);
  setInterval(loadDevices, 6000);
  setInterval(loadUsers, 8000);
  pollStatus();
  loadDevices();
  loadUsers();
}

function tickClock() {
  const d = new Date();
  clockEl.textContent = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

// ── WebSocket activity feed ──────────────────────────────────────────
function setupWS() {
  ws = new WebSocket(`ws://${location.hostname}:8000/ws/activity`);
  ws.onopen = () => console.log("[ws] connected");
  ws.onmessage = (evt) => {
    try {
      const event = JSON.parse(evt.data);
      addEvent(event);
      renderLatestThink();
    } catch (e) {
      console.warn("[ws] bad frame", e);
    }
  };
  ws.onclose = () => {
    clockEl.classList.add("err");
    setTimeout(setupWS, 3000);
  };
}

function addEvent(ev) {
  const div = document.createElement("div");
  div.className = "msg msg-" + ev.kind;
  div.dataset.id = ev.id;
  const icon = KIND_ICON[ev.kind] || "•";
  const when = new Date(ev.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" });
  const detail = (ev.detail || "").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  div.innerHTML = `<span class="t">${when}</span> <span class="icon">${icon}</span> <span class="lbl">${escapeHtml(ev.label || ev.kind)}</span><span class="dot"></span>`;
  if (ev.detail) {
    const d = document.createElement("div");
    d.className = "detail";
    d.textContent = ev.detail;
    div.appendChild(d);
  }
  streamEl.appendChild(div);
  lastEventId = ev.id;
  // keep last 300
  while (streamEl.children.length > 300) streamEl.removeChild(streamEl.firstChild);
  streamEl.scrollTop = streamEl.scrollHeight;
}

function renderLatestThink() {
  const msgs = streamEl.querySelectorAll(".msg");
  if (msgs.length === 0) return;
  const latest = msgs[msgs.length - 1];
  latest.classList.add("new");
  // typewriter + blinking dots on the latest message detail/label
  setTimeout(() => latest.classList.remove("new"), 4000);
}

// ── Polling: accounts + engine status ─────────────────────────────────
async function pollStatus() {
  // Engine health via 8000 proxy
  try {
    const r = await fetch("/status", { cache: "no-store" });
    const j = await r.json();
    renderSystem(j);
  } catch (e) {
    healthEl.textContent = "⚠ engine unreachable";
  }

  // BG + Exness from engine directly (CORS)
  try {
    const [bg, ex] = await Promise.all([
      fetch(`${ENG}/trading/status`).then(x => x.json()),
      fetch(`${ENG}/trading/exness/status`).then(x => x.json()),
    ]);
    renderAccounts(bg, ex);
  } catch (e) {
    accountsEl.innerHTML = `<div class="dim">engine offline</div>`;
  }
}

function renderAccounts(bg, ex) {
  const bgColor = bg?.running ? "#00ff88" : "#ff4757";
  const bgColor2 = ex?.running ? "#00ff88" : "#ff4757";
  accountsEl.innerHTML = `
    <div class="card card-bg">
      <div class="card-head"><span class="dot-led" style="background:${bgColor}"></span> BlueGuardian (prop-firm)</div>
      <div class="kv"><span>Status</span><span style="color:${bg?.running?'#00ff88':'#ff4757'}">${bg?.running?'LIVE':'CAPPED'}</span></div>
      <div class="kv"><span>Daily PnL</span><span>$${Number(bg?.daily_pnl||0).toFixed(2)}</span></div>
      <div class="kv"><span>Profit Cap</span><span>$${Number(bg?.daily_profit_cap||0).toFixed(2)}</span></div>
      <div class="kv"><span>Last error</span><span class="small dim">${escapeHtml(bg?.last_error||'—')}</span></div>
    </div>
    <div class="card card-ex">
      <div class="card-head"><span class="dot-led" style="background:${bgColor2}"></span> Exness (personal)</div>
      <div class="kv"><span>Status</span><span style="color:${ex?.running?'#00ff88':'#ff4757'}">${ex?.running?'LIVE':'OFF'}</span></div>
      <div class="kv"><span>Lot / SL / TP</span><span>${ex?.lot||0} / ${ex?.sl_pips||0}‴ / ${ex?.tp_pips||0}‴</span></div>
      <div class="kv"><span>Risk / Reward</span><span>$${ex?.risk_usd||0} / $${ex?.reward_usd||0}</span></div>
      <div class="kv"><span>Wins / Losses</span><span>${ex?.wins||0} / ${ex?.losses||0}</span></div>
      <div class="kv"><span>In trade</span><span>${ex?.in_trade?'✅ yes':'—'}</span></div>
    </div>
  `;
}

function renderSystem(j) {
  const providers = j.providers || [];
  systemEl.innerHTML = `
    <div class="kv"><span>Mode</span><span>${j.mode||'online'}</span></div>
    <div class="kv"><span>Providers</span><span class="small">${providers.join(' → ')}</span></div>
    <div class="kv"><span>Capabilities</span><span>${j.capabilities}</span></div>
    <div class="kv"><span>Uptime</span><span class="small">${j.uptime||'—'}</span></div>
  `;
  healthEl.textContent = (window.navigator.onLine ? "🟢 " : "🔴 ") + (providers.length ? "online" : "limited");
}

function renderBot(j) {
  botEl.innerHTML = `
    <div class="kv"><span>Bot</span><span>${j.name||'TradingBot'}</span></div>
    <div class="kv"><span>Trades</span><span>${j.trading?.trades_today||0}</span></div>
  `;
}

// ── Devices (Android APK install + connect) ──────────────────────────────
async function loadDevices() {
  try {
    const r = await fetch("/devices/status");
    const j = await r.json();
    renderDevices(j.devices);
  } catch (e) {
    devicesEl.innerHTML = `<div class="dim">offline</div>`;
  }
}

function renderDevices(devs) {
  const android = devs?.android || {};
  const connected = android.connected;
  const cap = android.capabilities || {};
  const caps = Object.keys(cap).filter(k => cap[k]).join(", ") || "—";
  const apk = "/static/Friday-Android-release.apk";
  devicesEl.innerHTML = `
    <div class="device-row"><span class="dot-led" style="background:${connected?'#00ff88':'#ff4757'}"></span> Android</div>
    ${connected
      ? `<div class="kv"><span>Status</span><span style="color:#00ff88">CONNECTED</span></div>
         <div class="kv"><span>Battery</span><span>${android.battery ?? '—'}</span></div>
         <div class="kv"><span>Version</span><span>${android.version ?? '—'}</span></div>`
      : `<div class="kv"><span>Status</span><span style="color:#ff4757">DISCONNECTED</span></div>`
    }
    <div class="kv"><span>Caps</span><span class="small dim">${caps}</span></div>
    <div class="kv"><span>APK</span><span><a class="apk-link" href="${apk}" download>⬇ Download Friday apk</a></span></div>
    <button id="connect-btn" class="connect-btn" onclick="connectAndroid()">${connected ? '↻ Reconnect phone' : '• Connect phone'}</button>
  `;
}

async function connectAndroid() {
  const btn = $("connect-btn"); // not used; fallback below
  try {
    const r = await fetch("/devices/android/connect", { method: "POST" });
    const j = await r.json();
    const badge = devicesEl.querySelector(".dot-led");
    if (badge) badge.style.background = "#00ff88";
    const st = devicesEl.querySelector("span[style*='color']");
    if (st) st.textContent = "connecting…";
    setTimeout(loadDevices, 2500);
  } catch (e) {
    alert("connect failed: " + e);
  }
}

// ── Team (signup + user list) ────────────────────────────────────────────
async function loadUsers() {
  try {
    const r = await fetch("/api/users");
    const j = await r.json();
    renderUsers(j.users || []);
  } catch (e) {
    teamEl.innerHTML = `<div class="dim">offline</div>`;
  }
}

function renderUsers(users) {
  const rows = (users || []).map(u => `
    <div class="user-row">
      <span class="nm">${u.name}</span>
      <span class="em">${u.email} · <span style="color:#8282ff">${u.plan||'trial'}</span></span>
    </div>`).join("");
  teamEl.innerHTML = `
    <div class="kv"><span>Users</span><span>${users.length}</span></div>
    ${rows}
    <div class="team-link" onclick="window.location.href='/signup'">→ Sign up a user</div>
  `;
}

// ── Chatbox ──────────────────────────────────────────────────────────
function setupChat() {
  const box = $("chat-float");
  const head = $("chat-header");
  const body = $("chat-body");
  const input = $("chat-input");
  const send = $("chat-send");

  renderChat();

  head.onclick = (e) => {
    if ((e.target).classList.contains("chat-title") || e.target === head || e.target.classList.contains("chat-toggle")) {
      chatOpen = !chatOpen;
      box.classList.toggle("collapsed", !chatOpen);
      $("chat-toggle").textContent = chatOpen ? "▼" : "►";
    }
  };

  function doSend() {
    const text = input.value.trim();
    if (!text) return;
    const msg = { role: "user", content: text };
    chatHistory.push(msg);
    saveChat();
    renderChat();
    body.scrollTop = body.scrollHeight;
    input.value = "";
    send.disabled = true;
    appendBotTyping();
    fetch("/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ model: "friday", stream: false, messages: chatHistory })
    })
      .then(r => r.json())
      .then(d => {
        removeTyping();
        const reply = (d.choices?.[0]?.message?.content) || d.reply || "(no reply)";
        chatHistory.push({ role: "assistant", content: reply });
        saveChat();
        renderChat();
        body.scrollTop = body.scrollHeight;
        send.disabled = false;
      })
      .catch(e => {
        removeTyping();
        chatHistory.push({ role: "assistant", content: "⚠ connection error" });
        saveChat();
        renderChat();
        send.disabled = false;
      });
  }

  send.onclick = doSend;
  input.onkeydown = (e) => { if (e.key === "Enter") doSend(); };
}

function appendBotTyping() {
  const body = $("chat-body");
  const t = document.createElement("div");
  t.className = "typing";
  t.innerHTML = `<span class="bot">Friday</span><span class="blink">▊</span> thinking...`;
  body.appendChild(t);
  body.scrollTop = body.scrollHeight;
}

function removeTyping() {
  const body = $("chat-body");
  const t = body.querySelector(".typing");
  if (t) body.removeChild(t);
}

function saveChat() {
  if (chatHistory.length > 100) chatHistory = chatHistory.slice(-100);
  localStorage.setItem("friday_chat", JSON.stringify(chatHistory));
}

function renderChat() {
  const body = $("chat-body");
  body.innerHTML = "";
  for (const m of chatHistory) {
    const d = document.createElement("div");
    d.className = "cm " + (m.role === "user" ? "user" : "bot");
    d.textContent = m.content;
    body.appendChild(d);
  }
  body.scrollTop = body.scrollHeight;
}

function escapeHtml(s) {
  return (s || "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

// load recent activity on open so the stream isn't empty
async function loadRecentActivity() {
  try {
    const r = await fetch("/api/activity?limit=40", { cache: "no-store" });
    const j = await r.json();
    (j.events || []).forEach(addEvent);
    renderLatestThink();
    streamEl.scrollTop = streamEl.scrollHeight;
  } catch (e) {
    /* offline — ws will fill it */
  }
}

document.addEventListener("DOMContentLoaded", () => {
  init();
  loadRecentActivity();
});