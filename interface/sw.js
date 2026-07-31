const CACHE_NAME = 'friday-v2';
const ASSETS = [
  '/', '/static/ios.css', '/static/ios.js', '/static/ios.html',
  '/static/editor.js', '/static/vision-editor.js', '/static/editor.js',
  '/static/assets/friday-logo-192.png', '/static/assets/friday-logo-512.png',
  '/static/assets/friday-mark.svg', '/static/manifest.json'
];

self.addEventListener('install', e => {
  e.waitUntil(caches.open(CACHE_NAME).then(c => c.addAll(ASSETS)));
  self.skipWaiting();
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(keys => 
      Promise.all(keys.filter(k => k !== CACHE_NAME).map(k => caches.delete(k)))
    )
  );
  self.clients.claim();
});

self.addEventListener('fetch', e => {
  // Network-first for API calls, cache-first for static
  if (e.request.url.includes('/api/')) {
    e.respondWith(networkFirst(e.request));
  } else {
    e.respondWith(cacheFirst(e.request));
  }
});

async function networkFirst(request) {
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(CACHE_NAME);
      cache.put(request, response.clone());
    }
    return response;
  } catch (e) {
    const cached = await caches.match(request);
    return cached || new Response('Offline', { status: 503 });
  }
}

async function cacheFirst(request) {
  const cached = await caches.match(request);
  if (cached) return cached;
  try {
    const response = await fetch(request);
    if (response.ok) {
      const cache = await caches.open(CACHE_NAME);
      cache.put(request, response.clone());
    }
    return response;
  } catch (e) {
    return new Response('Offline', { status: 503 });
  }
}

// Background sync for offline actions
self.addEventListener('sync', e => {
  if (e.tag === 'voice-actions') {
    e.waitUntil(syncVoiceActions());
  }
});

async function syncVoiceActions() {
  const db = await openDB();
  const actions = await db.getAll('pending-actions');
  for (const action of actions) {
    try {
      await fetch(action.url, { method: action.method, body: action.body });
      await db.delete('pending-actions', action.id);
    } catch (e) {
      console.log('Sync failed for:', action.id);
    }
  }
}

function openDB() {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open('friday-offline', 1);
    request.onupgradeneeded = e => {
      const db = e.target.result;
      db.createObjectStore('pending-actions', { keyPath: 'id' });
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  };
}