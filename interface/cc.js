// ── Friday Control Center ──
// Professional, live, zero-lag interface
// All controls auto-managed; kill switch only manual override

let API_BASE = window.location.origin;
let ws = null;
let wsReconnectAttempts = 0;
const MAX_WS_RECONNECT = 10;
const WS_RECONNECT_DELAY = 2000;

// ── State ──
const state = {
  views: ['dashboard', 'trading', 'workers', 'bots', 'mt5', 'logs', 'settings', 'devices'],
  currentView: 'dashboard',
  workers: {},
  bots: [],
  trading: { running: false, pnl: 0, totalPnl: 0, positions: [] },
  mt5: { connected: false, latency: 0, account: null, terminal: {} },
  system: { uptime: 0, workersActive: 0, providers: [] },
  logs: [],
  chat: { history: [], runId: null, streaming: false },
  killArmed: false,
  confirmDestructive: false,
  devices: { android: { connected: false, lastSeen: null, battery: null, version: null, wakeWord: false, capabilities: {} }, ios: { connected: false, lastSeen: null, battery: null, version: null, wakeWord: false, capabilities: {} } },
};

// ── DOM Refs ──
const $ = (id) => document.getElementById(id);
const $$ = (sel, root = document) => root.querySelectorAll(sel);
const el = (html) => { const t = document.createElement('template'); t.innerHTML = html.trim(); return t.content.firstChild; };

// ── Utilities ──
const sleep = (ms) => new Promise(r => setTimeout(r, ms));
const fmtNum = (n, decimals = 2) => (n >= 0 ? '+' : '') + Number(n).toFixed(decimals);
const fmtTime = (sec) => { const h = Math.floor(sec / 3600); const m = Math.floor((sec % 3600) / 60); const s = sec % 60; return `${h}h ${m}m ${s}s`; };
const nowISO = () => new Date().toISOString().slice(11, 23);

function showToast(message, type = 'info', duration = 4000) {
  const container = $('#toast-container');
  const toast = el(`
    <div class="toast ${type}" role="alert">
      <span class="toast-icon">
        ${type === 'success' ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>' : ''}
        ${type === 'error' ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>' : ''}
        ${type === 'warning' ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>' : ''}
        ${type === 'info' ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>' : ''}
      </span>
      <span class="toast-message">${message}</span>
      <button class="toast-close" aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
    </div>
  `);
  container.appendChild(toast);
  toast.querySelector('.toast-close').onclick = () => toast.remove();
  if (duration) setTimeout(() => toast.remove(), duration);
}

function showConfirm(title, message, details, onConfirm) {
  const modal = $('#confirm-modal');
  $('#confirm-title').textContent = title;
  $('#confirm-message').textContent = message;
  $('#confirm-details').textContent = details || '';
  modal.showModal().then(() => {
    const ok = $('#confirm-ok');
    ok.onclick = async () => {
      modal.close();
      await onConfirm();
    };
    modal.querySelector('[value="cancel"]').onclick = () => modal.close();
  });
}

function showKillSwitch() {
  const modal = $('#kill-modal');
  $('#kill-input').value = '';
  $('#kill-confirm').disabled = true;
  $('#kill-input').oninput = (e) => { $('#kill-confirm').disabled = e.target.value !== 'KILL'; };
  modal.showModal().then(() => {
    $('#kill-confirm').onclick = async () => {
      modal.close();
      await executeKillSwitch();
    };
    modal.querySelector('[value="cancel"]').onclick = () => modal.close();
  });
}

