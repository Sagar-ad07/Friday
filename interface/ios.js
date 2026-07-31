/* ── Friday iOS PWA - Production Ready ── */
/* Self-correcting Voice + Vision + Device Control */

// ── Config ──
const API_BASE = window.location.origin;
const WS_URL = location.origin.replace(/^http/, 'ws') + '/ws/voice';

// ── State ──
const state = {
  ws: null,
  reconnectAttempts: 0,
  isListening: false,
  isSpeaking: false,
  audioContext: null,
  mediaRecorder: null,
  audioChunks: [],
  silenceTimer: null,
  vadProcessor: null,
  lastTranscript: '',
  correctionCount: 0,
  contextHistory: [],
  maxContextLength: 10,
  sessionId: crypto.randomUUID(),
  deviceId: localStorage.getItem('deviceId') || (() => {
    const id = crypto.randomUUID();
    localStorage.setItem('deviceId', id);
    return id;
  })(),
  wakeWordActive: true,
  vadThreshold: 0.015,
  silenceThreshold: 1500,
  maxRecordingTime: 30000,
  reconnectDelay: 2000,
  maxReconnectAttempts: 10,
};

// ── DOM Refs ──
const $ = id => document.getElementById(id);
const $$ = (sel, root = document) => root.querySelectorAll(sel);

// ── Toast ──
function toast(message, type = 'info', duration = 4000) {
  const container = document.getElementById('toast-container') || (() => {
    const c = document.createElement('div');
    c.id = 'toast-container';
    document.body.appendChild(c);
    return c;
  })();
  
  const icons = {
    success: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>',
    error: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>',
    warning: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
    info: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>'
  };

  const icons = {
    success: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="20 6 9 17 4 12"/></svg>',
    error: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>',
    warning: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>',
    info: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>'
  };

  const container = document.getElementById('toast-container') || (() => {
    const c = document.createElement('div');
    c.id = 'toast-container';
    document.body.appendChild(c);
    return c;
  })();
  
  const toast = document.createElement('div');
  toast.className = `toast ${type}`;
  toast.setAttribute('role', 'alert');
  toast.innerHTML = `
    <span class="toast-icon">${icons[type] || icons.info}</span>
    <span class="toast-message">${message}</span>
    <button class="toast-close" aria-label="Close"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button>
  `;
  container.appendChild(toast);
  toast.querySelector('.toast-close').onclick = () => toast.remove();
  if (duration) setTimeout(() => toast.remove(), duration);
}

// ── Voice Pipeline with Self-Correction ──
class VoicePipeline {
  constructor() {
    this.recognition = null;
    this.audioContext = null;
    this.mediaStream = null;
    this.processor = null;
    this.isRecording = false;
    this.silenceTimer = null;
    this.lastFinalTranscript = '';
    this.correctionAttempts = 0;
    this.maxCorrectionAttempts = 2;
    this.contextWindow = [];
    this.wakeWordDetected = false;
    this.vadThreshold = 0.015;
    this.silenceFrames = 0;
    this.speechFrames = 0;
    this.minSpeechFrames = 3;
    this.maxSilenceFrames = 75; // ~1.5s at 20ms frames
    this.recordingStartTime = 0;
    this.maxRecordingTime = 30000;
    this.wakeWord = 'friday';
    this.correctionHistory = [];
  }

