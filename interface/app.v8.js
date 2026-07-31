/* Friday — Floating Widget JS
   Features:
   * Floating toggle button + draggable widget
   * Activity feed (thinking, tool calls, worker activity)
   * Smooth animated auto-scroll
   * Typewriter text reveal
   * SSE streaming with reconnection
   * Voice input (browser SpeechRecognition + server fallback)
   * LIVE WORKER STATUS PANEL (Control Center)
*/
const FALLBACK_ORIGINS = [];
let API_BASE = window.location.origin;
let _sessionStart = Date.now();
let _reconnectAttempts = 0;
const MAX_RECONNECT = 3;

// ── Worker Status State ──────────────────────────────────
const WORKERS = {
  router: { el: null, status: "idle", activity: "Routing requests" },
  reasoner: { el: null, status: "idle", activity: "Analyzing..." },
  coder: { el: null, status: "idle", activity: "Standing by" },
  researcher: { el: null, status: "idle", activity: "Scanning..." },
  judge: { el: null, status: "idle", activity: "Evaluating..." },
  verifier: { el: null, status: "idle", activity: "Quality check" },
  planner: { el: null, status: "idle", activity: "Mapping strategy" },
  builder: { el: null, status: "idle", activity: "Ready" },
  reviewer: { el: null, status: "idle", activity: "On guard" }
};

// Worker display names
const WORKER_NAMES = {
  router: "Router", reasoner: "Reasoner", coder: "Coder",
  researcher: "Researcher", judge: "Judge", verifier: "Verifier",
  planner: "Planner", builder: "Builder", reviewer: "Reviewer"
};

// Worker role mapping (internal -> display)
const WORKER_ROLES = {
  router: "Vayu - Router",
  reasoner: "Neo - Reasoner", 
  coder: "Forge - Coder",
  researcher: "Scout - Researcher",
  judge: "Verdict - Judge",
  verifier: "Prism - Verifier",
  planner: "Oracle - Planner",
  builder: "Titan - Builder",
  reviewer: "Sentinel - Reviewer"
};

// ── Initialize Worker Panel ─────────────────────────────
function initWorkerPanel() {
  for (const [key, w] of Object.entries(WORKERS)) {
    // Try to find status element by ID first
    w.el = document.getElementById(`status-${key}`);
    w.actEl = document.getElementById(`activity-${key}`);
    
    // Fallback: find via card element
    if (!w.el) {
      const card = document.getElementById(`worker-${key}`);
      if (card) {
        w.el = card.querySelector(".worker-status");
        w.actEl = card.querySelector(".worker-activity");
      }
    }
  }
}

// ── Worker Status Updates ─────────────────────────────────
function updateWorkerStatus(workerKey, status, activity) {
  const w = WORKERS[workerKey];
  if (!w || !w.el) return;
  
  // Update status class
  w.el.className = "worker-status " + status;
  w.el.textContent = status.toUpperCase();
  
  // Update activity if provided
  if (w.actEl && activity) {
    w.actEl.textContent = activity;
  }
}

function updateAllWorkersStatus(status, activity) {
  for (const [key, w] of Object.entries(WORKERS)) {
    if (w.el) {
      w.el.className = "worker-status " + status;
      w.el.textContent = status.toUpperCase();
      if (w.actEl && activity) {
        w.actEl.textContent = activity;
      }
    }
  }
}

function setWorkerBusy(workerKey) {
  updateWorkerStatus(workerKey, "thinking", "Processing...");
}

function setWorkerWorking(workerKey, task) {
  updateWorkerStatus(workerKey, "working", task || "Working...");
}

function setWorkerSpeaking(workerKey) {
  updateWorkerStatus(workerKey, "speaking", "Speaking...");
}

function setWorkerTyping(workerKey) {
  updateWorkerStatus(workerKey, "typing", "Typing...");
}

function setWorkerIdle(workerKey) {
  updateWorkerStatus(workerKey, "idle", "Standing by");
}

// ── Helper: qs (defined early for use in updateTradingPanel) ──
function qs(id) { return document.getElementById(id); }

async function apiFetch(url, opts) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 120000);
  try {
    const r = await window.fetch(url, { ...opts, signal: controller.signal });
    _reconnectAttempts = 0;
    return r;
  } catch (e) {
    if (API_BASE === window.location.origin && FALLBACK_ORIGINS.length && _reconnectAttempts < MAX_RECONNECT) {
      _reconnectAttempts++;
      console.warn("[Friday] fallback to", FALLBACK_ORIGINS[0]);
      try {
        return await window.fetch(url.replace(window.location.origin, FALLBACK_ORIGINS[0]), { ...opts, signal: controller.signal });
      } catch (e2) {}
    }
    throw e;
  } finally {
    clearTimeout(timer);
  }
}