// ── API ──
async function apiFetch(path, opts = {}) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), 120000);
  try {
    const res = await fetch(API_BASE + path, {
      ...opts,
      headers: { 'Content-Type': 'application/json', ...opts.headers },
      signal: ctrl.signal,
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    return res;
  } finally { clearTimeout(timer); }
}

async function apiJson(path, opts = {}) {
  const res = await apiFetch(path, opts);
  return res.json();
}

// ── WebSocket ──
function connectWS() {
  const wsUrl = API_BASE.replace(/^http/, 'ws') + '/ws/control';
  ws = new WebSocket(wsUrl);
  ws.binaryType = 'arraybuffer';

  ws.onopen = () => {
    wsReconnectAttempts = 0;
    console.log('[WS] Connected');
    sendWS({ type: 'subscribe', channels: ['workers', 'trading', 'mt5', 'system', 'logs', 'earnings'] });
  };

  ws.onmessage = (ev) => {
    try {
      const data = JSON.parse(ev.data);
      handleWSMessage(data);
    } catch (e) { console.error('[WS] Parse error', e); }
  };

  ws.onclose = () => {
    console.log('[WS] Closed, reconnecting...');
    if (wsReconnectAttempts < MAX_WS_RECONNECT) {
      wsReconnectAttempts++;
      setTimeout(connectWS, WS_RECONNECT_DELAY * wsReconnectAttempts);
    }
  };

  ws.onerror = (err) => console.error('[WS] Error', err);
}

function sendWS(msg) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(msg));
}

function handleWSMessage(msg) {
  switch (msg.type) {
    case 'worker_status': updateWorker(msg); break;
    case 'trading_update': updateTrading(msg); break;
    case 'mt5_status': updateMT5(msg); break;
    case 'system_update': updateSystem(msg); break;
    case 'log': appendLog(msg); break;
    case 'earnings_update': updateEarnings(msg); break;
    case 'bot_update': updateBots(msg); break;
    case 'chat_message': handleChatMessage(msg); break;
    case 'chat_final': handleChatFinal(msg); break;
    case 'chat_error': handleChatError(msg); break;
    case 'chat_audio': playAudio(msg.audio); break;
    default: console.log('[WS] Unknown', msg);
  }
}

// ── View Management ──
function switchView(name) {
  if (!state.views.includes(name)) return;
  $$('.view').forEach(v => v.classList.remove('active'));
  $$('.nav-item').forEach(n => n.classList.remove('active'));
  const view = $('#view-' + name);
  const nav = document.querySelector(`[data-view="${name}"]`);
  if (view) view.classList.add('active');
  if (nav) nav.classList.add('active');
  state.currentView = name;
  loadViewData(name);
}

function loadViewData(view) {
  switch (view) {
    case 'dashboard': loadDashboard(); break;
    case 'trading': loadTrading(); break;
    case 'workers': loadWorkers(); break;
    case 'bots': loadBots(); break;
    case 'mt5': loadMT5(); break;
    case 'devices': loadDevices(); break;
    case 'logs': loadLogs(); break;
    case 'settings': loadSettings(); break;
  }
}

// ── Data Loaders ──
async function loadDashboard() {
  try {
    const [status, earnings] = await Promise.all([
      apiJson('/status'),
      apiJson('/bots/earnings'),
    ]);
    updateDashboard(status, earnings);
  } catch (e) { console.error('Dashboard load failed', e); }
}

function updateDashboard(status, earnings) {
  const tradePnl = status.trading?.total_pnl || 0;
  const botEarnings = earnings.total_earnings || 0;
  const total = tradePnl + botEarnings;

  $('#stat-workers').textContent = `${status.workers_active || 0}/9`;
  $('#stat-bots').textContent = (status.bots?.length || 0) + (status.trading?.running ? 1 : 0);
  $('#stat-earnings').textContent = `${total >= 0 ? '+' : ''}$${total.toFixed(2)}`;
  $('#stat-earnings').style.color = total >= 0 ? 'var(--success)' : 'var(--danger)';
  $('#stat-uptime').textContent = fmtTime(status.uptime_seconds || 0);
}

async function loadTrading() {
  try {
    const [status, earn] = await Promise.all([
      apiJson('/status'),
      apiJson('/bots/earnings'),
    ]);
    renderTrading(status.trading, earn);
  } catch (e) { console.error('Trading load failed', e); }
}