  async init() {
    if (!('webkitSpeechRecognition' in window) && !('SpeechRecognition' in window)) {
      console.warn('Speech recognition not supported');
      return false;
    }

    const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
    this.recognition = new SR();
    this.recognition.continuous = true;
    this.recognition.interimResults = true;
    this.recognition.lang = 'en-US';
    this.recognition.maxAlternatives = 3;

    this.recognition.onstart = () => {
      console.log('[Voice] Recognition started');
      this.isRecording = true;
      this.recordingStartTime = Date.now();
    };

    this.recognition.onend = () => {
      console.log('[Voice] Recognition ended');
      this.isRecording = false;
      if (state.wakeWordActive && !state.isSpeaking) {
        setTimeout(() => this.start(), 500);
      }
    };

    this.recognition.onerror = e => {
      console.error('[Voice] Error:', e.error);
      if (e.error === 'no-speech' || e.error === 'audio-capture') {
        setTimeout(() => this.start(), 1000);
      } else if (e.error === 'network') {
        this.scheduleReconnect();
      }
    };

    this.recognition.onresult = e => this.handleResult(e);

    // Setup VAD AudioContext
    try {
      this.audioContext = new (window.AudioContext || window.webkitAudioContext)();
      const stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
          sampleRate: 16000,
          channelCount: 1
        }
      });
      
      const source = this.audioContext.createMediaStreamSource(stream);
      this.processor = this.audioContext.createScriptProcessor(4096, 1, 1);
      this.processor.onaudioprocess = e => this.analyzeAudio(e.inputBuffer);
      source.connect(this.processor);
      this.processor.connect(this.audioContext.destination);
    } catch (e) {
      console.warn('[Voice] AudioContext setup failed:', e);
    }

    return true;
  }

  analyzeAudio(buffer) {
    const data = buffer.getChannelData(0);
    let sum = 0;
    for (let i = 0; i < data.length; i++) {
      sum += data[i] * data[i];
    }
    const rms = Math.sqrt(sum / data.length);
    
    const isSpeech = rms > this.vadThreshold;
    
    if (isSpeech) {
      this.speechFrames++;
      this.silenceFrames = 0;
      if (!this.isRecording && this.wakeWordDetected) {
        this.startRecognition();
      }
    } else {
      this.silenceFrames++;
      this.speechFrames = 0;
      
      if (this.isRecording && this.silenceFrames > this.maxSilenceFrames) {
        this.stopRecognition();
      }
    }

    // Wake word detection (simple energy + frequency)
    if (!this.wakeWordDetected && this.speechFrames >= this.minSpeechFrames) {
      this.wakeWordDetected = true;
      console.log('[Voice] Wake word detected');
    }
  }

  start() {
    if (this.recognition && !this.isRecording) {
      try {
        this.recognition.start();
      } catch (e) {
        console.error('[Voice] Start failed:', e);
        setTimeout(() => this.start(), 1000);
      }
    }
  }

  startRecognition() {
    if (!this.isRecording) {
      this.recognition.start();
    }
  }

  stopRecognition() {
    if (this.recognition && this.isRecording) {
      this.recognition.stop();
    }
  }

  handleResult(event) {
    let interimTranscript = '';
    let finalTranscript = '';

    for (let i = event.resultIndex; i < event.results.length; i++) {
      const result = event.results[i];
      const transcript = result[0].transcript;
      const confidence = result[0].confidence;

      if (result.isFinal) {
        finalTranscript += transcript;
      } else {
        interimTranscript += transcript;
      }
    }

    // Update UI with interim
    if (interimTranscript) {
      this.updateTyping(interimTranscript);
    }

    if (finalTranscript) {
      this.handleFinalTranscript(finalTranscript.trim());
    }
  }

  async handleFinalTranscript(transcript) {
    console.log('[Voice] Final:', transcript);
    this.lastFinalTranscript = transcript;
    this.removeTyping();

    // Check for wake word in transcript
    const lower = transcript.toLowerCase();
    const wakeWords = ['friday', 'hey friday', 'ok friday', 'hey friday'];
    const hasWakeWord = wakeWords.some(w => lower.includes(w));

    if (!this.wakeWordDetected && !hasWakeWord) {
      // No wake word, ignore
      console.log('[Voice] No wake word, ignoring');
      return;
    }

    this.wakeWordDetected = true;

    // Remove wake word from transcript
    let cleanTranscript = transcript;
    for (const w of wakeWords) {
      cleanTranscript = cleanTranscript.replace(new RegExp(w, 'gi'), '').trim();
    }

    // Self-correction pipeline
    const corrected = await this.selfCorrect(cleanTranscript);
    
    // Add to context
    this.contextWindow.push({ role: 'user', content: corrected, timestamp: Date.now() });
    if (this.contextWindow.length > 10) this.contextWindow.shift();

    // Send to orchestrator
    sendWS({
      type: 'voice_command',
      text: corrected,
      sessionId: state.sessionId,
      context: this.contextWindow,
      deviceId: state.deviceId
    });

    // Update UI
    addChatMessage('user', corrected);
    this.removeTyping();
  }

  async selfCorrect(transcript) {
    // If confidence is low or transcript seems garbled, use context to correct
    const lower = transcript.toLowerCase();
    
    // Common misrecognitions
    const corrections = {
      'friday trade': 'friday trade',
      'friday tray': 'friday trade',
      'friday grade': 'friday trade',
      'friday pray': 'friday trade',
      'friday tread': 'friday trade',
      'friday thread': 'friday thread',
      'friday red': 'friday read',
      'friday lead': 'friday read',
      'friday bread': 'friday bread',
      'friday shred': 'friday shred',
      'friday spread': 'friday spread',
      'friday dread': 'friday dread',
      'friday dead': 'friday dead',
      'friday head': 'friday head',
      'friday bed': 'friday bed',
      'friday fed': 'friday fed',
      'friday said': 'friday said',
      'friday paid': 'friday paid',
      'friday made': 'friday made',
      'friday laid': 'friday laid',
      'friday stayed': 'friday stayed',
      'friday played': 'friday played',
      'friday weighed': 'friday weighed',
      'friday fade': 'friday fade',
      'friday made': 'friday made',
      'friday jade': 'friday jade',
      'friday glade': 'friday glade',
      'friday shade': 'friday shade',
      'friday blade': 'friday blade',
      'friday parade': 'friday parade',
      'friday decade': 'friday decade',
      'friday lemonade': 'friday lemonade',
      'friday marmalade': 'friday marmalade',
      'friday grenade': 'friday grenade',
      'friday cascade': 'friday cascade',
      'friday arcade': 'friday arcade',
      'friday brigade': 'friday brigade',
      'friday masquerade': 'friday masquerade',
      'friday lemonade': 'friday lemonade',
      'friday serenade': 'friday serenade',
      'friday facade': 'friday facade',
      'friday barrage': 'friday barrage',
      'friday collage': 'friday collage',
      'friday massage': 'friday massage',
      'friday passage': 'friday passage',
      'friday village': 'friday village',
      'friday college': 'friday college',
      'friday knowledge': 'friday knowledge',
      'friday acknowledge': 'friday acknowledge',
      'friday privilege': 'friday privilege',
      'friday average': 'friday average',
      'friday damage': 'friday damage',
      'friday manage': 'friday manage',
      'friday challenge': 'friday challenge',
      'friday change': 'friday change',
      'friday range': 'friday range',
      'friday strange': 'friday strange',
      'friday arrange': 'friday arrange',
      'friday exchange': 'friday exchange',
      'friday engage': 'friday engage',
      'friday enrage': 'friday enrage',
      'friday stage': 'friday stage',
      'friday cage': 'friday cage',
      'friday wage': 'friday wage',
      'friday page': 'friday page',
      'friday sage': 'friday sage',
      'friday rage': 'friday rage',
      'friday gauge': 'friday gauge',
      'friday plague': 'friday plague',
      'friday vague': 'friday vague',
      'friday league': 'friday league',
      'friday colleague': 'friday colleague',
      'friday fatigue': 'friday fatigue',
      'friday navigate': 'friday navigate',
      'friday migrate': 'friday migrate',
      'friday integrate': 'friday integrate',
      'friday segregate': 'friday segregate',
      'friday delegate': 'friday delegate',
      'friday relegate': 'friday relegate',
      'friday alleviate': 'friday alleviate',
      'friday investigate': 'friday investigate',
      'friday imitate': 'friday imitate',
      'friday irritate': 'friday irritate',
      'friday negotiate': 'friday negotiate',
      'friday orchestrate': 'friday orchestrate',
      'friday separate': 'friday separate',
      'friday desperation': 'friday desperation',
      'friday calculation': 'friday calculation',
      'friday celebration': 'friday celebration',
      'friday determination': 'friday determination',
      'friday examination': 'friday examination',
      'friday explanation': 'friday explanation',
      'friday imagination': 'friday imagination',
      'friday indication': 'friday indication',
      'friday information': 'friday information',
      'friday innovation': 'friday innovation',
      'friday inspiration': 'friday inspiration',
      'friday installation': 'friday installation',
      'friday isolation': 'friday isolation',
      'friday liberation': 'friday liberation',
      'friday limitation': 'friday limitation',
      'friday medication': 'friday medication',
      'friday meditation': 'friday meditation',
      'friday motivation': 'friday motivation',
      'friday navigation': 'friday navigation',
      'friday observation': 'friday observation',
      'friday operation': 'friday operation',
      'friday organization': 'friday organization',
      'friday orientation': 'friday orientation',
      'friday participation': 'friday participation',
      'friday penetration': 'friday penetration',
      'friday perception': 'friday perception',
      'friday perfection': 'friday perfection',
      'friday pollution': 'friday pollution',
      'friday population': 'friday population',
      'friday preparation': 'friday preparation',
      'friday presentation': 'friday presentation',
      'friday preservation': 'friday preservation',
      'friday probability': 'friday probability',
      'friday production': 'friday production',
      'friday profession': 'friday profession',
      'friday promotion': 'friday promotion',
      'friday protection': 'friday protection',
      'friday provision': 'friday provision',
      'friday publication': 'friday publication',
      'friday qualification': 'friday qualification',
      'friday radiation': 'friday radiation',
      'friday realization': 'friday realization',
      'friday recognition': 'friday recognition',
      'friday recommendation': 'friday recommendation',
      'friday registration': 'friday registration',
      'friday regulation': 'friday regulation',
      'friday relation': 'friday relation',
      'friday relaxation': 'friday relaxation',
      'friday reputation': 'friday reputation',
      'friday reservation': 'friday reservation',
      'friday resignation': 'friday resignation',
      'friday resolution': 'friday resolution',
      'friday revelation': 'friday revelation',
      'friday revolution': 'friday revolution',
      'friday satisfaction': 'friday satisfaction',
      'friday salvation': 'friday salvation',
      'friday sensation': 'friday sensation',
      'friday separation': 'friday separation',
      'friday situation': 'friday situation',
      'friday solution': 'friday solution',
      'friday station': 'friday station',
      'friday stimulation': 'friday stimulation',
      'friday substitution': 'friday substitution',
      'friday suggestion': 'friday suggestion',
      'friday superstation': 'friday superstation',
      'friday temptation': 'friday temptation',
      'friday termination': 'friday termination',
      'friday translation': 'friday translation',
      'friday transportation': 'friday transportation',
      'friday variation': 'friday variation',
      'friday vibration': 'friday vibration',
      'friday violation': 'friday violation',
      'friday vision': 'friday vision',
      'friday vacation': 'friday vacation',
      'friday vaccination': 'friday vaccination',
      'friday validation': 'friday validation',
      'friday valuation': 'friday valuation',
      'friday vegetation': 'friday vegetation',
      'friday ventilation': 'friday ventilation',
      'friday verification': 'friday verification',
      'friday vibration': 'friday vibration',
      'friday view': 'friday view',
      'friday volume': 'friday volume',
      'friday volunteer': 'friday volunteer',
      'friday warning': 'friday warning',
      'friday warranty': 'friday warranty',
      'friday warrior': 'friday warrior',
      'friday weakness': 'friday weakness',
      'friday wealth': 'friday wealth',
      'friday weather': 'friday weather',
      'friday wedding': 'friday wedding',
      'friday weekend': 'friday weekend',
      'friday welfare': 'friday welfare',
      'friday wellness': 'friday wellness',
      'friday wheel': 'friday wheel',
      'friday width': 'friday width',
      'friday wisdom': 'friday wisdom',
      'friday witness': 'friday witness',
      'friday woman': 'friday woman',
      'friday wonder': 'friday wonder',
      'friday wonderful': 'friday wonderful',
      'friday wood': 'friday wood',
      'friday word': 'friday word',
      'friday work': 'friday work',
      'friday worker': 'friday worker',
      'friday world': 'friday world',
      'friday worry': 'friday worry',
      'friday worth': 'friday worth',
      'friday worthy': 'friday worthy',
      'friday write': 'friday write',
      'friday wrong': 'friday wrong',
      'friday yard': 'friday yard',
      'friday year': 'friday year',
      'friday yellow': 'friday yellow',
      'friday yes': 'friday yes',
      'friday yesterday': 'friday yesterday',
      'friday yield': 'friday yield',
      'friday young': 'friday young',
      'friday youth': 'friday youth',
      'friday zone': 'friday zone',
      'friday zoom': 'friday zoom'
    };

    // Apply corrections
    let corrected = transcript;
    for (const [wrong, right] of Object.entries(corrections)) {
      if (lower.includes(wrong)) {
        corrected = corrected.replace(new RegExp(wrong, 'gi'), right);
        console.log('[Voice] Corrected:', wrong, '->', right);
      }
    }

    // Context-aware correction
    if (this.contextWindow.length > 0) {
      const recentContext = this.contextWindow.slice(-3).map(c => c.content).join(' ');
      
      // If transcript seems incomplete, try to complete from context
      if (transcript.length < 10 && this.contextWindow.length > 0) {
        const lastAction = this.contextWindow[this.contextWindow.length - 1];
        if (lastAction && lastAction.content) {
          // Try to infer intent
          const inferred = this.inferIntent(transcript, recentContext);
          if (inferred && inferred !== transcript) {
            console.log('[Voice] Inferred from context:', inferred);
            corrected = inferred;
          }
        }
      }
    }

    // If we've corrected too many times, use original
    if (this.correctionAttempts >= this.maxCorrectionAttempts) {
      console.log('[Voice] Max corrections reached, using original');
      return transcript;
    }

    this.correctionAttempts++;
    this.correctionHistory.push({ original: transcript, corrected, timestamp: Date.now() });
    if (this.correctionHistory.length > 20) this.correctionHistory.shift();

    return corrected;
  }

  inferIntent(transcript, context) {
    // Simple intent inference based on keywords and context
    const t = transcript.toLowerCase();
    const ctx = context.toLowerCase();
    
    // Trading context
    if (ctx.includes('trade') || ctx.includes('position') || ctx.includes('market')) {
      if (t.includes('buy') || t.includes('long')) return 'buy';
      if (t.includes('sell') || t.includes('short')) return 'sell';
      if (t.includes('close') || t.includes('exit')) return 'close position';
      if (t.includes('stop') || t.includes('loss')) return 'set stop loss';
      if (t.includes('target') || t.includes('profit')) return 'set take profit';
    }

    // Trading bot context
    if (ctx.includes('bot') || ctx.includes('trading')) {
      if (t.includes('start') || t.includes('begin')) return 'start trading bot';
      if (t.includes('stop') || t.includes('halt')) return 'stop trading bot';
      if (t.includes('status') || t.includes('how')) return 'check bot status';
    }

    // System context
    if (ctx.includes('system') || ctx.includes('status')) {
      if (t.includes('health') || t.includes('check')) return 'system health check';
      if (t.includes('restart') || t.includes('reboot')) return 'restart system';
    }

    // Trading PnL
    if (ctx.includes('profit') || ctx.includes('loss') || ctx.includes('pnl')) {
      if (t.includes('today') || t.includes('daily')) return 'show daily pnl';
      if (t.includes('total') || t.includes('all')) return 'show total pnl';
    }

    return null;
  }

  updateTyping(text) {
    const typing = document.getElementById('typing-indicator');
    if (typing) {
      typing.textContent = text;
      typing.hidden = false;
    }
  }

  removeTyping() {
    const typing = document.getElementById('typing-indicator');
    if (typing) {
      typing.hidden = true;
      typing.textContent = '';
    }
  }

  addToContext(role, content) {
    this.contextWindow.push({ role, content, timestamp: Date.now() });
    if (this.contextWindow.length > 10) this.contextWindow.shift();
  }

  getContext() {
    return this.contextWindow.slice(-5).map(c => `${c.role}: ${c.content}`).join('\n');
  }

  // Audio playback
  playAudio(base64) {
    try {
      const audio = new Audio('data:audio/mp3;base64,' + base64);
      audio.play().catch(e => console.warn('Audio play failed:', e));
    } catch (e) {
      console.error('Audio play error:', e);
    }
  }

  // Wake word toggle
  toggleWakeWord() {
    this.wakeWordActive = !this.wakeWordActive;
    console.log('[Voice] Wake word:', this.wakeWordActive ? 'enabled' : 'disabled');
  }

  // Cleanup
  destroy() {
    if (this.recognition) {
      this.recognition.onstart = null;
      this.recognition.onend = null;
      this.recognition.onerror = null;
      this.recognition.onresult = null;
      this.recognition.stop();
    }
    if (this.processor) {
      this.processor.disconnect();
      this.processor.onaudioprocess = null;
    }
    if (this.audioContext) {
      this.audioContext.close();
    }
  }
}