// ── DOM refs ──────────────────────────────────────────
const trigger = document.getElementById("friday-trigger");
const widget = document.getElementById("friday-widget");
const header = document.getElementById("widget-header");
const messagesEl = document.getElementById("messages");
const activityScroll = document.getElementById("activity-scroll");
const inputEl = document.getElementById("user-input");
const sendBtn = document.getElementById("send-btn");
const micBtn = document.getElementById("mic-btn");
const cancelBtn = document.getElementById("cancel-btn");
const minimizeBtn = document.getElementById("minimize-btn");
const closeBtn = document.getElementById("close-btn");
const statusEl = document.getElementById("header-status");

// Control center toggle
const controlCenterBtn = document.getElementById("control-center-btn");
const workerPanel = document.getElementById("worker-panel");

// ── Initialize Worker Panel ─────────────────────────────
initWorkerPanel();

// Show/hide control center
function toggleControlCenter() {
  if (workerPanel) {
    workerPanel.classList.toggle("hidden");
  }
}

// Quick toggle from header or trigger
document.addEventListener("keydown", (e) => {
  if (e.key === "c" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    toggleControlCenter();
  }
});

if (controlCenterBtn) {
  controlCenterBtn.addEventListener("click", toggleControlCenter);
}

// ── Module-level state ───────────────────────────────
let currentRunId = null;

// ── Toggle widget ────────────────────────────────────
trigger.addEventListener("click", () => {
  widget.classList.toggle("hidden");
  if (!widget.classList.contains("hidden")) {
    inputEl.focus();
    scrollToBottom("smooth");
  }
});
closeBtn.addEventListener("click", () => widget.classList.add("hidden"));
let minimized = false;
minimizeBtn.addEventListener("click", () => {
  minimized = !minimized;
  const chat = document.querySelector(".chat-area");
  const feed = document.querySelector(".activity-feed");
  if (minimized) {
    widget.style.height = "62px";
    chat.style.display = "none";
    feed.style.display = "none";
    minimizeBtn.textContent = "□";
  } else {
    widget.style.height = "";
    chat.style.display = "";
    feed.style.display = "";
    minimizeBtn.textContent = "─";
  }
});

// ── Draggable widget ─────────────────────────────────
let isDragging = false, dragStart = {}, widgetStart = {};
header.addEventListener("mousedown", (e) => {
  if (e.target.closest(".header-right")) return;
  isDragging = true;
  dragStart = { x: e.clientX, y: e.clientY };
  const rect = widget.getBoundingClientRect();
  widgetStart = { x: rect.left, y: rect.top };
  widget.style.cursor = "grabbing";
  widget.style.transition = "none";
});
document.addEventListener("mousemove", (e) => {
  if (!isDragging) return;
  const dx = e.clientX - dragStart.x;
  const dy = e.clientY - dragStart.y;
  widget.style.left = widgetStart.x + dx + "px";
  widget.style.top = widgetStart.y + dy + "px";
  widget.style.right = "auto";
  widget.style.bottom = "auto";
});
document.addEventListener("mouseup", () => {
  if (!isDragging) return;
  isDragging = false;
  widget.style.cursor = "";
  widget.style.transition = "";
});

// ── Scroll (smooth auto-scroll) ──────────────────────
function scrollToBottom(behavior = "smooth") {
  requestAnimationFrame(() => {
    messagesEl.scrollTo({
      top: messagesEl.scrollHeight,
      behavior: behavior,
    });
  });
}

// ── Tool icon map ─────────────────────────────────
const TOOL_ICONS = {
  web_search: "🔍", open_url: "🌐",
  run_terminal: "💻", run_code: "⌨", manage_files: "📁",
  get_time: "🕐", system_info: "🖥", open_app: "📂",
  desktop_control: "🖱", calc: "🔢",
  remember: "🧠", recall: "💭",
  trading_backtest: "📊", trading_analyze: "📈",
  trading_status: "📡", trading_start: "▶", trading_stop: "⏹",
  trading_suggest_strategy: "🎯", ui_update: "🎨",
  phone_send_sms: "📱", phone_open_app: "📲",
  phone_open_url: "🔗", phone_tap: "👆",
  phone_type: "⌨️", phone_screenshot: "📸",
};

function toolIcon(name) {
  return TOOL_ICONS[name] || "⚡";
}

// ── Activity Feed ────────────────────────────────────
const TYPE_ICONS = {
  thinking: "⟐", tool: "⚡", worker: "◈",
  result: "✓", error: "✕", done: "—",
};

function addActivity(type, label, tag = "", icon) {
  const item = document.createElement("div");
  item.className = "activity-item " + type;
  const iconSpan = document.createElement("span");
  iconSpan.className = "activity-icon";
  iconSpan.textContent = icon || TYPE_ICONS[type] || "";
  item.appendChild(iconSpan);
  const labelSpan = document.createElement("span");
  labelSpan.className = "activity-label";
  labelSpan.textContent = label;
  item.appendChild(labelSpan);
  if (tag) {
    const tagSpan = document.createElement("span");
    tagSpan.className = "activity-tag";
    tagSpan.textContent = tag;
    item.appendChild(tagSpan);
  }
  activityScroll.appendChild(item);
  activityScroll.scrollTop = activityScroll.scrollHeight;

  // Auto-remove old items (keep last 20)
  while (activityScroll.children.length > 20) {
    activityScroll.removeChild(activityScroll.firstChild);
  }
  return item;
}