function renderTrading(trading, earnings) {
  const running = trading?.running || false;
  const pnl = trading?.total_pnl || 0;
  const daily = trading?.daily_pnl || 0;

  $('#trade-status').textContent = running ? 'RUNNING' : 'STOPPED';
  $('#trade-status').className = 'badge ' + (running ? 'live' : '');
  $('#trade-pnl').textContent = `${pnl >= 0 ? '+' : ''}$${pnl.toFixed(2)}`;
  $('#trade-pnl').className = pnl >= 0 ? 'positive' : 'negative';
  $('#trade-daily').textContent = `${daily >= 0 ? '+' : ''}$${daily.toFixed(2)}`;
  $('#trade-positions').textContent = trading?.positions?.length || 0;
  $('#trade-bots').textContent = earnings.active_bots || 0;
  $('#trade-earnings').textContent = `$${(earnings.total_earnings || 0).toFixed(2)}`;

  // Positions table
  const tbody = $('#trade-positions-body');
  const positions = trading?.positions || [];
  if (positions.length) {
    tbody.innerHTML = positions.map(p => `
      <tr>
        <td>${p.symbol}</td>
        <td>${p.type}</td>
        <td>${p.volume}</td>
        <td>${p.open_price.toFixed(p.symbol?.includes('JPY') ? 3 : 5)}</td>
        <td>${p.current_price.toFixed(p.symbol?.includes('JPY') ? 3 : 5)}</td>
        <td class="${p.pnl >= 0 ? 'positive' : 'negative'}">${p.pnl >= 0 ? '+' : ''}$${p.pnl.toFixed(2)}</td>
        <td>${p.sl || '-'}</td>
        <td>${p.tp || '-'}</td>
      </tr>
    `).join('');
  } else {
    tbody.innerHTML = '<tr><td colspan="8" style="text-align:center;color:var(--text-dim);padding:20px">No open positions</td></tr>';
  }

  // Bot earnings
  const botList = earnings.bots || [];
  $('#trade-earnings-list').innerHTML = botList.length ? botList.map(b => `
    <div class="action-item">
      <div class="action-icon ${b.earnings >= 0 ? 'success' : 'danger'}">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
      </div>
      <div class="action-desc">
        <div>${b.name}</div>
        <div style="font-size:11px;color:var(--text-dim)">${b.type} · ${b.symbol || 'N/A'}</div>
      </div>
      <span style="color:${b.earnings >= 0 ? 'var(--success)' : 'var(--danger)'};font-weight:700">${b.earnings >= 0 ? '+' : ''}$${b.earnings.toFixed(2)}</span>
    </div>
  `).join('') : '<div style="color:var(--text-dim);padding:20px;text-align:center">No bot earnings yet</div>';

  // Update trade float
  updateTradeFloat(pnl, running);
}

function updateTradeFloat(pnl, running) {
  const tf = $('#trade-float');
  if (!running || pnl === 0) { tf.classList.add('hidden'); return; }
  tf.classList.remove('hidden');
  $('#trade-float-pnl').textContent = `${pnl >= 0 ? '+' : ''}$${pnl.toFixed(2)}`;
  $('#trade-float-pnl').style.color = pnl >= 0 ? 'var(--success)' : 'var(--danger)';
  $('#trade-float-indicator').style.background = running ? 'var(--success)' : 'var(--text-dim)';
}

async function loadWorkers() {
  try {
    const res = await apiJson('/workers/status');
    renderWorkers(res.workers || {});
  } catch (e) { console.error('Workers load failed', e); }
}

function renderWorkers(workers) {
  const grid = $('#workers-grid');
  const icons = { vayu: '➤', neo: '🧠', forge: '⚙', scout: '🔍', verdict: '⚖', prism: '✅', oracle: '🗺', titan: '🔨', sentinel: '🛡' };
  grid.innerHTML = Object.entries(workers).map(([name, info]) => {
    const status = info.status || 'idle';
    return `
      <article class="worker-card">
        <header class="worker-header">
          <div class="worker-avatar" style="background:linear-gradient(135deg,var(--accent),var(--accent2))">${icons[name.toLowerCase()] || '◆'}</div>
          <div class="worker-info">
            <div class="worker-name">${name}</div>
            <div class="worker-role">${info.role || 'worker'}</div>
          </div>
          <div class="worker-status ${status}">
            <span class="dot"></span>
            <span>${status}</span>
          </div>
        </header>
        <div class="worker-metrics">
          <div class="metric"><div class="metric-value">${info.tasks_completed || 0}</div><div class="metric-label">Completed</div></div>
          <div class="metric"><div class="metric-value">${info.success_rate || 0}%</div><div class="metric-label">Success</div></div>
          <div class="metric"><div class="metric-value">${info.avg_latency_ms || 0}ms</div><div class="metric-label">Avg Latency</div></div>
        </div>
      </article>
    `;
  }).join('');
}

