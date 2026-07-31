/* Friday — iOS PWA (Add to Home Screen)
   Enhanced to match Android app: avatar, text size, continuous conversation,
   voice options, screen share, settings persistence, professional dark UI.

   24/7 smooth strategy for iOS (no background daemon like Android):
     1. Home-screen launch -> full-screen native feel.
     2. Background-audio + wake-lock trick -> page survives app-switching.
     3. Silent self-ping (25s) + local notification -> stays warm + alerts you.
     4. Screen/camera share -> /eye/submit device=ios-<uuid>.
     5. Chat/voice -> /command/stream + /voice.
     6. Registers as device -> /device/register so it shows in Control Center.
*/
const API = location.origin;
const FALLBACK_ORIGINS = [
  "https://friday-assistant-thrumming-wave-915.fly.dev",
];
let _apiOrigin = API;
async function apiFetch(url, opts) {
  try {
    return await window.fetch(url, opts);
  } catch (e) {
    if (_apiOrigin === API && FALLBACK_ORIGINS.length) {
      _apiOrigin = FALLBACK_ORIGINS[0];
      const fixed = url.replace(API, _apiOrigin);
      console.warn("[Friday iOS] origin unreachable, falling back to", _apiOrigin);
      return window.fetch(fixed, opts);
    }
    throw e;
  }
}
const TOKEN = new URLSearchParams(location.search).get("token") || "";
const AUTH = TOKEN ? { "Authorization": "Bearer " + TOKEN } : {};
const DEVICE = "ios-" + (localStorage.getItem("friday_ios_id") || (() => {
  const id = "ios-" + Math.random().toString(36).slice(2, 10);
  localStorage.setItem("friday_ios_id", id); return id;
})());

const $ = s => document.querySelector(s);
const connEl = $("#conn");
const msgs = $("#msgs");
const presenceEl = $("#presence");
const eyeEl = $("#eye");
const contBanner = $("#cont-banner");

// ── Settings / preferences ──────────────────────────────────
const PREFS_KEY = "friday_ios_prefs";
const _sessionStart = Date.now();
function loadPrefs() {
  try {
    const raw = localStorage.getItem(PREFS_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch (e) { return {}; }
}
function savePrefs(p) {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(p)); } catch (e) {}
}
const prefs = loadPrefs();

// Session timer
const sessionTimerEl = document.getElementById("session-timer");
function updateSessionTimer() {
  if (!sessionTimerEl) return;
  const elapsed = Math.floor((Date.now() - _sessionStart) / 1000);
  const m = Math.floor(elapsed / 60);
  const s = elapsed % 60;
  sessionTimerEl.textContent = m > 0 ? `${m}m ${s}s` : `${s}s`;
}
setInterval(updateSessionTimer, 1000);
updateSessionTimer();

// Apply persisted settings
function applyPrefs() {
  const textScale = prefs.textScale || 1.0;
  document.getElementById("msgs").style.fontSize = (13 * textScale) + "px";
  const avatar = prefs.avatar || "";
  const avatarEl = document.getElementById("avatar");
  if (avatarEl) {
    if (avatar === "initial" || !avatar) {
      avatarEl.src = "/static/assets/friday-mark.svg";
      avatarEl.style.display = "";
    } else if (avatar.startsWith("http")) {
      avatarEl.src = avatar;
      avatarEl.style.display = "";
    }
  }
  const agentName = prefs.agentName || "Friday";
  const nameEl = document.getElementById("agent-name");
  if (nameEl) nameEl.textContent = agentName;
  document.title = agentName + " — Friday";
  const voiceOn = prefs.voiceOn !== false;
  document.getElementById("t-voice")?.classList.toggle("on", voiceOn);
  const continuousOn = prefs.continuous || false;
  document.getElementById("t-cont")?.classList.toggle("on", continuousOn);
  document.getElementById("t-cont")?.classList.toggle("danger", continuousOn);
  document.getElementById("set-continuous")?.checked = continuousOn;
  if (continuousOn) startContinuous(); else stopContinuous();
  const eyeOn = prefs.eyeOn || false;
  document.getElementById("t-eye")?.classList.toggle("on", eyeOn);
  document.getElementById("set-eye")?.checked = eyeOn;
  if (eyeOn) startCapture("screen"); else stopCapture();
  const timeout = prefs.listeningTimeout || 8;
  document.getElementById("timeout-val").textContent = timeout + "s";
  document.getElementById("set-textsize").value = prefs.textScale || 1.0;
  document.getElementById("textsize-val").textContent = ((prefs.textScale || 1.0).toFixed(2)) + "x";
  document.getElementById("set-voiceid").value = prefs.voiceId || "en-IN-NeerjaNeural";
  document.getElementById("set-voice").checked = prefs.voiceOn !== false;
  document.getElementById("set-autospeak").checked = prefs.autoSpeak !== false;
  document.getElementById("set-server").value = prefs.server || "";
  document.getElementById("set-token").value = prefs.token || "";
  if (prefs.server) {
    // Override API origin for this session if a custom server was saved
    if (prefs.server !== location.origin) {
      _apiOrigin = prefs.server.replace(/\/$/, "");
    }
  }
}