function clearActivity() {
  activityScroll.innerHTML = "";
}

// ── Live Worker Typing Indicators ─────────────────────────
function showWorkerTyping(workerKey, action) {
  const activity = addActivity("thinking", `${WORKER_ROLES[workerKey] || workerKey} is typing...`, action || "thinking");
  activity.classList.add("typing");
  activity.style.animation = "fadeIn 0.1s ease-in-out infinite";
  return activity;
}

function updateWorkerTyping(activityEl, workerKey, action) {
  if (!activityEl) return;
  activityEl.textContent = `${WORKER_ROLES[workerKey] || workerKey}: ${action}`;
}

function removeWorkerTyping(activityEl) {
  if (activityEl) {
    activityEl.className = "activity-item result";
    activityEl.textContent = activityEl.textContent.replace("is typing...", "complete");
  }
}

// ── Messages ─────────────────────────────────────────
function appendMessage(role, text, cls) {
  const div = document.createElement("div");
  div.className = `msg ${role}` + (cls ? " " + cls : "");
  div.textContent = text;
  messagesEl.appendChild(div);
  scrollToBottom();
  return div;
}

// ── Typewriter effect ────────────────────────────────
let _typingToken = 0;
function typewrite(el, text) {
  const myToken = ++_typingToken;
  el.textContent = "";
  const chars = Array.from(text);
  let i = 0;
  return new Promise((resolve) => {
    function step() {
      if (myToken !== _typingToken) return resolve();
      if (i >= chars.length) return resolve();
      el.textContent += chars[i];
      const c = chars[i];
      let delay = 20;
      if (c === "." || c === "!" || c === "?") delay = 200;
      else if (c === "," || c === ";" || c === ":") delay = 100;
      else if (c === "\n") delay = 60;
      else if (c === " " && chars[i - 1] && /[.!?]/.test(chars[i - 1])) delay = 140;
      i++;
      scrollToBottom();
      setTimeout(step, delay);
    }
    step();
  });
}