async function loadBots() {
  try {
    const res = await apiJson('/bots');
    state.bots = res.bots || [];
    renderBots(state.bots);
  } catch (e) { console.error('Bots load failed', e); }
}

function renderBots(bots) {
  const grid = $('#bots-grid');
  if (!bots.length) {
    grid.innerHTML = '<div style="grid-column:1/-1;text-align:center;color:var(--text-dim);padding:40px">No bots running. Create one to start.</div>';
    return;
  }
  grid.innerHTML = bots.map(b => `
    <article class="bot-card">
      <header class="bot-header">
        <div>
          <div class="bot-name">${b.name}</div>
          <span class="bot-type">${b.type}</span>
        </div>
        <div class="bot-status ${b.status}"><span class="dot"></span>${b.status}</div>
      </header>
      <pre class="bot-config">${JSON.stringify(b.config, null, 2)}</pre>
      <div class="bot-actions">
        <button class="btn secondary" onclick="stopBot('${b.id}')">Stop</button>
        <button class="btn danger" onclick="deleteBot('${b.id}')">Delete</button>
      </div>
    </article>
  `).join('');
}

window.stopBot = async (id) => {
  await apiJson('/bots/stop', { method: 'POST', body: JSON.stringify({ bot_id: id }) });
  loadBots();
};

window.deleteBot = async (id) => {
  if (!confirm('Delete this bot permanently?')) return;
  await apiJson('/bots/delete', { method: 'POST', body: JSON.stringify({ bot_id: id }) });
  loadBots();
};

async function loadMT5() {
  try {
    const res = await apiJson('/mt5/status');
    renderMT5(res);
  } catch (e) { console.error('MT5 load failed', e); }
}

function renderMT5(data) {
  const connected = data.connected || false;
  $('#mt5-conn-status').innerHTML = `<span class="dot ${connected ? 'connected' : 'disconnected'}"></span>${connected ? 'Connected' : 'Disconnected'}`;
  $('#mt5-conn-status').className = 'connection-status ' + (connected ? 'connected' : 'disconnected');
  $('#btn-mt5-connect').disabled = connected;
  $('#btn-mt5-disconnect').disabled = !connected;
  $('#mt5-conn-state').textContent = connected ? 'Connected' : 'Disconnected';
  $('#mt5-latency').textContent = data.latency ? `${data.latency}ms` : '—';
  $('#mt5-account').textContent = data.account || '—';
  $('#mt5-terminal-status').innerHTML = data.terminal ? `
    <div class="action-item"><div class="action-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/></svg></div><div class="action-desc"><div>Terminal</div><div style="font-size:11px;color:var(--text-dim)">${data.terminal.path || 'Found'}</div></div></div>
    <div class="action-item"><div class="action-icon result"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg></div><div class="action-desc"><div>Version</div><div style="font-size:11px;color:var(--text-dim)">${data.terminal.version || 'Unknown'}</div></div></div>
    <div class="action-item"><div class="action-icon tool"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8"/><path d="M12 17v4"/></svg></div><div class="action-desc"><div>Process ID</div><div style="font-size:11px;color:var(--text-dim)">${data.terminal.pid || 'N/A'}</div></div></div>
  ` : '<div style="color:var(--text-dim);padding:20px;text-align:center">MT5 not detected</div>';

  // Logs
  const logs = data.logs || [];
  $('#mt5-logs').innerHTML = logs.length ? logs.map(l => `
    <div class="log-line">
      <span class="log-time">${l.time}</span>
      <span class="log-level ${l.level}">${l.level}</span>
      <span class="log-source">MT5</span>
      <span class="log-message">${l.message}</span>
    </div>
  `).join('') : '<div class="log-line"><span class="log-time">—</span><span class="log-level info">INFO</span><span class="log-source">MT5</span><span class="log-message">No logs yet</span></div>';
}

async function loadLogs() {
  try {
    const res = await apiJson('/logs?limit=200');
    state.logs = res.logs || [];
    renderLogs(state.logs);
  } catch (e) { console.error('Logs load failed', e); }
}