// ── Chat (streamed, same contract as web Control Center) ──
function addMsg(role, text, cls) {
  const d = document.createElement("div");
  d.className = "msg " + role + (cls ? " " + cls : "");
  d.textContent = text;
  msgs.appendChild(d);
  scrollToBottom();
  updateContextSize();
  return d;
}
function scrollToBottom() {
  requestAnimationFrame(() => { msgs.scrollTop = msgs.scrollHeight; });
}
function updateContextSize() {
  const ctxEl = document.getElementById("ctx-size");
  if (!ctxEl) return;
  const msgCount = msgs.querySelectorAll(".msg").length;
  const approxTokens = msgCount * 100;
  const pct = Math.min(100, Math.round((approxTokens / 8000) * 100));
  ctxEl.textContent = `ctx ~${pct}%`;
  ctxEl.style.color = pct > 70 ? "var(--danger)" : pct > 40 ? "var(--warning)" : "var(--muted)";
}
function playB64(b64) {
  if (!b64) return;
  try { new Audio("data:audio/mp3;base64," + b64).play().catch(() => {}); } catch (e) {}
}

async function send(text) {
  if (!text.trim()) return;
  addMsg("user", text);
  const inp = document.getElementById("inp");
  if (inp) inp.value = "";
  const box = addMsg("assistant", (prefs.agentName || "Friday") + " is thinking…", "thinking");
  setPresence("thinking");
  let attempts = 0;
  const maxAttempts = 3;
  while (attempts < maxAttempts) {
    attempts++;
    try {
      const r = await apiFetch(API + "/command/stream", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ text, lang: "en", client_id: DEVICE }),
      });
      const reader = r.body.getReader(); const dec = new TextDecoder();
      let buf = "", got = false;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += dec.decode(value, { stream: true });
        const lines = buf.split("\n"); buf = lines.pop() || "";
        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          try {
            const d = JSON.parse(line.slice(6));
            if (d.type === "thought") { box.textContent = (prefs.agentName || "Friday") + " is thinking…"; box.className = "msg assistant thinking"; }
            else if (d.type === "step") {
              const who = d.worker || d.role || "";
              const what = d.task || d.result || d.step || "";
              box.textContent = who ? who + (what ? ": " + what : "") : (what || "working…");
              box.className = "msg assistant";
            }
            else if (d.type === "action") {
              box.textContent = "→ " + (d.action || "") + (d.args && d.args.expression ? " " + d.args.expression : "");
              box.className = "msg assistant";
            }
            else if (d.type === "result") {
              box.textContent = d.result != null ? String(d.result) : (d.action ? "done: " + d.action : "done.");
              box.className = "msg assistant"; got = true;
            }
            else if (d.type === "final") {
              const reply = (d.reply && String(d.reply).trim()) ? d.reply : (d.action ? "done: " + d.action : "Done.");
              box.textContent = reply;
              box.className = "msg assistant";
              got = true;
              if (prefs.autoSpeak !== false && d.audio) playB64(d.audio);
            }
            else if (d.type === "audio" && d.audio) {
              playB64(d.audio);
              setPresence("speaking");
              setTimeout(() => setPresence("here"), 1500);
            }
            else if (d.type === "confirm") showConfirm(d.run_id, d.action, d.args);
            else if (d.type === "error") { box.textContent = d.message || "Error"; box.className = "msg assistant"; }
          } catch (e) {}
        }
      }
      if (!got) { box.textContent = "Done."; box.className = "msg assistant"; }
      setPresence("here");
      return;
    } catch (e) {
      if (attempts >= maxAttempts) {
        box.textContent = `Connection error (${attempts}/${maxAttempts}) — try again.`;
        box.className = "msg assistant";
        setPresence("here");
      } else {
        box.textContent = `Reconnecting… (${attempts}/${maxAttempts})`;
        box.className = "msg assistant thinking";
        await new Promise(r => setTimeout(r, 1500 * attempts));
      }
    }
  }
}