// ── Streaming send ───────────────────────────────────
async function sendMessage(text) {
  if (!text.trim()) return;
  appendMessage("user", text);
  inputEl.value = "";
  inputEl.style.height = "auto";

  const assistantMsg = appendMessage("assistant", "Thinking…", "typing");
  statusEl.textContent = "Thinking";
  statusEl.style.color = "var(--yellow)";

  addActivity("thinking", "Processing your request…");

  let attempts = 0;
  const maxAttempts = 3;
  sendBtn.style.display = "none";
  cancelBtn.style.display = "";

  while (attempts < maxAttempts) {
    attempts++;
    try {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), 120000);
      const r = await apiFetch(`${API_BASE}/command/stream`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text, lang: "en", client_id: "friday" }),
        signal: controller.signal,
      });
      clearTimeout(timer);
      const reader = r.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "", fullReply = "", sawFinal = false, lastError = "";

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

            if (data.type === "run_id") {
              currentRunId = data.run_id;
              addActivity("thinking", "Run started", data.run_id.slice(0, 6));
            }

            else if (data.type === "start") {
              addActivity("thinking", "Starting…");
            }

            else if (data.type === "thought") {
              const thought = (data.thought || "").slice(0, 80);
              const who = data.name || data.provider || "brain";
              if (thought) {
                addActivity("thinking", thought, who);
              } else {
                addActivity("thinking", "Reasoning…", who);
              }
              assistantMsg.className = "msg assistant typing";
              assistantMsg.textContent = "Thinking…";
              scrollToBottom();
            }

            else if (data.type === "action") {
              const actionName = data.action || "?";
              const args = data.args || {};
              const who = data.name || "";
              const argStr = Object.keys(args).slice(0, 2).map(k => `${k}=${String(args[k]).slice(0, 30)}`).join(", ");
              addActivity("tool", actionName, (who ? who + " " : "") + (argStr.slice(0, 40) || "…"), toolIcon(actionName));
              assistantMsg.className = "msg assistant typing";
              assistantMsg.textContent = `⚡ Using ${actionName}…`;
              scrollToBottom();
            }

            else if (data.type === "result") {
              const res = (data.result || "").slice(0, 60);
              const who = data.name || "";
              addActivity("result", `${data.action || "tool"} → ${res}`, who || "done");
            }

            else if (data.type === "final") {
              sawFinal = true;
              fullReply = data.reply || "";
              assistantMsg.className = "msg assistant";
              assistantMsg.textContent = "";
              typewrite(assistantMsg, fullReply);
              statusEl.textContent = "Ready";
              statusEl.style.color = "var(--green)";
              const who = data.name || "";
              addActivity("done", who ? `${who} complete` : "Response complete");
            }

            else if (data.type === "audio" && data.audio) {
              playAudioB64(data.audio);
            }

            else if (data.type === "confirm") {
              showConfirm(data.run_id, data.action, data.args);
            }

            else if (data.type === "cancelled") {
              assistantMsg.textContent = "Cancelled.";
              assistantMsg.className = "msg assistant";
              scrollToBottom();
              addActivity("done", "Cancelled by user");
              sawFinal = true;
            }

            else if (data.type === "error") {
              assistantMsg.textContent = data.message || "Error";
              assistantMsg.className = "msg assistant";
              scrollToBottom();
              addActivity("error", data.message || "Error");
              lastError = data.message || "Error";
              sawFinal = true;
            }

            else if (data.type === "phone_command") {
              addActivity("tool", `📱 Phone: ${data.action || "command"}`, data.target || "");
            }

            else if (data.type === "step") {
              const who = (data.worker || data.role || "").toLowerCase();
              if (who) {
                addActivity("worker", `Worker: ${who}`, data.task ? data.task.slice(0, 40) : "");
                // Update worker status in control center
                const workerKey = who.replace(/-/g, "");
                if (Object.keys(WORKERS).includes(workerKey)) {
                  updateWorkerStatus(workerKey, "working", data.task || "Processing...");
                }
              }
              if (data.step) appendMessage("system", "▸ " + data.step);
            }
            
            // Live worker status updates
            else if (data.type === "worker_status") {
              const workerKey = (data.worker || "").toLowerCase().replace(/-/g, "");
              if (Object.keys(WORKERS).includes(workerKey)) {
                const status = (data.status || "idle").toLowerCase();
                const activity = data.activity || "";
                updateWorkerStatus(workerKey, status, activity);
              }
            }
          } catch (e) {}
        }
      }

      if (!sawFinal) {
        assistantMsg.textContent = lastError || "Done.";
        assistantMsg.className = "msg assistant";
        scrollToBottom();
        addActivity("done", lastError ? "Completed with issues" : "Done");
      }

      statusEl.textContent = "Ready";
      statusEl.style.color = "var(--green)";
      sendBtn.style.display = "";
      cancelBtn.style.display = "none";
      currentRunId = null;
      return;
    } catch (e) {
      if (attempts >= maxAttempts) {
        assistantMsg.textContent = `Connection error — Friday may be busy. Try again.`;
        assistantMsg.className = "msg assistant";
        scrollToBottom();
        addActivity("error", "Connection failed");
        statusEl.textContent = "Offline";
        statusEl.style.color = "var(--red)";
        sendBtn.style.display = "";
        cancelBtn.style.display = "none";
        currentRunId = null;
      } else {
        assistantMsg.textContent = `Reconnecting… (${attempts}/${maxAttempts})`;
        scrollToBottom();
        await new Promise(r => setTimeout(r, 1500 * attempts));
      }
    }
  }
}