function renderLogs(logs) {
  const levelFilter = $('#log-level').value;
  const sourceFilter = $('#log-source').value;
  const filtered = logs.filter(l => {
    if (levelFilter !== 'all' && l.level !== levelFilter) return false;
    if (sourceFilter !== 'all' && l.source !== sourceFilter) return false;
    return true;
  });
  $('#log-stream').innerHTML = filtered.map(l => `
    <div class="log-line">
      <span class="log-time">${l.time}</span>
      <span class="log-level ${l.level}">${l.level}</span>
      <span class="log-source">${l.source}</span>
      <span class="log-message">${l.message}</span>
    </div>
  `).join('');
}

async function loadSettings() {
  try {
    const res = await apiJson('/config');
    applySettings(res);
  } catch (e) { console.error('Settings load failed', e); }
}

function applySettings(cfg) {
  // Apply loaded settings to form
  Object.keys(cfg).forEach(key => {
    const el = document.getElementById('set-' + key.replace(/_/g, '-'));
    if (el) {
      if (el.type === 'checkbox') el.checked = cfg[key];
      else el.value = cfg[key];
    }
  }
}

function applySettings(cfg) {
  // Apply loaded settings to form
  Object.keys(cfg).forEach(key => {
    const el = document.getElementById('set-' + key.replace(/_/g, '-'));
    if (el) {
      if (el.type === 'checkbox') el.checked = cfg[key];
      else el.value = cfg[key];
    }
  });
}

// ── Device Functions ──
async function loadDevices() {
  try {
    const res = await apiJson('/devices/status');
    state.devices = res.devices || state.devices;
    renderDevices(state.devices);
  } catch (e) { console.error('Devices load failed', e); }
}

function renderDevices(devices) {
  // Update sidebar status
  const android = devices.android || {};
  const ios = devices.ios || {};

  $('#android-status').innerHTML = `<span class="dot ${android.connected ? 'connected' : 'offline'}"></span>${android.connected ? 'Online' : 'Offline'}`;
  $('#ios-status').innerHTML = `<span class="dot ${ios.connected ? 'connected' : 'offline'}"></span>${ios.connected ? 'Online' : 'Offline'}`;

  $('#android-card-status').innerHTML = `<span class="dot ${android.connected ? 'connected' : 'offline'}"></span>${android.connected ? 'Online' : 'Offline'}`;
  $('#ios-card-status').innerHTML = `<span class="dot ${ios.connected ? 'connected' : 'offline'}"></span>${ios.connected ? 'Online' : 'Offline'}`;

  $('#android-version').textContent = android.version || '—';
  $('#ios-version').textContent = ios.version || '—';
  $('#android-last-seen').textContent = android.last_seen ? fmtTime((Date.now() - android.last_seen) / 1000) + ' ago' : '—';
  $('#ios-last-seen').textContent = ios.last_seen ? fmtTime((Date.now() - ios.last_seen) / 1000) + ' ago' : '—';
  $('#android-battery').textContent = android.battery ? android.battery + '%' : '—';
  $('#ios-battery').textContent = ios.battery ? ios.battery + '%' : '—';
  $('#android-wake').textContent = android.wake_word ? 'Active' : 'Inactive';
  $('#ios-wake').textContent = ios.wake_word ? 'Active' : 'Inactive';

  // Capabilities
  const caps = ['voice', 'screen', 'sms', 'notifications', 'apps', 'contacts', 'location', 'media'];
  caps.forEach(cap => {
    const androidCap = $('#android-caps li[data-cap="' + cap + '"]');
    const iosCap = $('#ios-caps li[data-cap="' + cap + '"]');
    if (androidCap) androidCap.classList.toggle('connected', !!(devices.android?.capabilities?.[cap]));
    if (iosCap) iosCap.classList.toggle('connected', !!(devices.ios?.capabilities?.[cap]));
  });

  // Device logs
  const logs = devices.logs || [];
  $('#device-logs').innerHTML = logs.length ? logs.map(l => `
    <div class="log-line">
      <span class="log-time">${l.time}</span>
      <span class="log-level ${l.level}">${l.level}</span>
      <span class="log-source">${l.source}</span>
      <span class="log-message">${l.message}</span>
    </div>
  `).join('') : '<div class="log-line"><span class="log-time">—</span><span class="log-level info">INFO</span><span class="log-source">DEVICE</span><span class="log-message">No device logs yet</span></div>';
}

async function connectAndroid() {
  showToast('Connecting to Android...', 'info');
  try {
    const res = await apiJson('/devices/android/connect', { method: 'POST' });
    if (res.success) { showToast('Android connected', 'success'); loadDevices(); }
    else showToast('Connect failed: ' + res.error, 'error');
  } catch (e) { showToast('Android connect error: ' + e.message, 'error'); }
}

async function testAndroidVoice() {
  showToast('Sending test voice command to Android...', 'info');
  try {
    const res = await apiJson('/devices/android/test-voice', { method: 'POST' });
    if (res.success) { showToast('Test voice sent to Android', 'success'); }
    else showToast('Test failed: ' + res.error, 'error');
  } catch (e) { showToast('Test voice error: ' + e.message, 'error'); }
}

async function connectIOS() {
  showToast('Connecting to iOS...', 'info');
  try {
    const res = await apiJson('/devices/ios/connect', { method: 'POST' });
    if (res.success) { showToast('iOS connected', 'success'); loadDevices(); }
    else showToast('Connect failed: ' + res.error, 'error');
  } catch (e) { showToast('iOS connect error: ' + e.message, 'error'); }
}

async function testIOSVoice() {
  showToast('Sending test voice command to iOS...', 'info');
  try {
    const res = await apiJson('/devices/ios/test-voice', { method: 'POST' });
    if (res.success) { showToast('Test voice sent to iOS', 'success'); }
    else showToast('Test failed: ' + res.error, 'error');
  } catch (e) { showToast('Test voice error: ' + e.message, 'error'); }
}
  try {
    showToast('Executing emergency kill switch...', 'warning');
    const res = await apiJson('/emergency/kill', { method: 'POST' });
    if (res.success) {
      showToast('KILL SWITCH EXECUTED — All systems halted', 'warning', 10000);
      loadDashboard();
      loadTrading();
      loadWorkers();
      loadMT5();
    } else {
      showToast('Kill switch failed: ' + res.error, 'error');
    }
  } catch (e) {
    showToast('Kill switch error: ' + e.message, 'error');
  }
}