// ── Voice ──
let rec = null, recing = false;
async function startVoice() {
  if (recing) { rec && rec.stop(); return; }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    rec = new MediaRecorder(stream);
    addMsg("assistant", "Friday is listening…", "thinking");
    const chunks = [];
    rec.ondataavailable = e => chunks.push(e.data);
    rec.onstop = async () => {
      stream.getTracks().forEach(t => t.stop());
      const blob = new Blob(chunks, { type: "audio/webm" });
      const fr = new FileReader();
      fr.onloadend = async () => {
        const b64 = fr.result.split(",")[1];
        try {
          const d = await (await apiFetch(API + "/voice", {
            method: "POST", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ audio: b64, client_id: DEVICE }),
          })).json();
          if (d.transcript) { addMsg("user", d.transcript); send(d.transcript); }
        } catch (e) { addMsg("assistant", "Voice error."); }
      };
      fr.readAsDataURL(blob);
    };
    rec.start(); recing = true;
    setTimeout(() => rec && rec.stop(), 5000);
  } catch (e) { addMsg("assistant", "Mic denied."); }
}

// ── Continuous conversation mode ─────────────────────────────
let continuousOn = false;
let contRec = null, contReqing = false;
let contAnalyzer = null, contSilenceTimer = null;
const SILENCE_MS = 1200; // 1.2s of silence = end of utterance
const MAX_UTTERANCE_MS = 15000; // 15s max per utterance

async function startContinuous() {
  if (continuousOn) return;
  continuousOn = true;
  contBanner.classList.remove("hidden");
  document.getElementById("t-cont")?.classList.add("on", "danger");
  setPresence("listening");
  listenLoop();
}
function stopContinuous() {
  continuousOn = false;
  contBanner.classList.add("hidden");
  document.getElementById("t-cont")?.classList.remove("on", "danger");
  if (contRec) { try { contRec.stop(); } catch (e) {} contRec = null; }
  if (contAnalyzer) { contAnalyzer.disconnect(); contAnalyzer = null; }
  if (contSilenceTimer) { clearTimeout(contSilenceTimer); contSilenceTimer = null; }
  setPresence("here");
}

// Voice Activity Detection using Web Audio API (detects silence instead of fixed timeout).
function setupVAD(stream) {
  const audioCtx = new (window.AudioContext || window.webkitAudioContext)();
  const source = audioCtx.createMediaStreamSource(stream);
  const analyzer = audioCtx.createAnalyser();
  analyzer.fftSize = 512;
  source.connect(analyzer);
  contAnalyzer = analyzer;
  return { audioCtx, analyzer };
}

function detectSilence(analyzer) {
  const data = new Uint8Array(analyzer.fftSize);
  analyzer.getByteTimeDomainData(data);
  let sum = 0;
  for (let i = 0; i < data.length; i++) {
    const v = (data[i] - 128) / 128;
    sum += v * v;
  }
  const rms = Math.sqrt(sum / data.length);
  return rms < 0.02; // silence threshold
}