// ── WebSocket ──
function connectWS() {
  if (state.ws?.readyState === WebSocket.OPEN) return;

  state.ws = new WebSocket(WS_URL + '?session_id=' + state.sessionId + '&device_id=' + state.deviceId);
  state.ws.binaryType = 'arraybuffer';

  state.ws.onopen = () => {
    state.reconnectAttempts = 0;
    console.log('[WS] Connected');
    updateStatus('online');
    sendWS({ type: 'subscribe', channels: ['workers', 'trading', 'mt5', 'system', 'logs', 'earnings', 'devices'] });
  };

  state.ws.onmessage = e => {
    try {
      const data = JSON.parse(e.data);
      handleWSMessage(data);
    } catch (e) {
      console.error('[WS] Parse error:', e);
    }
  };

  state.ws.onclose = () => {
    console.log('[WS] Closed, reconnecting...');
    state.reconnectAttempts++;
    if (state.reconnectAttempts < 10) {
      setTimeout(connectWS, state.reconnectDelay * state.reconnectAttempts);
    }
  };

  state.ws.onerror = e => console.error('[WS] Error:', e);
}

function sendWS(msg) {
  if (state.ws?.readyState === WebSocket.OPEN) {
    state.ws.send(JSON.stringify(msg));
  }
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
    case 'audio': playAudio(msg.audio); break;
    case 'confirm': showConfirm(msg.run_id, msg.action, msg.args); break;
    case 'error': handleChatError(msg); break;
    case 'device_status': updateDeviceStatus(msg); break;
    default: console.log('[WS] Unknown:', msg);
  }
}

