/* ── Friday Image Editor ── */
/* Professional-grade image editor built into Friday */
/* Works on iOS PWA, Android, Desktop */

const ImageEditor = (function() {
  'use strict';

  // ── State ──
  let canvas = null;
  let ctx = null;
  let fabric = null;
  let currentImage = null;
  let history = [];
  let historyIndex = -1;
  let isEditing = false;
  let currentTool = 'select';
  let zoom = 1;
  let pan = { x: 0, y: 0 };

  // ── Tools ──
  const tools = {
    select: { name: 'Select', icon: '🖱️', cursor: 'default' },
    crop: { name: 'Crop', icon: '✂️', cursor: 'crosshair' },
    brush: { name: 'Brush', icon: '🖌️', cursor: 'crosshair' },
    eraser: { name: 'Eraser', icon: '🧽', cursor: 'crosshair' },
    text: { name: 'Text', icon: '📝', cursor: 'text' },
    shapes: { name: 'Shapes', icon: '🔷', cursor: 'crosshair' },
    filter: { name: 'Filters', icon: '🎨', cursor: 'default' },
    adjust: { name: 'Adjust', icon: '📊', cursor: 'default' },
    ai: { name: 'AI Magic', icon: '✨', cursor: 'default' },
    removeBg: { name: 'Remove BG', icon: '🪄', cursor: 'default' },
    upscale: { name: 'Upscale', icon: '🔍', cursor: 'default' },
    inpaint: { name: 'Inpaint', icon: '🖌️', cursor: 'crosshair' },
    outpaint: { name: 'Outpaint', icon: '🖼️', cursor: 'crosshair' },
  };

  // ── Adjustment Presets ──
  const presets = {
    auto: { name: 'Auto Enhance', filters: { brightness: 10, contrast: 15, saturation: 10, sharpness: 10 } },
    vivid: { name: 'Vivid', filters: { saturation: 30, contrast: 20, vibrance: 20 } },
    warm: { name: 'Warm', filters: { temperature: 30, tint: 10 } },
    cool: { name: 'Cool', filters: { temperature: -30, tint: -10 } },
    bw: { name: 'Black & White', filters: { saturation: -100, contrast: 20 } },
    vintage: { name: 'Vintage', filters: { sepia: 40, contrast: 10, vignette: 30 } },
    cinematic: { name: 'Cinematic', filters: { contrast: 25, saturation: -10, letterbox: true } },
    hdr: { name: 'HDR', filters: { clarity: 30, highlights: -20, shadows: 20 } },
  };

  // ── Filter Definitions ──
  const filters = {
    brightness: { name: 'Brightness', min: -100, max: 100, default: 0, unit: '' },
    contrast: { name: 'Contrast', min: -100, max: 100, default: 0, unit: '' },
    saturation: { name: 'Saturation', min: -100, max: 100, default: 0, unit: '' },
    vibrance: { name: 'Vibrance', min: -100, max: 100, default: 0, unit: '' },
    temperature: { name: 'Temperature', min: -100, max: 100, default: 0, unit: 'K' },
    tint: { name: 'Tint', min: -100, max: 100, default: 0, unit: '' },
    highlights: { name: 'Highlights', min: -100, max: 100, default: 0, unit: '' },
    shadows: { name: 'Shadows', min: -100, max: 100, default: 0, unit: '' },
    whites: { name: 'Whites', min: -100, max: 100, default: 0, unit: '' },
    blacks: { name: 'Blacks', min: -100, max: 100, default: 0, unit: '' },
    clarity: { name: 'Clarity', min: -100, max: 100, default: 0, unit: '' },
    sharpness: { name: 'Sharpness', min: 0, max: 100, default: 0, unit: '' },
    vignette: { name: 'Vignette', min: 0, max: 100, default: 0, unit: '' },
    grain: { name: 'Grain', min: 0, max: 100, default: 0, unit: '' },
    sepia: { name: 'Sepia', min: 0, max: 100, default: 0, unit: '' },
    blur: { name: 'Blur', min: 0, max: 100, default: 0, unit: 'px' },
  };

  // ── Init ──
  function init(containerId) {
    const container = document.getElementById(containerId);
    if (!container) return;

    container.innerHTML = `
      <div class="editor-container">
        <header class="editor-header">
          <div class="header-left">
            <h1>Friday Editor</h1>
            <span class="badge" id="img-info"></span>
          </div>
          <div class="header-center" id="tool-toolbar"></div>
          <div class="header-right">
            <button class="icon-btn" id="btn-undo" title="Undo (⌘Z)" aria-label="Undo">↶</button>
            <button class="icon-btn" id="btn-redo" title="Redo (⌘⇧Z)" aria-label="Redo">↷</button>
            <div class="divider"></div>
            <button class="icon-btn" id="btn-zoom-out" title="Zoom Out">🔍⁻</button>
            <span id="zoom-level">100%</span>
            <button class="icon-btn" id="btn-zoom-in" title="Zoom In">🔍⁺</button>
            <div class="divider"></div>
            <button class="icon-btn primary" id="btn-export" title="Export">⬇</button>
            <button class="icon-btn" id="btn-close" title="Close">✕</button>
          </div>
        </header>

        <main class="editor-main">
          <aside class="sidebar-tools" id="tools-sidebar">
            <div class="tool-group">
              <h3>Tools</h3>
              <div class="tool-list" id="tools-list"></div>
            </div>
            <div class="tool-group">
              <h3>AI Magic</h3>
              <div class="tool-list" id="ai-tools-list"></div>
            </div>
          </aside>

          <div class="canvas-wrapper" id="canvas-wrapper">
            <canvas id="editor-canvas"></canvas>
            <div class="crop-overlay" id="crop-overlay" hidden></div>
          </main>

          <aside class="sidebar-panels" id="panels-sidebar">
            <div class="panel-tabs" id="panel-tabs"></div>
            <div class="panel-content" id="panel-content"></div>
          </aside>
        </main>

        <footer class="editor-footer">
          <div class="footer-left">
            <span id="cursor-pos">0, 0</span>
            <span class="divider"></span>
            <span id="canvas-size">0 × 0</span>
          </div>
          <div class="footer-center">
            <button class="icon-btn" id="btn-zoom-fit" title="Fit to Screen">🔍⛶</button>
            <button class="icon-btn" id="btn-zoom-100" title="100%">100%</button>
            <button class="icon-btn" id="btn-rotate-ccw" title="Rotate CCW">↶</button>
            <button class="icon-btn" id="btn-rotate-cw" title="Rotate CW">↷</button>
            <button class="icon-btn" id="btn-flip-h" title="Flip Horizontal">↔</button>
            <button class="icon-btn" id="btn-flip-v" title="Flip Vertical">↕</button>
          </div>
          <div class="footer-right">
            <span id="color-preview" class="color-preview"></span>
            <input type="color" id="color-picker" value="#000000">
          </div>
        </footer>
      </div>

      <!-- Panels Content Templates -->
      <template id="panel-adjust">
        <div class="panel-section">
          <h4>Light</h4>
          <div class="slider-row"><label>Exposure</label><input type="range" data-filter="exposure" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Contrast</label><input type="range" data-filter="contrast" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Highlights</label><input type="range" data-filter="highlights" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Shadows</label><input type="range" data-filter="shadows" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Whites</label><input type="range" data-filter="whites" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Blacks</label><input type="range" data-filter="blacks" min="-100" max="100" value="0"><span class="value">0</span></div>
        </div>
        <div class="panel-section">
          <h4>Color</h4>
          <div class="slider-row"><label>Temperature</label><input type="range" data-filter="temperature" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Tint</label><input type="range" data-filter="tint" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Vibrance</label><input type="range" data-filter="vibrance" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Saturation</label><input type="range" data-filter="saturation" min="-100" max="100" value="0"><span class="value">0</span></div>
        </div>
        <div class="panel-section">
          <h4>Detail</h4>
          <div class="slider-row"><label>Clarity</label><input type="range" data-filter="clarity" min="-100" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Sharpness</label><input type="range" data-filter="sharpness" min="0" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Denoise</label><input type="range" data-filter="denoise" min="0" max="100" value="0"><span class="value">0</span></div>
        </div>
        <div class="panel-section">
          <h4>Effects</h4>
          <div class="slider-row"><label>Vignette</label><input type="range" data-filter="vignette" min="0" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Grain</label><input type="range" data-filter="grain" min="0" max="100" value="0"><span class="value">0</span></div>
          <div class="slider-row"><label>Sharpness</label><input type="range" data-filter="sharpness" min="0" max="100" value="0"><span class="value">0</span></div>
        </div>
        <div class="presets-grid">
          <h4>Presets</h4>
          <div class="presets-row" id="presets-row"></div>
        </div>
      </template>

      <template id="panel-filters">
        <div class="filter-grid" id="filter-grid"></div>
      </template>

      <template id="panel-ai">
        <div class="ai-tools-grid">
          <button class="ai-tool-card" data-ai="removeBg">
            <span class="ai-icon">🪄</span>
            <h4>Remove Background</h4>
            <p>Auto-remove background with AI</h4>
          </button>
          <button class="ai-tool-card" data-ai="upscale">
            <span class="ai-icon">🔍</span>
            <h4>Upscale 2x/4x</h4>
            <p>AI super-resolution upscaling</h4>
          </button>
          <button class="ai-tool-card" data-ai="inpaint">
            <span class="ai-icon">🖌️</span>
            <h4>Inpaint</h4>
            <p>Remove objects, fill with AI</h4>
          </button>
          <button class="ai-tool-card" data-ai="outpaint">
            <span class="ai-icon">🖼️</span>
            <h4>Outpaint</h4>
            <p>Extend image beyond borders</h4>
          </button>
          <button class="ai-tool-card" data-ai="enhance">
            <span class="ai-icon">✨</span>
            <h4>Enhance Face</h4>
            <p>AI face restoration & enhancement</h4>
          </button>
          <button class="ai-tool-card" data-ai="colorize">
            <span class="ai-icon">🎨</span>
            <h4>Colorize</h4>
            <p>Colorize B&W photos</h4>
          </button>
          <button class="ai-tool-card" data-ai="style">
            <span class="ai-icon">🎭</span>
            <h4>Style Transfer</h4>
            <p>Apply artistic styles</h4>
          </button>
          <button class="ai-tool-card" data-ai="replace">
            <span class="ai-icon">🔄</span>
            <h4>Replace Object</h4>
            <p>Select & replace with AI</h4>
          </button>
        </div>
      </template>

      <template id="panel-layers">
        <div class="layers-list" id="layers-list"></div>
        <button class="btn-secondary" id="btn-add-layer">+ Add Layer</button>
      </template>

      <template id="panel-text">
        <div class="text-options">
          <input type="text" id="text-content" placeholder="Type your text...">
          <div class="font-options">
            <select id="font-family"><option value="Inter">Inter</option><option value="system-ui">System</option><option value="Georgia">Georgia</option><option value="Impact">Impact</option></select>
            <input type="number" id="font-size" value="48" min="8" max="500">
            <input type="color" id="text-color" value="#ffffff">
          </div>
          <div class="text-align">
            <button data-align="left">⬛</button>
            <button data-align="center">⬜</button>
            <button data-align="right">⬛</button>
          </div>
          <label><input type="checkbox" id="text-stroke"> Stroke</label>
          <input type="number" id="stroke-width" value="2" min="0" max="20">
          <input type="color" id="stroke-color" value="#000000">
          <button class="btn-primary" id="btn-add-text">Add Text</button>
        </div>
      </template>
    `;

    container.innerHTML = html;
    initEditor();
  }

  // ── Initialize Editor ──
  function initEditor() {
    canvas = document.getElementById('editor-canvas');
    ctx = canvas.getContext('2d', { willReadFrequently: true });

    setupCanvas();
    setupToolbars();
    setupPanels();
    setupEventListeners();
    setupDragDrop();
    setupTouch();
    setupKeyboardShortcuts();

    // Load Fabric.js if available
    if (typeof window.fabric !== 'undefined') {
      fabric = window.fabric;
      initFabric();
    } else {
      loadFabric();
    }

    console.log('[ImageEditor] Initialized');
  }

  function setupCanvas() {
    const wrapper = document.getElementById('canvas-wrapper');
    resizeCanvas();
    window.addEventListener('resize', resizeCanvas);
  }

  function resizeCanvas() {
    const wrapper = document.getElementById('canvas-wrapper');
    const rect = wrapper.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;

    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    canvas.style.width = rect.width + 'px';
    canvas.style.height = rect.height + 'px';
    ctx.scale(dpr, dpr);

    if (currentImage) {
      redrawCanvas();
    }
    updateCanvasInfo();
  }

  function updateCanvasInfo() {
    const wrapper = document.getElementById('canvas-wrapper');
    $('#canvas-size').textContent = `${canvas.width} × ${canvas.height}`;
  }

  // ── Toolbar Setup ──
  function setupToolbars() {
    // Main tools
    const toolsList = $('#tools-list');
    Object.entries(tools).filter(([k]) => !['removeBg', 'upscale', 'inpaint', 'outpaint'].includes(k))
      .forEach(([key, tool]) => {
        const btn = document.createElement('button');
        btn.className = 'tool-btn' + (key === currentTool ? ' active' : '');
        btn.dataset.tool = key;
        btn.title = tool.name;
        btn.innerHTML = `<span class="tool-icon">${tool.icon}</span><span>${tool.name}</span>`;
        btn.onclick = () => setTool(key);
        toolsList.appendChild(btn);
      });

    // AI Tools
    const aiList = $('#ai-tools-list');
    ['removeBg', 'upscale', 'inpaint', 'outpaint', 'enhance', 'colorize', 'style', 'replace'].forEach(key => {
      const tool = tools[key];
      const btn = document.createElement('button');
      btn.className = 'ai-tool-btn';
      btn.dataset.ai = key;
      btn.innerHTML = `<span class="ai-icon">${tool.icon}</span><span>${tool.name}</span>`;
      btn.onclick = () => runAITool(key);
      document.getElementById('ai-tools-list').appendChild(btn);
    });

    // Panel tabs
    const panelTabs = ['adjust', 'filters', 'ai', 'layers', 'text'];
    const panelTabsEl = $('#panel-tabs');
    panelTabs.forEach((tab, i) => {
      const btn = document.createElement('button');
      btn.className = 'panel-tab' + (i === 0 ? ' active' : '');
      btn.dataset.panel = tab;
      btn.textContent = tab.charAt(0).toUpperCase() + tab.slice(1);
      btn.onclick = () => switchPanel(tab);
      panelTabsEl.appendChild(btn);
    });

    // Load first panel
    loadPanel('adjust');

    // Filter grid
    renderFilters();

    // Presets
    renderPresets();

    // Layers
    updateLayersList();

    // Text panel
    setupTextPanel();
  }

  // ── Panel Management ──
  function switchPanel(name) {
    $$('.panel-tab').forEach(btn => btn.classList.toggle('active', btn.dataset.panel === name));
    loadPanel(name);
  }

  function loadPanel(name) {
    const content = $('#panel-content');
    const template = document.getElementById('panel-' + name);
    if (template) {
      content.innerHTML = template.innerHTML;
      initPanel(name);
    }
  }

  function initPanel(name) {
    switch (name) {
      case 'adjust':
        initAdjustPanel();
        break;
      case 'filters':
        renderFilters();
        break;
      case 'ai':
        // AI tools already rendered
        break;
      case 'layers':
        updateLayersList();
        break;
      case 'text':
        setupTextPanel();
        break;
    }
  }

  function initAdjustPanel() {
    // Sliders
    $$('#panel-content input[type="range"]').forEach(slider => {
      slider.addEventListener('input', (e) => {
        const filter = e.target.dataset.filter;
        const value = parseInt(e.target.value);
        e.target.nextElementSibling.textContent = value;
        applyFilter(filter, value);
      });
    });

    // Presets
    $('#presets-row').innerHTML = Object.entries(presets).map(([key, preset]) => `
      <button class="preset-btn" data-preset="${key}" title="${preset.name}">
        ${preset.name}
      </button>
    `).join('');

    $$('.preset-btn').forEach(btn => {
      btn.onclick = () => applyPreset(btn.dataset.preset);
    });

    // Color picker
    $('#color-picker').addEventListener('input', (e) => {
      $('#color-preview').style.background = e.target.value;
      setColor(e.target.value);
    });
  }

  // ── Filter Rendering ──
  function renderFilters() {
    const grid = $('#filter-grid');
    grid.innerHTML = Object.entries(filters).map(([key, filter]) => `
      <button class="filter-btn" data-filter="${key}" title="${filter.name}">
        <span class="filter-name">${filter.name}</span>
        <input type="range" min="${filter.min}" max="${filter.max}" value="${filter.default}" data-filter="${key}">
        <span class="filter-value">${filter.default}${filter.unit}</span>
      </button>
    `).join('');

    $$('.filter-btn input').forEach(input => {
      input.addEventListener('input', (e) => {
        const filter = e.target.dataset.filter;
        const value = parseInt(e.target.value);
        e.target.nextElementSibling.textContent = value + (filters[filter].unit || '');
        applyFilter(filter, value);
      });
    });
  }

  function renderPresets() {
    const row = $('#presets-row');
    row.innerHTML = Object.entries(presets).map(([key, preset]) => `
      <button class="preset-btn" data-preset="${key}" title="${preset.name}">${preset.name}</button>
    `).join('');

    $$('.preset-btn').forEach(btn => {
      btn.onclick = () => applyPreset(btn.dataset.preset);
    });
  }

  // ── Filter Application ──
  let filterValues = {};

  function applyFilter(name, value) {
    filterValues[name] = value;
    if (currentImage) {
      applyFiltersToCanvas();
    }
  }

  function applyFiltersToCanvas() {
    if (!currentImage) return;

    // Save current state for history
    saveState();

    const tempCanvas = document.createElement('canvas');
    const tempCtx = tempCanvas.getContext('2d');
    tempCanvas.width = currentImage.width;
    tempCanvas.height = currentImage.height;

    // Draw original
    tempCtx.drawImage(currentImage, 0, 0);

    // Apply filters using CSS filters via canvas
    const imageData = tempCtx.getImageData(0, 0, tempCanvas.width, tempCanvas.height);
    const data = imageData.data;

    const f = filterValues;
    const brightness = (f.brightness || 0) / 100;
    const contrast = (f.contrast || 0) / 100;
    const saturation = (f.saturation || 0) / 100;
    const temperature = f.temperature || 0;
    const tint = f.tint || 0;
    const brightnessVal = (f.brightness || 0) / 100;
    const contrast = 1 + (f.contrast || 0) / 100;
    const saturation = 1 + (f.saturation || 0) / 100;
    const vibrance = f.vibrance || 0;
    const temperature = f.temperature || 0;
    const tint = f.tint || 0;
    const highlights = f.highlights || 0;
    const shadows = f.shadows || 0;
    const whites = f.whites || 0;
    const blacks = f.blacks || 0;
    const clarity = f.clarity || 0;
    const sharpness = f.sharpness || 0;
    const vignette = f.vignette || 0;
    const grain = f.grain || 0;
    const sepia = f.sepia || 0;
    const blur = f.blur || 0;
    const sharpness = f.sharpness || 0;

    for (let i = 0; i < data.length; i += 4) {
      let r = data[i];
      let g = data[i + 1];
      let b = data[i + 2];
      const a = data[i + 3];

      // Brightness
      r = Math.min(255, r + brightness * 255);
      g = Math.min(255, g + brightness * 255);
      b = Math.min(255, b + brightness * 255);

      // Contrast
      r = 128 + contrast * (r - 128);
      g = 128 + contrast * (g - 128);
      b = 128 + contrast * (b - 128);

      // Saturation
      const gray = 0.299 * r + 0.587 * g + 0.114 * b;
      r = gray + saturation * (r - gray);
      g = gray + saturation * (g - gray);
      b = gray + saturation * (b - gray);

      // Temperature & Tint
      r += temperature;
      b -= temperature;
      g += tint;

      // Highlights/Shadows/Whites/Blacks (simplified)
      const luminance = 0.299 * r + 0.587 * g + 0.114 * b;
      if (luminance > 180) { r += highlights * 0.5; g += highlights * 0.5; b += highlights * 0.5; }
      if (luminance < 60) { r += shadows * 0.5; g += shadows * 0.5; b += shadows * 0.5; }
      if (r > 240) { r += whites; g += whites; b += whites; }
      if (luminance < 30) { r += blacks; g += blacks; b += blacks; }

      // Clarity (local contrast approximation)
      // Simplified - would need convolution for real clarity

      // Sepia
      if (sepia > 0) {
        const sr = r * 0.393 + g * 0.769 + b * 0.189;
        const sg = r * 0.349 + g * 0.686 + b * 0.168;
        const sb = r * 0.272 + g * 0.534 + b * 0.131;
        r = r * (1 - sepia/100) + sr * (sepia/100);
        g = g * (1 - sepia/100) + sg * (sepua/100);
        b = b * (1 - sepia/100) + sb * (sepia/100);
      }

      // Vignette
      // Would need coordinate-based calculation

      data[i] = Math.max(0, Math.min(255, r));
      data[i+1] = Math.max(0, Math.min(255, g));
      data[i+2] = Math.max(0, Math.min(255, b));
    }

    tempCtx.putImageData(imageData, 0, 0);

    // Apply blur if needed
    if (blur > 0) {
      // Would need convolution
    }

    // Draw to main canvas
    redrawCanvas(tempCanvas);
  }

  function redrawCanvas(sourceCanvas = null) {
    const src = sourceCanvas || canvas;
    ctx.clearRect(0, 0, canvas.width, canvas.height);

    // Apply pan/zoom
    ctx.save();
    ctx.translate(pan.x, pan.y);
    ctx.scale(zoom, zoom);

    if (sourceCanvas) {
      ctx.drawImage(sourceCanvas, 0, 0);
    } else if (currentImage) {
      ctx.drawImage(currentImage, 0, 0);
    }

    // Draw crop overlay if active
    if (currentTool === 'crop' && cropRect) {
      drawCropOverlay();
    }

    ctx.restore();
  }

  // ── History ──
  function saveState() {
    if (historyIndex < history.length - 1) {
      history = history.slice(0, historyIndex + 1);
    }
    const dataUrl = canvas.toDataURL('image/png');
    history.push(dataUrl);
    historyIndex = history.length - 1;
    if (history.length > 50) { history.shift(); historyIndex--; }
    updateUndoRedo();
  }

  function undo() {
    if (historyIndex > 0) {
      historyIndex--;
      loadFromHistory();
    }
  }

  function redo() {
    if (historyIndex < history.length - 1) {
      historyIndex++;
      loadFromHistory();
    }
  }

  function loadFromHistory() {
    const img = new Image();
    img.onload = () => {
      currentImage = img;
      redrawCanvas();
      saveState(); // Don't save again
    };
    img.src = history[historyIndex];
  }

  function updateUndoRedo() {
    $('#btn-undo').disabled = historyIndex <= 0;
    $('#btn-redo').disabled = historyIndex >= history.length - 1;
  }

  // ── Tools ──
  function setTool(name) {
    currentTool = name;
    $$('.tool-btn').forEach(btn => btn.classList.toggle('active', btn.dataset.tool === name));
    $$('.tool-btn').forEach(btn => btn.classList.toggle('active', btn.dataset.tool === name));

    canvas.style.cursor = tools[name]?.cursor || 'default';

    // Show/hide relevant panels
    if (name === 'adjust') switchPanel('adjust');
    else if (name === 'filter') switchPanel('filters');
    else if (name === 'crop') showCropOverlay();
    else if (name === 'text') { switchPanel('text'); showTextInput(); }
  }

  function runAITool(name) {
    showToast(`Running ${tools[name].name}...`, 'info');

    switch (name) {
      case 'removeBg':
        removeBackground();
        break;
      case 'upscale':
        upscaleImage();
        break;
      case 'inpaint':
        currentTool = 'inpaint';
        setTool('inpaint');
        showToast('Paint over area to remove', 'info');
        break;
      case 'outpaint':
        outpaintImage();
        break;
      case 'enhance':
        enhanceFace();
        break;
      case 'colorize':
        colorizeImage();
        break;
      case 'style':
        applyStyleTransfer();
        break;
      case 'replace':
        currentTool = 'replace';
        setTool('replace');
        showToast('Select object to replace', 'info');
        break;
    }
  }

  // ── AI Operations ──
  async function removeBackground() {
    if (!currentImage) return;
    showToast('Removing background...', 'info');

    try {
      // Call AI endpoint
      const blob = await canvasToBlob(canvas);
      const formData = new FormData();
      formData.append('image', blob, 'image.png');

      const res = await fetch('/api/ai/remove-bg', {
        method: 'POST',
        body: formData
      });

      const result = await res.json();
      if (result.success) {
        loadImageFromUrl(result.imageUrl);
        showToast('Background removed!', 'success');
      }
    } catch (e) {
      showToast('Background removal failed', 'error');
    }
  }

  async function upscaleImage() {
    if (!currentImage) return;
    showToast('Upscaling 2x...', 'info');

    try {
      const blob = await canvasToBlob(canvas);
      const formData = new FormData();
      formData.append('image', blob, 'image.png');
      formData.append('scale', '2');

      const res = await fetch('/api/ai/upscale', {
        method: 'POST',
        body: formData
      });

      const result = await res.json();
      if (result.success) {
        loadImageFromUrl(result.imageUrl);
        showToast('Upscaled 2x!', 'success');
      }
    } catch (e) {
      showToast('Upscale failed', 'error');
    }
  }

  async function inpaint(mask) {
    // Would send image + mask to inpainting API
  }

  async function outpaintImage() {
    // Extend canvas and fill with AI
  }

  async function enhanceFace() {
    // Call face enhancement API
  }

  async function colorizeImage() {
    // Call colorization API
  }

  async function applyStyleTransfer() {
    // Show style options, then apply
  }

  // ── File Upload ──
  function setupDragDrop() {
    const wrapper = document.getElementById('canvas-wrapper');

    ['dragenter', 'dragover'].forEach(e => {
      wrapper.addEventListener(e, (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        wrapper.classList.add('drag-over');
      });
    });

    ['dragleave', 'drop'].forEach(e => {
      wrapper.addEventListener(e, (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        wrapper.classList.remove('drag-over');
      });
    });

    wrapper.addEventListener('drop', async (ev) => {
      const file = ev.dataTransfer.files[0];
      if (file && file.type.startsWith('image/')) {
        loadImageFile(file);
      }
    });
  }

  function setupTouch() {
    let lastTouch = null;

    canvas.addEventListener('touchstart', (e) => {
      if (e.touches.length === 1) {
        lastTouch = { x: e.touches[0].clientX, y: e.touches[0].clientY };
      } else if (e.touches.length === 2) {
        // Pinch zoom
        const dx = e.touches[0].clientX - e.touches[1].clientX;
        const dy = e.touches[0].clientY - e.touches[1].clientY;
        lastTouch = { distance: Math.hypot(dx, dy) };
      }
    }, { passive: false });

    canvas.addEventListener('touchmove', (e) => {
      e.preventDefault();
      if (e.touches.length === 1 && lastTouch && !lastTouch.distance) {
        const dx = e.touches[0].clientX - lastTouch.x;
        const dy = e.touches[0].clientY - lastTouch.y;
        pan.x += dx;
        pan.y += dy;
        lastTouch = { x: e.touches[0].clientX, y: e.touches[0].clientY };
        redrawCanvas();
      } else if (e.touches.length === 2 && lastTouch.distance) {
        const dx = e.touches[0].clientX - e.touches[1].clientX;
        const dy = e.touches[0].clientY - e.touches[1].clientY;
        const newDist = Math.hypot(dx, dy);
        const scale = newDist / lastTouch.distance;
        zoom = Math.max(0.1, Math.min(5, zoom * scale));
        lastTouch.distance = newDist;
        $('#zoom-level').textContent = Math.round(zoom * 100) + '%';
        redrawCanvas();
      }
    }, { passive: false });

    canvas.addEventListener('touchend', () => { lastTouch = null; });
  }

  function setupKeyboardShortcuts() {
    document.addEventListener('keydown', (e) => {
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;

      if (e.metaKey || e.ctrlKey) {
        switch (e.key.toLowerCase()) {
          case 'z': e.preventDefault(); e.shiftKey ? redo() : undo(); break;
          case 'y': e.preventDefault(); redo(); break;
          case 's': e.preventDefault(); exportImage(); break;
          case 'o': e.preventDefault(); openFile(); break;
          case '=': e.preventDefault(); zoomIn(); break;
          case '-': e.preventDefault(); zoomOut(); break;
          case '0': e.preventDefault(); resetZoom(); break;
        }
      } else {
        switch (e.key.toLowerCase()) {
          case 'v': setTool('select'); break;
          case 'c': setTool('crop'); break;
          case 'b': setTool('brush'); break;
          case 'e': setTool('eraser'); break;
          case 't': setTool('text'); break;
          case 'r': resetZoom(); break;
          case 'z': zoomIn(); break;
          case 'x': zoomOut(); break;
          case 'delete':
          case 'backspace': deleteSelected(); break;
          case 'escape': closeSheet(); cancelCrop(); break;
        }
      }
    });
  }

  // ── File Handling ──
  async function loadImageFile(file) {
    if (!file.type.startsWith('image/')) {
      showToast('Please select an image file', 'error');
      return;
    }

    showToast('Loading image...', 'info');

    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = (e) => {
        const img = new Image();
        img.onload = () => {
          currentImage = img;
          saveState();
          setupCanvasForImage(img);
          redrawCanvas();
          updateCanvasInfo();
          $('#img-info').textContent = `${img.width} × ${img.height} · ${formatBytes(file.size)}`;
          showToast('Image loaded', 'success');
          resolve(img);
        };
        img.onerror = () => reject(new Error('Failed to load image'));
        img.src = e.target.result;
      };
      reader.onerror = () => reject(new Error('Failed to read file'));
      reader.readAsDataURL(file);
    });
  }

  function loadImageFromUrl(url) {
    return new Promise((resolve, reject) => {
      const img = new Image();
      img.crossOrigin = 'anonymous';
      img.onload = () => {
        currentImage = img;
        setupCanvasForImage(img);
        redrawCanvas();
        updateCanvasInfo();
        resolve(img);
      };
      img.onerror = () => reject(new Error('Failed to load image'));
      img.src = url;
    };
  }

  function setupCanvasForImage(img) {
    const maxDim = 4096;
    let { width, height } = img;

    if (width > maxDim || height > maxDim) {
      const scale = Math.min(maxDim / width, maxDim / height);
      width = Math.round(width * scale);
      height = Math.round(height * scale);
    }

    canvas.width = width * (window.devicePixelRatio || 1);
    canvas.height = height * (window.devicePixelRatio || 1);
    canvas.style.width = width + 'px';
    canvas.height = height + 'px';

    // Reset transform
    zoom = 1;
    pan = { x: 0, y: 0 };
    zoom = 1;
    $('#zoom-level').textContent = '100%';
  }

  // ── File Upload (iOS/Android) ──
  function openFile() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/*';
    input.capture = 'environment'; // Prefer camera on mobile
    input.onchange = (e) => {
      const file = e.target.files[0];
      if (file) loadImageFile(file);
    };
    input.click();
  }

  // Native camera capture
  async function takePhoto() {
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        video: { facingMode: 'environment', width: { ideal: 1920 }, height: { ideal: 1080 } }
      });

      const video = document.createElement('video');
      video.srcObject = stream;
      video.play();

      await new Promise(r => video.onloadeddata = r);

      const canvas = document.createElement('canvas');
      canvas.width = video.videoWidth;
      canvas.height = video.videoHeight;
      canvas.getContext('2d').drawImage(video, 0, 0);

      stream.getTracks().forEach(t => t.stop());

      const blob = await new Promise(r => canvas.toBlob(r, 'image/jpeg', 0.9));
      loadImageFile(new File([blob], 'photo.jpg', { type: 'image/jpeg' }));
    } catch (e) {
      showToast('Camera access denied', 'error');
    }
  }

  // ── Export ──
  function exportImage() {
    const format = 'png';
    const quality = 1.0;

    canvas.toBlob((blob) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `friday-edit-${Date.now()}.${format}`;
      a.click();
      URL.revokeObjectURL(url);
      showToast('Image exported', 'success');
    }, `image/${format}`, quality);
  }

  // ── Utility ──
  function canvasToBlob(canvas, type = 'image/png', quality = 1) {
    return new Promise(resolve => canvas.toBlob(resolve, type, quality));
  }

  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
  }

  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.textContent = message;
    document.body.appendChild(toast);
    setTimeout(() => toast.remove(), 3000);
  }

  function formatBytes(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1048576) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / 1048576).toFixed(1) + ' MB';
  }

  // ── Public API ──
  return {
    init,
    loadImageFile,
    loadImageFromUrl,
    takePhoto,
    openFile,
    exportImage,
    setTool,
    runAITool,
    undo,
    redo,
    zoomIn,
    zoomOut,
    resetZoom,
    exportImage,
    applyPreset,
    applyFilter,
    runAITool,
    removeBackground,
    upscaleImage,
  };
})();

// ── Initialize on Load ──
document.addEventListener('DOMContentLoaded', () => {
  if (document.getElementById('editor-container')) {
    ImageEditor.init('editor-container');
  }
});

// Export for module systems
if (typeof module !== 'undefined' && module.exports) {
  module.exports = ImageEditor;
}