async function listenLoop() {
  if (!continuousOn) return;
  if (contReqing) { setTimeout(listenLoop, 500); return; }
  try {
    const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    const { audioCtx, analyzer } = setupVAD(stream);
    contRec = new MediaRecorder(stream);
    const chunks = [];
    let lastVoiceTime = Date.now();
    let isSpeaking = false;

    contRec.ondataavailable = e => { if (e.data.size) chunks.push(e.data); };
    contRec.onstop = async () => {
      stream.getTracks().forEach(t => t.stop());
      if (audioCtx) audioCtx.close();
      const blob = new Blob(chunks, { type: "audio/webm" });
      const fr = new FileReader();
      fr.onloadend = async () => {
        const b64 = fr.result.split(",")[1];
        try {
          contReqing = true;
          const d = await (await apiFetch(API + "/voice", {
            method: "POST", headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ audio: b64, client_id: DEVICE }),
          })).json();
          if (d.transcript && d.transcript.trim()) {
            addMsg("user", d.transcript);
            send(d.transcript);
          }
        } catch (e) { /* ignore transient errors in continuous mode */ }
        finally { contReqing = false; setTimeout(listenLoop, 400); }
      };
      fr.readAsDataURL(blob);
    };

    // VAD polling loop
    const vadInterval = setInterval(() => {
      if (!continuousOn || !analyzer) { clearInterval(vadInterval); return; }
      const silent = detectSilence(analyzer);
      const now = Date.now();
      if (!silent) {
        lastVoiceTime = now;
        if (!isSpeaking) {
          isSpeaking = true;
        }
      } else if (isSpeaking) {
        if (now - lastVoiceTime > SILENCE_MS) {
          // Silence after speech -> end utterance
          isSpeaking = false;
          clearInterval(vadInterval);
          if (contRec && contRec.state === "recording") contRec.stop();
        } else if (now - lastVoiceTime > MAX_UTTERANCE_MS) {
          // Force-end long utterance
          isSpeaking = false;
          clearInterval(vadInterval);
          if (contRec && contRec.state === "recording") contRec.stop();
        }
      }
    }, 200);

    contRec.start();
  } catch (e) {
    setTimeout(listenLoop, 2000);
  }
}

// ── Confirmation (destructive actions) ──
function showConfirm(runId, action, args) {
  const box = document.createElement("div");
  box.className = "msg assistant confirm";
  box.innerHTML = `<div>${(prefs.agentName || "Friday")} wants to <b>${action}</b> — confirm?</div>
    <div class="row"><button class="mini yes">Yes</button><button class="mini no">No</button></div>`;
  msgs.appendChild(box); scrollToBottom();
  const done = (approve) => apiFetch(API + "/run/" + runId + "/confirm", {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ approve }),
  }).then(r => r.body.getReader()).then(async rd => {
    const dec = new TextDecoder(); let buf = "";
    while (true) { const { done, value } = await rd.read(); if (done) break;
      buf += dec.decode(value, { stream: true });
      for (const l of buf.split("\n")) { if (l.startsWith("data: ")) {
        try { const d = JSON.parse(l.slice(6));
          if (d.type === "final") box.textContent = d.reply; } catch (e) {} } }
    }
  });
  box.querySelector(".yes").onclick = () => done(true);
  box.querySelector(".no").onclick = () => done(false);
}

