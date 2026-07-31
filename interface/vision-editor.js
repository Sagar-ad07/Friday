/* ── Friday Vision & Natural Language Editing ── */
/* Seamless integration: "Friday, make this photo warmer" → done */

const VisionEditor = (function() {
  'use strict';

  let isAnalyzing = false;
  let currentAnalysis = null;
  let pendingEdit = null;

  // ── Vision Analysis ──
  async function analyzeImage(canvas, prompt = 'Describe this image in detail') {
    if (!canvas) return null;

    showToast('Analyzing image...', 'info');

    try {
      const blob = await canvasToBlob(canvas);
      const formData = new FormData();
      formData.append('image', blob, 'image.png');
      formData.append('prompt', prompt);

      const res = await fetch('/api/vision/analyze', {
        method: 'POST',
        body: formData
      });

      const result = await res.json();
      if (result.success) {
        currentAnalysis = result;
        showToast('Analysis complete', 'success');
        return result;
      }
    } catch (e) {
      console.error('Vision analysis failed:', e);
      showToast('Analysis failed', 'error');
    }
    return null;
  }

  // ── Natural Language Editing ──
  async function editWithNaturalLanguage(canvas, instruction) {
    if (!canvas) return false;

    showToast(`Applying: "${instruction}"...`, 'info');
    pendingEdit = instruction;

    try {
      const blob = await canvasToBlob(canvas);
      const formData = new FormData();
      formData.append('image', blob, 'image.png');
      formData.append('instruction', instruction);

      const res = await fetch('/api/vision/edit', {
        method: 'POST',
        body: formData
      });

      const result = await res.json();
      if (result.success) {
        await loadImageFromUrl(result.imageUrl);
        showToast('Edit applied!', 'success');
        return true;
      }
    } catch (e) {
      console.error('Natural language edit failed:', e);
      showToast('Edit failed: ' + e.message, 'error');
    }
    return false;
  }

  // ── Voice + Vision Combined ──
  async function voiceEdit(canvas) {
    if (!('SpeechRecognition' in window || 'webkitSpeechRecognition' in window)) {
      showToast('Voice not supported', 'warning');
      return;
    }

    const SR = window.SpeechRecognition || window.webkitSpeechRecognition;
    const rec = new SR();
    rec.lang = 'en-US';
    rec.interimResults = false;

    return new Promise((resolve) => {
      rec.onresult = async (e) => {
        const text = e.results[0][0].transcript.trim();
        if (text) {
          const canvas = document.getElementById('editor-canvas');
          await editWithNaturalLanguage(canvas, text);
          resolve(true);
        } else {
          resolve(false);
        }
      };
      rec.onerror = (e) => {
        if (e.error !== 'no-speech') showToast('Voice error: ' + e.error, 'error');
        resolve(false);
      };
      rec.start();
      setTimeout(() => { try { rec.stop(); } catch(e) {} resolve(false); }, 10000);
    });
  }

  // ── Quick Voice Commands ──
  const voiceCommands = {
    // Color/Tone
    'warmer': 'Make the image warmer, increase temperature',
    'cooler': 'Make the image cooler, decrease temperature',
    'brighter': 'Increase brightness and exposure',
    'darker': 'Decrease brightness and exposure',
    'more contrast': 'Increase contrast',
    'less contrast': 'Decrease contrast',
    'more color': 'Increase saturation and vibrance',
    'less color': 'Decrease saturation',
    'black and white': 'Convert to black and white',
    'sepia': 'Apply sepia tone',
    'vintage': 'Apply vintage film look',
    'cinematic': 'Make it look cinematic',

    // AI Operations
    'remove background': 'Remove the background completely',
    'remove bg': 'Remove the background completely',
    'blur background': 'Blur the background, keep subject sharp',
    'sharpen': 'Sharpen the image',
    'denoise': 'Reduce noise and grain',
    'upscale': 'Upscale the image 2x',
    'enhance face': 'Enhance and restore faces',
    'fix face': 'Enhance and restore faces',
    'colorize': 'Colorize this black and white photo',
    'restore': 'Restore this old damaged photo',
    'remove object': 'Remove the selected object',
    'remove person': 'Remove the person from the photo',
    'extend': 'Extend the image borders with AI',
    'outpaint': 'Extend the image borders with AI',
    'replace sky': 'Replace the sky with a dramatic sky',
    'change background': 'Change the background to something else',

    // Artistic
    'vintage': 'Apply vintage film look',
    'cinematic': 'Make it look cinematic',
    'film look': 'Apply film grain and color grading',
    'black and white': 'Convert to black and white',
    'sketch': 'Convert to pencil sketch',
    'oil painting': 'Make it look like an oil painting',
    'watercolor': 'Make it look like watercolor',
    'anime': 'Convert to anime style',

    // Practical
    'crop to square': 'Crop to 1:1 square',
    'crop to story': 'Crop to 9:16 story format',
    'crop to post': 'Crop to 4:5 post format',
    'straighten': 'Straighten the horizon',
    'rotate': 'Rotate 90 degrees clockwise',
    'flip': 'Flip horizontally',
    'add text': 'Add text overlay',
    'watermark': 'Add watermark',
    'blur faces': 'Blur all faces for privacy',
  };

  // Parse natural language to structured edit
  function parseInstruction(text) {
    const lower = text.toLowerCase().trim();

    // Check for direct command matches
    for (const [phrase, action] of Object.entries(voiceCommands)) {
      if (lower.includes(phrase)) {
        return { action, confidence: 0.9, original: text };
      }
    }

    // Fallback: send to LLM for parsing
    return { action: text, confidence: 0.5, original: text };
  }

  // ── Smart Edit: Analyze + Edit ──
  async function smartEdit(canvas, instruction) {
    if (!canvas) return false;

    // First analyze if needed
    if (!currentAnalysis) {
      await analyzeImage(canvas, 'Analyze this image for editing context');
    }

    // Parse instruction
    const parsed = parseInstruction(instruction);

    if (parsed.confidence > 0.8) {
      // High confidence - execute directly
      return await editWithNaturalLanguage(canvas, parsed.action);
    } else {
      // Lower confidence - send full instruction to AI
      return await editWithNaturalLanguage(canvas, instruction);
    }
  }

  // ── Batch Operations ──
  async function batchEdit(files, instruction) {
    const results = [];
    for (const file of files) {
      try {
        const canvas = await fileToCanvas(file);
        await editWithNaturalLanguage(canvas, instruction);
        const blob = await canvasToBlob(canvas);
        results.push({ file: file.name, success: true, blob });
      } catch (e) {
        results.push({ file: file.name, success: false, error: e.message });
      }
    }
    return results;
  }

  // ── Upload Handlers (Mobile + Desktop) ──
  function setupMobileUpload() {
    // Camera capture
    window.takePhoto = async () => {
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

        return canvas;
      } catch (e) {
        showToast('Camera access denied', 'error');
        return null;
      }
    };

    // Photo library
    window.pickPhoto = () => {
      return new Promise((resolve) => {
        const input = document.createElement('input');
        input.type = 'file';
        input.accept = 'image/*';
        input.capture = 'environment';
        input.onchange = (e) => {
          const file = e.target.files[0];
          if (file) loadImageFile(file).then(() => resolve());
        };
        input.click();
      });
    };

    // Drag & drop (desktop)
    document.addEventListener('drop', (e) => {
      e.preventDefault();
      const file = e.dataTransfer.files[0];
      if (file && file.type.startsWith('image/')) {
        loadImageFile(file);
      }
    });

    // Paste from clipboard
    document.addEventListener('paste', async (e) => {
      const items = e.clipboardData.items;
      for (const item of items) {
        if (item.type.startsWith('image/')) {
          const blob = item.getAsFile();
          const canvas = await fileToCanvas(blob);
          if (canvas) {
            currentImage = canvas;
            redrawCanvas();
            showToast('Image pasted', 'success');
          }
        }
      }
    });
  }

  // ── Natural Language Chat Integration ──
  function bindChat() {
    const originalSend = window.sendChatMessage;
    window.sendChatMessage = async function(text) {
      // Check if it's an image editing command
      const lower = text.toLowerCase();
      const editTriggers = ['edit', 'change', 'make', 'apply', 'add', 'remove', 'fix', 'enhance', 'adjust', 'filter', 'color'];

      const isEditCommand = editTriggers.some(t => text.toLowerCase().includes(t)) &&
        (currentImage || document.getElementById('editor-canvas'));

      if (isEditCommand) {
        const canvas = document.getElementById('editor-canvas');
        if (canvas) {
          const success = await smartEdit(canvas, text);
          if (success) return; // Don't send to chat
        }
      }

      // Fall back to normal chat
      if (originalSend) originalSend(text);
    };
  }

  // ── Proactive Suggestions ──
  function suggestEdits() {
    if (!currentImage || !currentAnalysis) return [];

    const suggestions = [];

    if (currentAnalysis.hasFaces) {
      suggestions.push({ action: 'enhance face', label: '✨ Enhance Faces', desc: 'AI face restoration' });
      suggestions.push({ action: 'blur faces', label: '🔒 Blur Faces', desc: 'Privacy protection' });
    }

    if (currentAnalysis.hasSky) {
      suggestions.push({ action: 'replace sky', label: '☁️ Replace Sky', desc: 'Dramatic sky replacement' });
    }

    if (currentAnalysis.hasBackground) {
      suggestions.push({ action: 'remove background', label: '🪄 Remove BG', desc: 'Transparent background' });
      suggestions.push({ action: 'blur background', label: '🌫️ Blur Background', desc: 'Portrait mode effect' });
    }

    if (currentAnalysis.isLowLight) {
      suggestions.push({ action: 'brighten', label: '☀️ Brighten', desc: 'Fix underexposure' });
      suggestions.push({ action: 'denoise', label: '🔧 Denoise', desc: 'Reduce grain' });
    }

    if (currentAnalysis.isOldPhoto) {
      suggestions.push({ action: 'restore', label: '🔧 Restore', desc: 'Repair damage & colorize' });
    }

    return suggestions;
  }

  // ── Quick Actions UI ──
  function renderQuickActions() {
    const container = document.getElementById('quick-actions');
    if (!container) return;

    const suggestions = suggestEdits();
    if (!suggestions.length) return;

    container.innerHTML = `
      <div class="quick-actions-bar">
        <h4>💡 Suggested</h4>
        <div class="quick-actions-scroll">
          ${suggestions.map(s => `
            <button class="quick-action-btn" data-action="${s.action}">
              <span class="qa-icon">${s.label.split(' ')[0]}</span>
              <span class="qa-label">${s.label.split(' ').slice(1).join(' ')}</span>
            </button>
          `).join('')}
        </div>
      </div>
    `;

    container.querySelectorAll('.quick-action-btn').forEach(btn => {
      btn.onclick = () => {
        const canvas = document.getElementById('editor-canvas');
        if (canvas) smartEdit(canvas, btn.dataset.action);
      };
    });
  }

  // ── Expose API ──
  return {
    analyzeImage,
    editWithNaturalLanguage,
    smartEdit,
    voiceEdit,
    voiceCommands,
    parseInstruction,
    parseInstruction,
    voiceCommands,
    batchEdit,
    bindChat,
    suggestEdits,
    renderQuickActions,
    setupMobileUpload,
    bindChat,
  };
})();

// Auto-init when editor loads
document.addEventListener('DOMContentLoaded', () => {
  if (window.ImageEditor) {
    // Bind chat after editor loads
    setTimeout(() => {
      if (window.VisionEditor) {
        VisionEditor.bindChat();
        VisionEditor.setupMobileUpload();
      }
    }, 100);
  }
});

// Export
if (typeof module !== 'undefined' && module.exports) {
  module.exports = VisionEditor;
}