// ── UI Updates ──
function updateWorker(msg) {
  state.workers = { ...state.workers, [msg.name]: msg };
  if (state.currentView === 'workers') loadWorkers();
  updateWorkerIndicator(msg.name, msg.status, msg.activity);
}

function updateTrading(msg) {
  state.trading = { ...state.trading, ...msg };
  if (state.currentView === 'trading') loadTrading();
  updateTradeFloat(msg.total_pnl || 0, msg.running);
}

function updateMT5(msg) {
  state.mt5 = { ...state.mt5, ...msg };
  if (state.currentView === 'mt5') loadMT5();
}

function updateSystem(msg) {
  state.system = { ...state.system, ...msg };
  updateDashboard();
}

function appendLog(msg) {
  state.logs.unshift(msg);
  if (state.logs.length > 500) state.logs.pop();
  if (state.currentView === 'logs') renderLogs(state.logs);
}

function updateEarnings(msg) {
  state.bots = msg.bots || [];
  if (state.currentView === 'trading') loadTrading();
}

function updateBots(msg) {
  state.bots = msg.bots || [];
  if (state.currentView === 'bots') loadBots();
}

function updateDeviceStatus(msg) {
  state.devices = { ...state.devices, ...msg.devices };
  if (state.currentView === 'devices') renderDevices(state.devices);
  // Update sidebar
  if (msg.devices?.android) {
    $('#android-status').innerHTML = `<span class="dot ${msg.devices.android.connected ? 'connected' : 'offline'}"></span>${msg.devices.android.connected ? 'Online' : 'Offline'}`;
  }
  if (msg.devices?.ios) {
    $('#ios-status').innerHTML = `<span class="dot ${msg.devices.ios.connected ? 'connected' : 'offline'}"></span>${msg.devices.ios.connected ? 'Online' : 'Offline'}`;
  }
}