// ── Live eye (screen/camera share) ──────────────────────────
let capStream = null, capTimer = null;
async function startCapture(kind) {
  stopCapture();
  try {
    capStream = kind === "camera"
      ? await navigator.mediaDevices.getUserMedia({ video: { facingMode: "environment" } })
      : await navigator.mediaDevices.getDisplayMedia({ video: true });
  } catch (e) { eyeEl.textContent = "capture denied: " + e.message; return; }
  const v = document.createElement("video");
  v.srcObject = capStream; v.muted = true; v.play();
  const c = document.createElement("canvas");
  await new Promise(r => setTimeout(r, 400));
  capTimer = setInterval(async () => {
    if (!v.videoWidth) return;
    c.width = Math.min(640, v.videoWidth);
    c.height = c.width * (v.videoHeight / v.videoWidth);
    c.getContext("2d").drawImage(v, 0, 0, c.width, c.height);
    const b64 = c.toDataURL("image/jpeg", 0.55).split(",")[1];
    try {
      await apiFetch(API + "/eye/submit", {
        method: "POST", headers: { "Content-Type": "application/json", ...AUTH },
        body: JSON.stringify({ device: DEVICE, kind, image_b64: b64 }),
      });
      eyeEl.textContent = "👁 sharing…";
    } catch (e) {}
  }, 4000);
}
function stopCapture() {
  if (capTimer) clearInterval(capTimer);
  if (capStream) capStream.getTracks().forEach(t => t.stop());
  capStream = null;
}
async function refreshEye() {
  try {
    const d = await (await apiFetch(API + "/screen/state", { headers: AUTH })).json();
    const ph = d.devices && d.devices[DEVICE];
    eyeEl.textContent = (ph && ph.description)
      ? "👁 " + ph.description.slice(0, 160)
      : (d.description || "eye idle");
  } catch (e) { eyeEl.textContent = "eye offline"; }
}
setInterval(refreshEye, 4000);

// ── Presence ────────────────────────────────────────────────
function setPresence(state) {
  const map = {
    here: "Here.", thinking: "Thinking…", speaking: "Speaking…",
    listening: "Listening…", watching: "Watching your screen", busy: "Busy", offline: "Reconnecting…"
  };
  const label = map[state] || "Here.";
  presenceEl.textContent = label;
  presenceEl.className = "presence " + state;
}
async function refreshPresence() {
  try {
    const d = await (await apiFetch(API + "/status", { headers: AUTH })).json();
    if (!d || d.status === "offline") { setPresence("offline"); return; }
    const busy = d.session_lock && d.session_lock.busy;
    const holder = d.session_lock && d.session_lock.holder;
    if (busy && holder && holder !== DEVICE) { setPresence("busy"); return; }
    if (prefs.eyeOn && d.eye_active) { setPresence("watching"); return; }
    setPresence("here");
  } catch (e) { setPresence("offline"); }
}
setInterval(refreshPresence, 2500);

// ── iOS 24/7-smooth trick ───────────────────────────────────
const bg = document.getElementById("bg");
let stayOn = false;
function startStay() {
  stayOn = true;
  bg.play().catch(() => {});
  if (navigator.wakeLock) {
    navigator.wakeLock.request("system").catch(() => {});
  }
  notify("Friday staying awake", "Tap to open. Friday keeps itself warm.");
  selfPing();
}
function selfPing() {
  if (!stayOn) return;
  apiFetch(API + "/health", { headers: AUTH }).catch(() => {});
  setTimeout(selfPing, 25000);
}
function notify(title, body) {
  if (!("Notification" in window)) return;
  if (Notification.permission === "granted") {
    new Notification(title, { body, tag: "friday" });
  } else if (Notification.permission !== "denied") {
    Notification.requestPermission();
  }
}

// ── Settings drawer ─────────────────────────────────────────
function openSettings() { document.getElementById("settings-drawer").classList.add("show"); }
function closeSettings() { document.getElementById("settings-drawer").classList.remove("show"); }

