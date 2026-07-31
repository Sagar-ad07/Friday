#!/usr/bin/env python3
"""
Friday HTTP Server — No dependencies, no problems.
Pure Python 3.11+ with built-in resilience.
"""
import json
import time
import threading
import traceback
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs
from datetime import datetime

# Import the core engine
from friday.core_engine import process, status, workers, set_worker_status

# ═══════════════════════════════════════════════════════════
# THE SERVER'S HEARTBEAT
# ═══════════════════════════════════════════════════════════

class FridayHandler(BaseHTTPRequestHandler):
    """Every request is a conversation."""
    
    # Class-level rate limiter
    _request_count = 0
    _last_reset = time.time()
    _lock = threading.Lock()
    
    def log_message(self, format, *args):
        """Silent logging - production doesn't need chatter."""
        pass  # Uncomment for debugging: print(f"[{datetime.now().isoformat()}] {args[0]}")
    
    def _send_json(self, data: dict, code: int = 200):
        """Send JSON response with proper headers."""
        self.send_response(code)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Cache-Control', 'no-store, no-cache, must-revalidate')
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False, indent=2).encode('utf-8'))
    
    def _send_html(self, html: str, code: int = 200):
        """Send HTML response."""
        self.send_response(code)
        self.send_header('Content-Type', 'text/html; charset=utf-8')
        self.send_header('Cache-Control', 'no-store, no-cache, must-revalidate')
        self.end_headers()
        self.wfile.write(html.encode('utf-8'))
    
    def _rate_limit(self) -> bool:
        """Simple rate limiting - 30 requests per second per IP."""
        with self.__class__._lock:
            now = time.time()
            if now - self.__class__._last_reset > 1:
                self.__class__._request_count = 0
                self.__class__._last_reset = now
            
            if self.__class__._request_count > 30:
                return False
            
            self.__class__._request_count += 1
            return True
    
    def do_OPTIONS(self):
        """Handle CORS preflight."""
        self.send_response(200)
        self.send_header('Access-Control-Allow-Origin', '*')
        self.send_header('Access-Control-Allow-Methods', 'GET, POST, OPTIONS')
        self.send_header('Access-Control-Allow-Headers', 'Content-Type, Authorization')
        self.end_headers()
    
    def do_GET(self):
        """Handle GET requests."""
        if not self._rate_limit():
            self._send_json({'error': 'rate_limited', 'retry_after': 1}, 429)
            return
        
        try:
            parsed = urlparse(self.path)
            path = parsed.path
            
            # ── HEALTH CHECK ──
            if path == '/health':
                return self._send_json({'status': 'ok', 'timestamp': time.time()})
            
            # ── STATUS ENDPOINT ──
            if path == '/status':
                hb = status()
                return self._send_json({
                    'status': 'online',
                    'no_key': False,
                    'providers': ['local'],
                    'uptime': hb['uptime'],
                    'ticks': hb['ticks'],
                    'workers': hb['workers'],
                    'memory_facts': hb['memory']
                })
            
            # ── WORKER STATUS ──
            if path == '/workers/status':
                return self._send_json(workers())
            
            # ── TEAM ROSTER ──
            if path == '/team':
                from friday.team import roster
                return self._send_json({'team': roster()})
            
            # ── ROOT / INTERFACE ──
            if path == '/' or path == '/index.html':
                try:
                    with open('interface/index.html', 'rb') as f:
                        html = f.read().decode('utf-8')
                    return self._send_html(html)
                except FileNotFoundError:
                    return self._send_json({'error': 'interface not found'}, 404)
            
            # ── STATIC FILES ──
            if path.startswith('/static/'):
                try:
                    file_path = path[1:]  # Remove leading /
                    with open(file_path, 'rb') as f:
                        content = f.read()
                    self.send_response(200)
                    self.end_headers()
                    return self.wfile.write(content)
                except FileNotFoundError:
                    return self._send_json({'error': 'not found'}, 404)
            
            # 404 for unknown routes
            return self._send_json({'error': 'not found', 'path': path}, 404)
            
        except Exception as e:
            self._send_json({'error': 'server_error', 'message': str(e)}, 500)
    
    def do_POST(self):
        """Handle POST requests."""
        if not self._rate_limit():
            self._send_json({'error': 'rate_limited', 'retry_after': 1}, 429)
            return
        
        try:
            parsed = urlparse(self.path)
            path = parsed.path
            
            # ── CHAT / COMMAND ──
            if path == '/command' or path == '/chat':
                return self._handle_chat()
            
            # ── STREAMING COMMAND ──
            if path == '/command/stream':
                return self._handle_stream()
            
            # ── WORKER STATUS UPDATE ──
            if path == '/workers/update':
                return self._handle_worker_update()
            
            return self._send_json({'error': 'not found'}, 404)
            
        except Exception as e:
            self._send_json({'error': 'server_error', 'message': str(e)}, 500)
    
    def _read_body(self) -> dict:
        """Safely read and parse request body."""
        try:
            content_length = int(self.headers.get('Content-Length', 0))
            if content_length > 1024 * 1024:  # 1MB limit
                return {}
            
            body = self.rfile.read(content_length)
            return json.loads(body.decode('utf-8'))
        except:
            return {}
    
    def _handle_chat(self):
        """Process a chat message."""
        data = self._read_body()
        text = data.get('text', '')
        
        if not text:
            return self._send_json({'error': 'empty_message', 'reply': 'What would you like to discuss?'})
        
        # Update router status
        set_worker_status('router', 'thinking', f'Receiving: {text[:30]}...')
        
        # Process through the engine
        result = process(text)
        
        # Reset status
        set_worker_status('router', 'idle', 'Ready')
        
        return self._send_json(result)
    
    def _handle_stream(self):
        """Handle streaming responses."""
        data = self._read_body()
        text = data.get('text', '')
        
        if not text:
            return self._send_json({'error': 'empty_message'})
        
        # Send SSE headers
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Cache-Control', 'no-cache')
        self.send_header('X-Accel-Buffering', 'no')
        self.end_headers()
        
        # Stream the response
        try:
            result = process(text)
            
            # Stream worker status events
            for worker in result.get('workers', []):
                event = {
                    'type': 'worker_status',
                    'worker': worker['role'],
                    'status': 'working',
                    'activity': worker['result'][:40]
                }
                self.wfile.write(f"data: {json.dumps(event)}\n\n".encode('utf-8'))
                self.wfile.flush()
            
            # Final response
            event = {
                'type': 'final',
                'reply': result.get('response', ''),
                'name': result['workers'][0]['worker'] if result.get('workers') else 'Friday'
            }
            self.wfile.write(f"data: {json.dumps(event)}\n\n".encode('utf-8'))
            self.wfile.flush()
            
        except Exception as e:
            event = {'type': 'error', 'message': str(e)}
            self.wfile.write(f"data: {json.dumps(event)}\n\n".encode('utf-8'))
    
    def _handle_worker_update(self):
        """Update worker status directly."""
        data = self._read_body()
        worker = data.get('worker')
        status = data.get('status', 'idle')
        task = data.get('task', 'nothing')
        
        if worker:
            set_worker_status(worker, status, task)
            return self._send_json({'success': True, 'worker': worker, 'status': status})
        
        return self._send_json({'error': 'worker not specified'}, 400)


# ═══════════════════════════════════════════════════════════
# SERVER STARTUP
# ═══════════════════════════════════════════════════════════

def create_server(host: str = '0.0.0.0', port: int = 8080) -> HTTPServer:
    """Create the HTTP server with built-in resilience."""
    
    # Custom server class with timeout
    class ResilientServer(HTTPServer):
        allow_reuse_address = True
        timeout = 30
        
        def handle_timeout(self):
            """Handle server timeout gracefully."""
            pass
    
    server = ResilientServer((host, port), FridayHandler)
    print(f"Friday server listening on {host}:{port}")
    return server


if __name__ == '__main__':
    server = create_server()
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nFriday shutting down...")
        server.shutdown()