async function connectMT5() {
  try {
    showToast('Connecting MT5 bridge...', 'info');
    const res = await apiJson('/mt5/connect', { method: 'POST' });
    if (res.success) { showToast('MT5 connected', 'success'); loadMT5(); }
    else showToast('Connect failed: ' + res.error, 'error');
  } catch (e) { showToast('MT5 connect error: ' + e.message, 'error'); }
}

async function disconnectMT5() {
  try {
    const res = await apiJson('/mt5/disconnect', { method: 'POST' });
    if (res.success) { showToast('MT5 disconnected', 'info'); loadMT5(); }
    else showToast('Disconnect failed: ' + res.error, 'error');
  } catch (e) { showToast('MT5 disconnect error: ' + e.message, 'error'); }
}

async function createBot() {
  const name = $('#bot-name').value;
  const type = $('#bot-type').value;
  const symbols = $('#bot-symbols').value;
  const interval = parseInt($('#bot-interval').value);
  const config = JSON.parse($('#bot-config').value || '{}');
  config.symbols = symbols.split(',').map(s => s.trim()).filter(Boolean);
  config.interval = interval;

  try {
    const res = await apiJson('/bots/create', { method: 'POST', body: JSON.stringify({ bot_type: type, name, config }) });
    if (res.id) { $('#bot-dialog').close(); loadBots(); showToast('Bot created: ' + name, 'success'); }
    else showToast('Create failed: ' + res.error, 'error');
  } catch (e) { showToast('Create bot error: ' + e.message, 'error'); }
}

// ── Chat ──
let chatInputEl = null;