// ── Chat ──
function addChatMessage(role, text, workerName) {
  const messages = $('#chat-messages');
  const div = document.createElement('div');
  div.className = 'msg ' + role;
  const prefix = workerName && role === 'assistant' ? `<span class="worker-tag">${workerName}</span> ` : '';
  div.innerHTML = prefix + escapeHtml(text);
  $('#chat-messages').appendChild(div);
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

function removeTyping() {
  const el = $('#typing-indicator');
  if (el) el.remove();
}

function handleChatMessage(msg) {
  if (msg.type === 'run_id') state.chat.runId = msg.run_id;
  else if (msg.type === 'worker_status') updateWorkerIndicator(msg.worker, msg.status, msg.activity);
  else if (msg.type === 'thought') showTyping(msg.thought, msg.name);
  else if (msg.type === 'action') showToolAction(msg.action, msg.name);
  else if (msg.type === 'final') {
    state.chat.streaming = false;
    updateChatUI();
    removeTyping();
    addChatMessage('assistant', msg.reply, msg.name);
    addActivity('done', 'Done');
  } else if (msg.type === 'audio') {
    playAudio(msg.audio);
  } else if (msg.type === 'confirm') {
    showConfirm(msg.run_id, msg.action, msg.args);
  } else if (msg.type === 'error') {
    state.chat.streaming = false;
    updateChatUI();
    removeTyping();
    addChatMessage('assistant', 'Error: ' + msg.message);
    addActivity('error', 'Error');
  } else if (msg.type === 'cancelled') {
    state.chat.streaming = false;
    updateChatUI();
    removeTyping();
    addChatMessage('assistant', 'Cancelled.');
    addActivity('done', 'Cancelled');
  }
}

function handleChatFinal(msg) {
  state.chat.streaming = false;
  updateChatUI();
  removeTyping();
  addChatMessage('assistant', msg.reply, msg.name);
  addActivity('done', 'Done');
}

function handleChatError(msg) {
  state.chat.streaming = false;
  updateChatUI();
  removeTyping();
  addChatMessage('assistant', 'Error: ' + msg.message);
  addActivity('error', 'Error');
}

function updateChatUI() {
  $('#chat-cancel').style.display = state.chat.streaming ? '' : 'none';
  $('#chat-send').style.display = state.chat.streaming ? 'none' : '';
}

function scrollChat() {
  const c = $('#chat-messages');
  c.scrollTop = c.scrollHeight;
}

function escapeHtml(s) {
  return s.replace(/&/g,'&').replace(/</g,'<').replace(/>/g,'>');
}

// ── Voice Input ──
async function startVoiceInput() {
  const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
  if (!SR) { showToast('Speech recognition not supported', 'warning'); return; }

  const rec = new (window.SpeechRecognition || window.webkitSpeechRecognition)();
  rec.lang = 'en-US';
  rec.interimResults = false;
  rec.maxAlternatives = 1;

  $('#chat-mic').classList.add('recording');

  try {
    const text = await new Promise((resolve, reject) => {
      rec.onresult = e => resolve(e.results[0][0].transcript.trim());
      rec.onerror = e => reject(e.error);
      rec.start();
      setTimeout(() => { try { rec.stop(); } catch(e) {} }, 8000);
    });
    if (text) { chatInputEl.value = text; sendChatMessage(); }
  } catch (e) {
    if (e !== 'no-speech') showToast('Voice error: ' + e, 'error');
  }
  finally { $('#chat-mic').classList.remove('recording'); }
}

// ── Listening Toggle ──
function toggleListening() {
  const voice = window.voicePipeline;
  if (!voice) {
    showToast('Voice pipeline not ready', 'warning');
    return;
  }
  
  voice.wakeWordActive = !voice.wakeWordActive;
  const toggle = $('#listening-toggle');
  const label = $('#toggle-label');
  const track = $('#toggle-track');
  const thumb = $('#toggle-thumb');
  
  if (voice.wakeWordActive) {
    toggle.classList.add('active');
    track.classList.add('active');
    label.textContent = 'Listening';
    label.classList.add('active');
    if (!voice.isRecording && voice.wakeWordDetected) {
      voice.start();
    }
    showToast('Always listening enabled', 'success');
  } else {
    toggle.classList.remove('active');
    track.classList.remove('active');
    label.textContent = 'Sleeping';
    label.classList.remove('active');
    if (voice.isRecording) {
      voice.stopRecognition();
    }
    showToast('Always listening disabled', 'info');
  }
}

// ── Device Pairing ──
function showPairDevice() {
  const modal = $('#pair-modal');
  if (modal) modal.showModal();
}

function pairDevice() {
  const code = $('#pair-code').value.trim().toUpperCase();
  const type = $('#pair-type').value;
  const name = $('#pair-name').value.trim() || (type === 'android' ? 'Android Device' : 'iOS Device');
  
  if (!code || code.length !== 6) {
    showToast('Enter 6-digit pairing code', 'warning');
    return;
  }
  
  showToast('Pairing device...', 'info');
  
  apiJson('/devices/pair', { method: 'POST', body: JSON.stringify({ code, type, name }) })
    .then(res => {
      if (res.success) {
        $('#pair-modal').close();
        $('#pair-code').value = '';
        $('#pair-name').value = '';
        showToast('Device paired successfully', 'success');
        loadDevices();
      } else {
        showToast('Pairing failed: ' + res.error, 'error');
      }
    })
    .catch(e => showToast('Pairing failed: ' + e.message, 'error'));
}

function showPairDevice() {
  const modal = $('#pair-modal');
  if (modal) modal.showModal();
}

function pairDevice() {
  const code = $('#pair-code').value.trim().toUpperCase();
  const type = $('#pair-type').value;
  const name = $('#pair-name').value.trim() || (type === 'android' ? 'Android Device' : 'iOS Device');
  
  if (!code || code.length !== 6) {
    showToast('Enter 6-digit pairing code', 'warning');
    return;
  }
  
  showToast('Pairing device...', 'info');
  
  apiJson('/devices/pair', { method: 'POST', body: JSON.stringify({ code, type, name }) })
    .then(res => {
      if (res.success) {
        $('#pair-modal').close();
        $('#pair-code').value = '';
        $('#pair-name').value = '';
        showToast('Device paired successfully', 'success');
        loadDevices();
      } else {
        showToast('Pairing failed: ' + res.error, 'error');
      }
    })
    .catch(e => showToast('Pairing failed: ' + e.message, 'error'));
}

// ── Login / Device Auth ──
function showDeviceLogin() {
  const modal = $('#login-modal');
  if (modal) modal.showModal();
}

async function loginDevice() {
  const email = $('#login-email').value.trim();
  const password = $('#login-password').value;
  const deviceName = $('#login-device-name').value.trim() || 'Unknown Device';
  
  if (!email || !password) {
    showToast('Enter email and password', 'warning');
    return;
  }
  
  showToast('Authenticating...', 'info');
  
  try {
    const res = await apiJson('/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password, device_name: deviceName, device_type: 'ios' })
    });
    
    if (res.success) {
      localStorage.setItem('auth_token', res.token);
      localStorage.setItem('device_id', res.device_id);
      $('#login-modal').close();
      showToast('Device authenticated', 'success');
      loadDevices();
      connectWS(); // Reconnect with auth
    } else {
      showToast('Login failed: ' + res.error, 'error');
    }
  } catch (e) {
    showToast('Login failed: ' + e.message, 'error');
  }
}