// ── Confirm dialog ───────────────────────────────────
function showConfirm(runId, action, args) {
  const box = document.createElement("div");
  box.className = "confirm-box";
  const msg = document.createElement("span");
  msg.textContent = `Allow: ${action}?`;
  box.appendChild(msg);
  const yes = document.createElement("button");
  yes.className = "confirm-btn confirm-yes";
  yes.textContent = "Yes";
  yes.onclick = async () => {
    box.remove();
    addActivity("result", "User approved: " + action);
    const r = await apiFetch(`${API_BASE}/run/${runId}/confirm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ approve: true }),
    });
    streamConfirmResponse(r);
  };
  const no = document.createElement("button");
  no.className = "confirm-btn confirm-no";
  no.textContent = "No";
  no.onclick = async () => {
    box.remove();
    addActivity("result", "User declined: " + action);
    const r = await apiFetch(`${API_BASE}/run/${runId}/confirm`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ approve: false }),
    });
    streamConfirmResponse(r);
  };
  box.appendChild(yes); box.appendChild(no);
  messagesEl.appendChild(box);
  scrollToBottom();
}

async function streamConfirmResponse(r) {
  const reader = r.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += decoder.decode(value, { stream: true });
    const lines = buf.split("\n");
    buf = lines.pop() || "";
    for (const line of lines) {
      if (!line.startsWith("data: ")) continue;
      try {
        const data = JSON.parse(line.slice(6));
        if (data.type === "final") {
          const msg = appendMessage("assistant", "");
          typewrite(msg, data.reply || "");
        }
        if (data.type === "result") {
          addActivity("result", `${data.action} → done`);
        }
        if (data.type === "thought") {
          addActivity("thinking", (data.thought || "").slice(0, 60));
        }
        if (data.type === "action") {
          addActivity("tool", data.action || "?");
        }
      } catch (e) {}
    }
  }
}

// ── Voice ────────────────────────────────────────────
let isRecording = false, mediaRecorder = null;
let recognition = null;

function ensureSpeechRecognition() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) return null;
  if (!recognition) {
    recognition = new SR();
    recognition.continuous = false;
    recognition.interimResults = false;
    recognition.lang = "en-US";
    recognition.maxAlternatives = 1;
  }
  return recognition;
}

micBtn.addEventListener("click", async () => {
  const rec = ensureSpeechRecognition();
  if (rec) {
    micBtn.classList.add("recording");
    isRecording = true;
    try {
      await new Promise((resolve, reject) => {
        rec.onresult = (e) => {
          const t = e.results[0][0].transcript.trim();
          if (t) {
            appendMessage("user", "🎤 " + t);
            sendMessage(t);
          } else {
            appendMessage("assistant", "Couldn't hear clearly.");
          }
          resolve();
        };
        rec.onerror = (e) => reject(e);
        rec.onend = () => resolve();
        rec.start();
        setTimeout(() => { if (isRecording) try { rec.stop(); } catch (e) {} }, 8000);
      });
    } catch {
      // fallback below
    } finally {
      isRecording = false;
      micBtn.classList.remove("recording");
    }
    return;
  }

  // Fallback: record and POST
  if (isRecording) { mediaRecorder?.stop(); return; }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    mediaRecorder = new MediaRecorder(stream);
    const chunks = [];
    mediaRecorder.ondataavailable = (e) => { if (e.data.size) chunks.push(e.data); };
    mediaRecorder.onstop = async () => {
      isRecording = false;
      micBtn.classList.remove("recording");
      stream.getTracks().forEach(t => t.stop());
      const blob = new Blob(chunks, { type: "audio/webm" });
      appendMessage("user", "🎤 Voice");
      const loadingEl = appendMessage("assistant", "Processing…", "typing");
      try {
        const buf = await blob.arrayBuffer();
        const bytes = new Uint8Array(buf);
        let binary = "";
        for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
        const b64 = btoa(binary);
        const r = await apiFetch(`${API_BASE}/voice`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ audio: b64, client_id: "friday" }),
        });
        const data = await r.json();
        loadingEl.className = "msg assistant";
        loadingEl.textContent = "";
        typewrite(loadingEl, data.response || "Done.");
        if (data.audio) playAudioB64(data.audio);
      } catch (e) {
        loadingEl.textContent = "Voice error.";
        loadingEl.className = "msg assistant";
      }
      scrollToBottom();
    };
    mediaRecorder.start();
    isRecording = true;
    micBtn.classList.add("recording");
    setTimeout(() => { if (isRecording) mediaRecorder?.stop(); }, 8000);
  } catch {
    appendMessage("assistant", "Microphone access denied.");
  }
});

function playAudioB64(b64) {
  if (!b64) return;
  try { const a = new Audio("data:audio/mp3;base64," + b64); a.play().catch(() => {}); } catch (e) {}
}

// ── Voice Session (continuous hands-free) ─────────────
let vsActive = false;
let vsRecognition = null;
let vsSilenceTimer = null;
let vsInterimEl = null;
let vsFinalText = "";
const VS_SILENCE_MS = 1500;

const vsBtn = document.getElementById("vs-btn");

function vsCreateInterim() {
  vsInterimEl = document.createElement("div");
  vsInterimEl.className = "vs-interim";
  vsBtn.appendChild(vsInterimEl);
}

function vsRemoveInterim() {
  if (vsInterimEl && vsInterimEl.parentNode) {
    vsInterimEl.parentNode.removeChild(vsInterimEl);
  }
  vsInterimEl = null;
}

function vsUpdateInterim(text, speaking) {
  if (!vsInterimEl) vsCreateInterim();
  vsInterimEl.textContent = text || "🎤 Listening…";
  vsInterimEl.className = "vs-interim" + (speaking ? " speaking" : "");
}

function vsResetSilence() {
  if (vsSilenceTimer) { clearTimeout(vsSilenceTimer); vsSilenceTimer = null; }
}

function vsStartSilenceTimer() {
  vsResetSilence();
  vsSilenceTimer = setTimeout(() => {
    if (vsFinalText.trim()) {
      vsRemoveInterim();
      vsSendVoiceTurn(vsFinalText.trim());
      vsFinalText = "";
    }
  }, VS_SILENCE_MS);
}

async function vsSendVoiceTurn(text) {
  if (!text) return;
  appendMessage("user", "🎤 " + text);
  statusEl.textContent = "Voice";
  statusEl.style.color = "var(--yellow)";
  const assistantMsg = appendMessage("assistant", "Thinking…", "typing");
  try {
    const r = await apiFetch(`${API_BASE}/command/stream`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text, lang: "en", client_id: "friday" }),
    });
    const reader = r.body.getReader();
    const decoder = new TextDecoder();
    let buf = "", fullReply = "", sawFinal = false;
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const lines = buf.split("\n");
      buf = lines.pop() || "";
      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        try {
          const data = JSON.parse(line.slice(6));
          if (data.type === "final") {
            sawFinal = true;
            fullReply = data.reply || "";
            assistantMsg.className = "msg assistant";
            assistantMsg.textContent = "";
            typewrite(assistantMsg, fullReply);
            statusEl.textContent = "Voice Session";
            statusEl.style.color = "var(--green)";
            if (fullReply && vsActive) {
              speakText(fullReply);
            }
          } else if (data.type === "thought") {
            const t = (data.thought || "").slice(0, 60);
            assistantMsg.textContent = t || "Thinking…";
          } else if (data.type === "audio" && data.audio) {
            playAudioB64(data.audio);
          } else if (data.type === "error") {
            assistantMsg.textContent = data.message || "Error";
            assistantMsg.className = "msg assistant";
            sawFinal = true;
          }
        } catch (e) {}
      }
    }
    if (!sawFinal) {
      assistantMsg.textContent = "Done.";
      assistantMsg.className = "msg assistant";
    }
    scrollToBottom();
  } catch (e) {
    assistantMsg.textContent = "Voice error — check connection.";
    assistantMsg.className = "msg assistant";
    scrollToBottom();
  }
}

function speakText(text) {
  if (!window.speechSynthesis) return;
  window.speechSynthesis.cancel();
  const u = new SpeechSynthesisUtterance(text);
  u.rate = 1.0;
  u.pitch = 1.0;
  u.volume = 1.0;
  u.lang = "en-US";
  window.speechSynthesis.speak(u);
}

function vsStart() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) {
    appendMessage("assistant", "Voice session not supported in this browser. Try Chrome.");
    return;
  }
  vsActive = true;
  vsFinalText = "";
  vsBtn.classList.add("active");
  statusEl.textContent = "Voice Session";
  statusEl.style.color = "var(--green)";
  addActivity("thinking", "Voice session started — speak naturally");
  vsCreateInterim();

  vsRecognition = new SR();
  vsRecognition.continuous = true;
  vsRecognition.interimResults = true;
  vsRecognition.lang = "en-US";
  vsRecognition.maxAlternatives = 3;

  vsRecognition.onresult = (e) => {
    let interim = "", final = "";
    for (let i = e.resultIndex; i < e.results.length; i++) {
      const t = e.results[i][0].transcript;
      if (e.results[i].isFinal) {
        final += t + " ";
      } else {
        interim += t;
      }
    }
    if (final) {
      vsFinalText += final;
      vsUpdateInterim(vsFinalText + (interim ? "…" : ""), !!interim);
      if (!interim) {
        vsResetSilence();
        vsSendVoiceTurn(vsFinalText.trim());
        vsFinalText = "";
      } else {
        vsStartSilenceTimer();
      }
    } else if (interim) {
      vsUpdateInterim(vsFinalText + " " + interim, true);
      vsStartSilenceTimer();
    }
  };

  vsRecognition.onerror = (e) => {
    if (e.error === "no-speech" || e.error === "aborted") return;
    addActivity("error", "Voice session error: " + e.error);
    if (e.error === "not-allowed") {
      vsStop();
      appendMessage("assistant", "Microphone access denied.");
    }
  };

  vsRecognition.onend = () => {
    if (vsActive) {
      try { vsRecognition.start(); } catch (e) {}
    }
  };

  try { vsRecognition.start(); } catch (e) {
    vsActive = false;
    vsBtn.classList.remove("active");
    addActivity("error", "Voice session failed to start");
  }
}

function vsStop() {
  vsActive = false;
  vsResetSilence();
  vsRemoveInterim();
  vsBtn.classList.remove("active");
  vsBtn.classList.remove("listening");
  if (vsRecognition) {
    try { vsRecognition.stop(); } catch (e) {}
    vsRecognition = null;
  }
  if (vsFinalText.trim()) {
    vsSendVoiceTurn(vsFinalText.trim());
    vsFinalText = "";
  }
  statusEl.textContent = "Ready";
  statusEl.style.color = "var(--green)";
  addActivity("done", "Voice session ended");
}

vsBtn.addEventListener("click", () => {
  if (vsActive) {
    vsStop();
  } else {
    vsStart();
  }
});

// ── Keyboard shortcuts ───────────────────────────────
inputEl.addEventListener("keydown", (e) => {
  if (e.key === "Enter" && !e.shiftKey) {
    e.preventDefault();
    sendMessage(inputEl.value);
  }
});
sendBtn.addEventListener("click", () => sendMessage(inputEl.value));
cancelBtn.addEventListener("click", async () => {
  if (currentRunId) {
    try { await apiFetch(`${API_BASE}/run/${currentRunId}/cancel`, { method: "POST" }); } catch (e) {}
  }
});

// Auto-resize textarea
inputEl.addEventListener("input", () => {
  inputEl.style.height = "auto";
  inputEl.style.height = Math.min(inputEl.scrollHeight, 80) + "px";
});

// ── Session Timer ────────────────────────────────────
const timerEl = document.getElementById("session-timer");
setInterval(() => {
  const elapsed = Math.floor((Date.now() - _sessionStart) / 1000);
  const m = Math.floor(elapsed / 60);
  const s = elapsed % 60;
  timerEl.textContent = `${m}:${s.toString().padStart(2, "0")}`;
}, 1000);

// ── Status polling ───────────────────────────────────
async function pollStatus() {
  try {
    const r = await apiFetch(`${API_BASE}/status`);
    const data = await r.json();
    const caps = {
      chat: document.getElementById("cap-chat"),
      voice: document.getElementById("cap-voice"),
      eye: document.getElementById("cap-eye"),
    };
    const homeCaps = {
      chat: document.getElementById("home-cap-chat"),
      voice: document.getElementById("home-cap-voice"),
      eye: document.getElementById("home-cap-eye"),
    };
    if (caps.chat) caps.chat.className = "header-cap" + (data.no_key ? "" : " active");
    if (caps.voice) caps.voice.className = "header-cap" + (data.providers?.length ? " active" : "");
    if (caps.eye) caps.eye.className = "header-cap" + (data.eye_active ? " active" : "");
    if (homeCaps.chat) homeCaps.chat.className = "home-cap" + (data.no_key ? "" : " active");
    if (homeCaps.voice) homeCaps.voice.className = "home-cap" + (data.providers?.length ? " active" : "");
    if (homeCaps.eye) homeCaps.eye.className = "home-cap" + (data.eye_active ? " active" : "");
    const tled = document.getElementById("trading-led");
    if (tled && data.trading) {
      const t = data.trading;
      tled.className = "header-cap" + (t.running && t.in_trade ? " trading-active" :
                                         t.running ? " trading-idle" : "");
      tled.title = t.running
        ? `Trading ${t.symbol} | PnL: $${t.daily_pnl} | Trades: ${t.trades_today} | Days: ${t.profitable_days}`
        : "Trading bot off";
    }
    updateTradingPanel(data.trading);
    const label = data.status === "online" ? "Ready" : data.status === "degraded" ? "Degraded" : "Offline";
    statusEl.textContent = label;
    statusEl.style.color = data.status === "online" ? "var(--green)" : data.status === "degraded" ? "var(--yellow)" : "var(--red)";
    const homeStatus = document.getElementById("home-status");
    if (homeStatus) {
      homeStatus.textContent = label;
      homeStatus.className = "home-status" + (data.status === "online" ? "" : data.status === "degraded" ? " degraded" : " offline");
    }
    const homeTradeCap = document.getElementById("home-cap-trade");
    if (homeTradeCap) homeTradeCap.className = "home-cap" + (data.trading?.running ? " active" : "");
    if (data.learning) {
      const l = data.learning;
      if (l.patterns_learned > 0 || l.skills_developed > 0) {
        statusEl.textContent = `Learned ${l.patterns_learned} patterns`;
      }
    }
  } catch (e) {
    statusEl.textContent = "Offline";
    statusEl.style.color = "var(--red)";
    const hs = document.getElementById("home-status");
    if (hs) { hs.textContent = "Offline"; hs.className = "home-status offline"; }
  }
}

// ── Trading Panel + Float Badge ──────────────────
let tradePanelOpen = false;
const tradePanel = document.getElementById("trade-panel");
const tradeLed = document.getElementById("trading-led");
const tradeFloat = document.getElementById("trade-float");
const tradeFloatPnl = document.getElementById("trade-float-pnl");
const tradeFloatInd = document.getElementById("trade-float-indicator");

function _openTradePanel() {
  tradePanelOpen = true;
  tradePanel.classList.add("open");
  document.querySelector(".chat-area").style.display = "none";
}

function _closeTradePanel() {
  tradePanelOpen = false;
  tradePanel.classList.remove("open");
  document.querySelector(".chat-area").style.display = "flex";
}

if (tradeLed) {
  tradeLed.addEventListener("click", () => {
    tradePanelOpen = !tradePanelOpen;
    tradePanel.classList.toggle("open", tradePanelOpen);
    const ca = document.querySelector(".chat-area");
    ca.style.display = tradePanelOpen ? "none" : "flex";
  });
}
if (tradeFloat) {
  tradeFloat.addEventListener("click", () => {
    widget.classList.remove("hidden");
    _openTradePanel();
    inputEl.focus();
  });
}

function updateTradingPanel(t) {
  const on = t && t.running;
  qs("#trade-status").className = "trade-status " + (on && t.in_trade ? "on" : on ? "idle" : "off");
  qs("#trade-status").textContent = on && t.in_trade ? "IN TRADE" : on ? "IDLE" : "OFF";
  const hasData = t && ((t.wins || 0) + (t.losses || 0) > 0 || t.running);
  tradeFloat.classList.toggle("hidden", !hasData);

  if (!t) {
    qs("#trade-daily").textContent = "$0.00";
    qs("#trade-total").textContent = "$0.00";
    qs("#trade-wins").textContent = "0";
    qs("#trade-losses").textContent = "0";
    qs("#trade-winrate").textContent = "0%";
    qs("#trade-trades-today").textContent = "0";
    qs("#trade-days").textContent = "0";
    qs("#trade-strategy").textContent = "London ORB";
    qs("#trade-last-result").textContent = "—";
    const hts0 = qs("#home-trade-status"); if (hts0) hts0.className = "home-trade-status", hts0.textContent = "OFF";
    const htt0 = qs("#home-trade-total"); if (htt0) htt0.textContent = "$0.00";
    const htd0 = qs("#home-trade-daily"); if (htd0) htd0.textContent = "$0.00";
    return;
  }
  const daily = t.daily_pnl || 0;
  const total = t.total_pnl || 0;
  qs("#trade-daily").textContent = (daily >= 0 ? "+" : "") + "$" + daily.toFixed(2);
  qs("#trade-daily").style.color = daily >= 0 ? "var(--green)" : "var(--red)";
  qs("#trade-total").textContent = (total >= 0 ? "+" : "") + "$" + total.toFixed(2);
  qs("#trade-total").style.color = total >= 0 ? "var(--green)" : "var(--red)";
  qs("#trade-wins").textContent = t.wins || 0;
  qs("#trade-losses").textContent = t.losses || 0;
  const totalTrades = (t.wins || 0) + (t.losses || 0);
  const wr = totalTrades > 0 ? ((t.wins / totalTrades) * 100).toFixed(1) : "0";
  qs("#trade-winrate").textContent = wr + "%";
  qs("#trade-trades-today").textContent = t.trades_today || 0;
  qs("#trade-days").textContent = t.profitable_days || 0;
  qs("#trade-strategy").textContent = t.strategy || "London ORB";
  const lr = document.getElementById("trade-last-result");
  if (t.last_result) {
    lr.textContent = t.last_result.toUpperCase();
    lr.className = "trade-last-result " + (t.last_result === "win" ? "win" : "loss");
  } else {
    lr.textContent = "—";
    lr.className = "trade-last-result";
  }

  // Update floating badge
  if (tradeFloatPnl) tradeFloatPnl.textContent = (total >= 0 ? "+" : "") + "$" + total.toFixed(0);
  if (tradeFloatInd) {
    tradeFloatInd.className = "trade-float-indicator " +
      (on && t.in_trade ? "on" : on ? "idle" : "off");
  }

  // Update home trade card
  const hts = qs("#home-trade-status");
  if (hts) { hts.textContent = on && t.in_trade ? "IN TRADE" : on ? "IDLE" : "OFF"; hts.className = "home-trade-status " + (on && t.in_trade ? "on" : on ? "idle" : "off"); }
  const htt = qs("#home-trade-total");
  if (htt) { htt.textContent = (total >= 0 ? "+" : "") + "$" + total.toFixed(2); htt.style.color = total >= 0 ? "var(--green)" : "var(--red)"; }
  const htd = qs("#home-trade-daily");
  if (htd) { htd.textContent = (daily >= 0 ? "+" : "") + "$" + daily.toFixed(2); htd.style.color = daily >= 0 ? "var(--green)" : "var(--red)"; }
}

pollStatus();
setInterval(pollStatus, 15000);

// ── Control Center Command Handler ──────────────────────────
document.addEventListener("keydown", (e) => {
  // Ctrl/Cmd + C to toggle control center
  if (e.key === "c" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    toggleControlCenter();
  }
});

// Handle "show control center" command from quick buttons
document.addEventListener("click", (e) => {
  const btn = e.target.closest(".quick-btn");
  if (btn) {
    const cmd = btn.getAttribute("data-cmd");
    if (cmd && cmd.toLowerCase().includes("control center")) {
      toggleControlCenter();
    }
  }
});

// ── Live Worker Status Polling ──────────────────────────────
let workerPollInterval = null;
function startWorkerPolling() {
  if (workerPollInterval) clearInterval(workerPollInterval);
  workerPollInterval = setInterval(async () => {
    try {
      const r = await apiFetch(`${API_BASE}/workers/status`);
      if (r.ok) {
        const data = await r.json();
        for (const [key, w] of Object.entries(data.workers || {})) {
          const workerKey = key.toLowerCase().replace(/-/g, "");
          if (Object.keys(WORKERS).includes(workerKey)) {
            updateWorkerStatus(workerKey, w.status || "idle", w.activity || "");
          }
        }
      }
    } catch (e) {
      // Silent fail - workers panel works with stream updates
    }
  }, 2000);
}

// Start worker polling when page loads
startWorkerPolling();