function initChat() {
  chatInputEl = $('#chat-input');
  const chatFloat = $('#chat-float');
  const chatHeader = $('#chat-header');

  // Toggle collapse
  chatHeader.onclick = (e) => {
    if (e.target.closest('.chat-actions')) return;
    chatFloat.classList.toggle('chat-collapsed');
  };

  // Send
  $('#chat-send').onclick = () => sendChatMessage();
  chatInputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendChatMessage(); }
  });

  // Mic
  $('#chat-mic').onclick = startVoiceInput;

  // Auto-resize
  chatInputEl.addEventListener('input', () => {
    chatInputEl.style.height = 'auto';
    chatInputEl.style.height = Math.min(chatInputEl.scrollHeight, 100) + 'px';
  });
}

async function sendChatMessage() {
  const text = chatInputEl.value.trim();
  if (!text || state.chat.streaming) return;
  chatInputEl.value = '';
  chatInputEl.style.height = 'auto';

  addChatMessage('user', text);
  state.chat.streaming = true;
  updateChatUI();

  try {
    const res = await apiFetch('/command/stream', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ text, lang: 'en', client_id: 'cc' })
    });
    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '', sawFinal = false, typingEl = null;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      buffer = lines.pop() || '';
      for (const line of lines) {
        if (!line.startsWith('data: ')) continue;
        try {
          const data = JSON.parse(line.slice(6));
          handleChatStream(data);
        } catch (e) {}
      }
    }
  } catch (e) {
    addChatMessage('assistant', 'Connection error. Check server.');
  } finally {
    state.chat.streaming = false;
    updateChatUI();
  }
}

function handleChatStream(data) {
  switch (data.type) {
    case 'run_id': state.chat.runId = data.run_id; break;
    case 'thought': showTyping(data.content || data.thought, data.worker || data.name); break;
    case 'action': showToolAction(data.content || data.action, data.worker || data.name); break;
    case 'result': break;
    case 'final':
      removeTyping();
      addChatMessage('assistant', data.content || data.reply || '', data.worker || data.name);
      break;
    case 'audio': playAudio(data.audio); break;
    case 'error': removeTyping(); addChatMessage('assistant', 'Error: ' + data.message); break;
    case 'cancelled': removeTyping(); addChatMessage('assistant', 'Cancelled.'); break;
  }
}

function handleChatFinal(data) { removeTyping(); addChatMessage('assistant', data.content || data.reply || '', data.worker || data.name); }
function handleChatError(data) { removeTyping(); addChatMessage('assistant', 'Error: ' + data.message); }

function addChatMessage(role, text, workerName) {
  const el = document.createElement('div');
  el.className = 'msg ' + role;
  const prefix = workerName && role === 'assistant' ? `<span class="worker-tag">${workerName}</span> ` : '';
  el.innerHTML = prefix + escapeHtml(text);
  $('#chat-messages').appendChild(el);
  scrollChat();
}

function showTyping(text, workerName) {
  removeTyping();
  const el = document.createElement('div');
  el.className = 'msg assistant typing';
  el.id = 'typing-indicator';
  const prefix = workerName ? `<span class="worker-tag">${workerName}</span> ` : '';
  el.innerHTML = prefix + escapeHtml(text);
  $('#chat-messages').appendChild(el);
  scrollChat();
}

function showToolAction(action, workerName) {
  removeTyping();
  const name = typeof action === 'string' ? action : (action?.function?.name || 'tool');
  addChatMessage('assistant', `Using ${name}...`, workerName);
}

function removeTyping() { const el = $('#typing-indicator'); if (el) el.remove(); }

function updateChatUI() {
  $('#chat-cancel').style.display = state.chat.streaming ? '' : 'none';
  $('#chat-send').style.display = state.chat.streaming ? 'none' : '';
}

function scrollChat() { const c() { const c = $('#chat-messages'); c.scrollTop = c.scrollHeight; }

function escapeHtml(s) { return s.replace(/&/g,'&').replace(/</g,'<').replace(/>/g,'>'); }

function playAudio(b64) { try { new Audio('data:audio/mp3;base64,' + b64).play(); } catch(e) {} }

// ── Voice Input ──
async function startVoiceInput() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) { showToast('Speech recognition not supported', 'warning'); return; }
  const rec = new SR();
  rec.lang = 'en-US';
  rec.interimResults = false;
  $('#chat-mic').classList.add('recording');
  try {
    const text = await new Promise((resolve, reject) => {
      rec.onresult = e => resolve(e.results[0][0].transcript.trim());
      rec.onerror = e => reject(e.error);
      rec.start();
      setTimeout(() => { try { rec.stop(); } catch(e) {} }, 8000);
    });
    if (text) { chatInputEl.value = text; sendChatMessage(); }
  } catch (e) { if (e !== 'no-speech') showToast('Voice error: ' + e, 'error'); }
  finally { $('#chat-mic').classList.remove('recording'); }
}