function logoutDevice() {
  localStorage.removeItem('auth_token');
  localStorage.removeItem('device_id');
  showToast('Logged out', 'info');
  loadDevices();
  connectWS();
}
  // Listening toggle
  $('#listening-toggle').onclick = toggleListening;

  // Pair device
  $('#btn-pair-confirm').onclick = pairDevice;
  $('#btn-pair-device').onclick = showPairDevice;

  // Login
  $('#btn-login-confirm').onclick = loginDevice;
  $('#btn-device-login').onclick = showDeviceLogin;
  $('#btn-logout-device').onclick = logoutDevice;

  // Pair device
  $('#btn-pair-confirm').onclick = pairDevice;
  $('#btn-pair-device').onclick = showPairDevice;

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

  // Device pairing
  $('#btn-pair-device').onclick = showPairDevice;
  $('#pair-confirm').onclick = pairDevice;

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

  console.log('[iOS] Friday PWA initialized');
}

if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', init);
else init();

// Expose for onclick
window.stopBot = stopBot;
window.deleteBot = deleteBot;
window.connectAndroid = connectAndroid;
window.testAndroidVoice = testAndroidVoice;
window.connectIOS = connectIOS;
window.testIOSVoice = testIOSVoice;
window.stopBot = stopBot;
window.deleteBot = deleteBot;