document.getElementById("settings-btn")?.addEventListener("click", openSettings);
document.getElementById("drawer-close")?.addEventListener("click", closeSettings);
document.getElementById("drawer-bg")?.addEventListener("click", closeSettings);
document.getElementById("set-save")?.addEventListener("click", () => {
  prefs.server = document.getElementById("set-server").value.trim();
  prefs.token = document.getElementById("set-token").value.trim();
  prefs.voiceOn = document.getElementById("set-voice").checked;
  prefs.autoSpeak = document.getElementById("set-autospeak").checked;
  prefs.voiceId = document.getElementById("set-voiceid").value.trim();
  prefs.continuous = document.getElementById("set-continuous").checked;
  prefs.eyeOn = document.getElementById("set-eye").checked;
  prefs.reduceMotion = document.getElementById("set-motion").checked;
  prefs.textScale = parseFloat(document.getElementById("set-textsize").value) || 1.0;
  savePrefs(prefs);
  closeSettings();
  location.reload();
});
document.getElementById("set-export")?.addEventListener("click", () => {
  const msgsEl = document.getElementById("msgs");
  if (!msgsEl) return;
  const lines = [];
  msgsEl.querySelectorAll(".msg").forEach(m => {
    const role = m.classList.contains("user") ? "You" : (prefs.agentName || "Friday");
    lines.push(`${role}: ${m.textContent}`);
  });
  const text = lines.join("\n\n");
  const blob = new Blob([text], { type: "text/plain" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `friday-session-${new Date().toISOString().slice(0,10)}.txt`;
  a.click();
  URL.revokeObjectURL(url);
});
document.getElementById("set-textsize")?.addEventListener("input", (e) => {
  document.getElementById("textsize-val").textContent = parseFloat(e.target.value).toFixed(2) + "x";
});
document.getElementById("set-timeout-inc")?.addEventListener("click", () => {
  const cur = prefs.listeningTimeout || 8;
  prefs.listeningTimeout = Math.min(30, cur + 1);
  document.getElementById("timeout-val").textContent = prefs.listeningTimeout + "s";
  savePrefs(prefs);
});

// ── Toggle strip (mirrors Android settings) ────────────────
let eyeOn = false, voiceOn = true;
document.getElementById("t-eye")?.addEventListener("click", () => {
  eyeOn = !eyeOn;
  prefs.eyeOn = eyeOn; savePrefs(prefs);
  const b = document.getElementById("t-eye");
  if (b) b.classList.toggle("on", eyeOn);
  if (eyeOn) startCapture("screen"); else stopCapture();
});
document.getElementById("t-voice")?.addEventListener("click", () => {
  voiceOn = !voiceOn;
  prefs.voiceOn = voiceOn; savePrefs(prefs);
  const b = document.getElementById("t-voice");
  if (b) b.classList.toggle("on", voiceOn);
});
document.getElementById("t-cont")?.addEventListener("click", () => {
  const next = !continuousOn;
  prefs.continuous = next; savePrefs(prefs);
  const b = document.getElementById("t-cont");
  if (b) { b.classList.toggle("on", next); b.classList.toggle("danger", next); }
  if (next) startContinuous(); else stopContinuous();
});
document.getElementById("cont-stop")?.addEventListener("click", () => {
  stopContinuous();
  prefs.continuous = false; savePrefs(prefs);
  const b = document.getElementById("t-cont");
  if (b) b.classList.remove("on", "danger");
});
document.getElementById("stay")?.addEventListener("click", () => {
  if (!stayOn) { startStay(); const b = document.getElementById("stay"); if (b) { b.textContent = "◻ Awake"; b.classList.add("on"); } }
});

// ── Wire up (guarded so missing elements never break the rest) ──
function bind(id, ev, fn) { const el = document.getElementById(id); if (el) el.addEventListener(ev, fn); }
const inpEl = document.getElementById("inp");
function doSend() { send((inpEl && inpEl.value) || ""); }
bind("share", "click", () => startCapture("screen"));
bind("cam", "click", () => startCapture("camera"));
bind("send", "click", doSend);
bind("mic", "click", startVoice);
if (inpEl) {
  inpEl.addEventListener("keydown", e => {
    if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); doSend(); }
  });
  inpEl.addEventListener("input", () => {
    inpEl.style.height = "auto";
    inpEl.style.height = Math.min(inpEl.scrollHeight, 96) + "px";
  });
}

// ── Boot ────────────────────────────────────────────────────
function boot() {
  applyPrefs();
  register();
  refreshPresence();
  refreshEye();
  notify("Friday ready", "Open from Home Screen for full-screen experience.");
}
if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", boot);
} else {
  boot();
}