// ── Logs ──
function appendLog(msg) {
  state.logs.unshift(msg);
  if (state.logs.length > 500) state.logs.pop();
  if (state.currentView === 'logs') renderLogs(state.logs);
}

$('#log-level').onchange = () => renderLogs(state.logs);
$('#log-source').onchange = () => renderLogs(state.logs);
$('#btn-clear-logs').onclick = () => { state.logs = []; renderLogs([]); };

// ── Settings ──
const settingsInputs = $$('#view-settings input, #view-settings select');
settingsInputs.forEach(el => {
  el.addEventListener('change', () => saveSettings());
});

async function saveSettings() {
  const cfg = {};
  settingsInputs.forEach(el => {
    if (el.type === 'checkbox') cfg[el.id.replace('set-', '').replace(/-/g, '_')] = el.checked;
    else cfg[el.id.replace('set-', '').replace(/-/g, '_')] = el.value;
  });
  try {
    await apiJson('/config', { method: 'POST', body: JSON.stringify(cfg) });
    showToast('Settings saved', 'success');
  } catch (e) { showToast('Save failed: ' + e.message, 'error'); }
}

$('#btn-reset-data').onclick = async () => {
  showConfirm('Reset All Data', 'This will clear memory, logs, episodes, and earnings history.', '', async () => {
    await apiJson('/admin/reset', { method: 'POST' });
    showToast('All data reset', 'success');
    loadDashboard();
  });
};

$('#btn-factory-reset').onclick = async () => {
  showConfirm('Factory Reset', 'Removes ALL settings, API keys, bots, and data. Cannot be undone.', '', async () => {
    await apiJson('/admin/factory-reset', { method: 'POST' });
    showToast('Factory reset complete. Reloading...', 'warning');
    setTimeout(() => location.reload(), 1500);
  });
};

// ── Init ──
function init() {
  // Navigation
  $$('.nav-item[data-view]').forEach(btn => btn.onclick = () => switchView(btn.dataset.view));
  $('#kill-switch').onclick = showKillSwitch;

  // WebSocket
  connectWS();

  // Chat
  initChat();

  // Logs filters
  $('#log-level').onchange = () => renderLogs(state.logs);
  $('#log-source').onchange = () => renderLogs(state.logs);

  // Settings
  settingsInputs.forEach(el => el.addEventListener('change', saveSettings));

  // MT5
  $('#btn-mt5-connect').onclick = connectMT5;
  $('#btn-mt5-disconnect').onclick = disconnectMT5;

  // Bot dialog
  $('#bot-submit').onclick = createBot;

  // Device buttons
  $('#btn-android-connect').onclick = connectAndroid;
  $('#btn-android-test').onclick = testAndroidVoice;
  $('#btn-ios-connect').onclick = connectIOS;
  $('#btn-ios-test').onclick = testIOSVoice;

  // Initial load
  loadDashboard();
  loadTrading();
  loadWorkers();
  loadMT5();
  loadLogs();
  loadDevices();

  // Polling fallback
  setInterval(loadDashboard, 30000);
  setInterval(loadTrading, 10000);
  setInterval(loadWorkers, 15000);
  setInterval(loadBots, 20000);
  setInterval(loadMT5, 30000);
  setInterval(loadLogs, 5000);
  setInterval(loadDevices, 15000);

  console.log('[CC] Control Center initialized');
}

if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
else init();

// Expose device functions globally for onclick handlers
window.connectAndroid = connectAndroid;
window.testAndroidVoice = testAndroidVoice;
window.connectIOS = connectIOS;
window.testIOSVoice = testIOSVoice;
window.stopBot = stopBot;
window.deleteBot = deleteBot;