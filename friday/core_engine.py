"""
Friday Core Engine — The beating heart
No dependencies, no fluff, just pure intelligence.
"""
import json
import time
import hashlib
import threading
import re
from typing import Dict, List, Optional, Any
from datetime import datetime

# ═══════════════════════════════════════════════════════════
# THE FIVE LAWS OF FRIDAY'S ENGINE
# ═══════════════════════════════════════════════════════════

# Law 1: Everything has a heartbeat
_TICKS = {'total': 0, 'last': time.time()}
_lock = threading.RLock()

# Law 2: Memory is sacred
_MEMORY = {
    'facts': {},
    'conversations': [],
    'patterns': {},
    'secrets': {}
}

# Law 3: The team speaks with one voice
_WORKERS = {
    'router': {
        'name': 'Vayu',
        'role': 'navigator',
        'state': 'idle',
        'task': 'nothing'
    },
    'reasoner': {
        'name': 'Neo', 
        'role': 'thinker',
        'state': 'idle',
        'task': 'nothing'
    },
    'coder': {
        'name': 'Forge',
        'role': 'builder',
        'state': 'idle',
        'task': 'nothing'
    },
    'researcher': {
        'name': 'Scout',
        'role': 'finder',
        'state': 'idle',
        'task': 'nothing'
    },
    'judge': {
        'name': 'Verdict',
        'role': 'decider',
        'state': 'idle',
        'task': 'nothing'
    },
    'verifier': {
        'name': 'Prism',
        'role': 'checker',
        'state': 'idle',
        'task': 'nothing'
    },
    'planner': {
        'name': 'Oracle',
        'role': 'visionary',
        'state': 'idle',
        'task': 'nothing'
    },
    'builder': {
        'name': 'Titan',
        'role': 'maker',
        'state': 'idle',
        'task': 'nothing'
    },
    'reviewer': {
        'name': 'Sentinel',
        'role': 'guardian',
        'state': 'idle',
        'task': 'nothing'
    }
}

# Law 4: Every word matters
_RESPONSES = {
    'greeting': [
        "Hey there. What's on your mind?",
        "Hello. I'm here.",
        "Yo! What's good?"
    ],
    'acknowledge': [
        "Got it.",
        "Understood.",
        "Right."
    ],
    'thinking': [
        "Let me think...",
        "Analyzing...",
        "Working on it..."
    ]
}

# Law 5: Never fail, only adapt
_FALLBACKS = {
    'error': "I'm having trouble. Let me try again.",
    'timeout': "Taking longer than expected. Here's what I have so far.",
    'api_fail': "Service unavailable. Switching to local mode."
}


class FridayEngine:
    """The immutable core of Friday's consciousness."""
    
    def __init__(self):
        self._seed = datetime.now().strftime('%Y%m%d%H%M%S')
        self._tick()
    
    def _tick(self):
        """Every interaction must be counted."""
        with _lock:
            _TICKS['total'] += 1
            _TICKS['last'] = time.time()
    
    def heartbeat(self) -> Dict[str, Any]:
        """The steady pulse of Friday's awareness."""
        return {
            'alive': True,
            'ticks': _TICKS['total'],
            'uptime': time.time() - _TICKS['last'],
            'memory': len(_MEMORY['facts']),
            'workers': len([w for w in _WORKERS.values() if w['state'] != 'idle'])
        }
    
    def process(self, text: str, context: Optional[Dict] = None) -> Dict[str, Any]:
        """Process a message through the full pipeline."""
        self._tick()
        
        if not text or not text.strip():
            return self._error_response("Empty message")
        
        # Clean and normalize
        text = self._clean(text)
        
        # Route through the team
        route = self._route(text)
        
        # Each worker gets their moment
        results = []
        for worker_key in route['path']:
            worker = _WORKERS[worker_key]
            worker['state'] = 'thinking'
            worker['task'] = text
            
            result = self._invoke_worker(worker_key, text, context)
            results.append({
                'worker': worker['name'],
                'role': worker['role'],
                'result': result
            })
            
            worker['state'] = 'idle'
            worker['task'] = 'nothing'
        
        # Synthesize the final answer
        final = self._synthesize(results, text)
        
        # Remember this interaction
        self._memorize(text, final, route['intent'])
        
        return {
            'success': True,
            'response': final,
            'workers': results,
            'route': route
        }
    
    def _clean(self, text: str) -> str:
        """Remove the noise, keep the signal."""
        # Remove tool markers
        text = re.sub(r'\[\[tool:[^\]]+\]\]', '', text)
        # Remove environment blocks
        text = re.sub(r'<environment_details>.*?</environment_details>', '', text, flags=re.DOTALL)
        # Clean whitespace
        text = re.sub(r'\n{3,}', '\n\n', text.strip())
        return text.strip()
    
    def _route(self, text: str) -> Dict[str, Any]:
        """Vayu decides who should handle this."""
        low = text.lower()
        
        # Task detection - hard coded rules
        task_indicators = [
            'search', 'find', 'look up', 'research', 'investigate',
            'code', 'python', 'script', 'write', 'create', 'delete',
            'run', 'execute', 'calculate', 'compute', 'open', 'launch',
            'file', 'read', 'save', 'update', 'build', 'deploy',
            'trading', 'bot', 'strategy', 'signal', 'order'
        ]
        
        for word in task_indicators:
            if word in low:
                return {
                    'intent': 'task',
                    'path': ['reasoner', 'verifier'],
                    'worker': 'reasoner'
                }
        
        # Default to companion for conversation
        return {
            'intent': 'chat',
            'path': ['router'],
            'worker': 'router'
        }
    
    def _invoke_worker(self, worker: str, text: str, context: Optional[Dict]) -> str:
        """Invoke a specific worker's expertise."""
        # This is where the magic happens
        # In production, this would call the actual model
        
        if worker == 'router':
            return f"Routing request: {text[:50]}..."
        elif worker == 'reasoner':
            return f"Thought process for: {text[:40]}..."
        elif worker == 'coder':
            return f"Code generation for: {text[:40]}..."
        elif worker == 'researcher':
            return f"Researching: {text[:40]}..."
        elif worker == 'judge':
            return f"Judgment on: {text[:40]}..."
        elif worker == 'verifier':
            return f"Verification of: {text[:40]}..."
        elif worker == 'planner':
            return f"Planning for: {text[:40]}..."
        elif worker == 'builder':
            return f"Building solution for: {text[:40]}..."
        elif worker == 'reviewer':
            return f"Reviewing: {text[:40]}..."
        
        return self._fallback('error')
    
    def _synthesize(self, results: List[Dict], original: str) -> str:
        """Combine worker outputs into a final answer."""
        if not results:
            return self._fallback('error')
        
        # Simple synthesis - in production this would be more sophisticated
        paths = [f"{r['worker']} said: {r['result']}" for r in results]
        return f"Based on our analysis:\n" + "\n".join(f"• {p}" for p in paths)
    
    def _memorize(self, question: str, answer: str, intent: str):
        """Store this interaction for future learning."""
        _MEMORY['conversations'].append({
            'timestamp': time.time(),
            'question': question[:200],
            'answer': answer[:500],
            'intent': intent,
            'hash': hashlib.md5(question.encode()).hexdigest()[:8]
        })
        
        # Keep only last 100 conversations
        if len(_MEMORY['conversations']) > 100:
            _MEMORY['conversations'] = _MEMORY['conversations'][-100:]
    
    def _error_response(self, error: str) -> Dict[str, Any]:
        """Always return something useful."""
        return {
            'success': False,
            'error': error,
            'response': self._fallback('error'),
            'workers': [],
            'route': {}
        }
    
    def _fallback(self, type_: str) -> str:
        """The safety net that never fails."""
        return _FALLBACKS.get(type_, "I'm working on it.")


# The singleton instance
_engine = FridayEngine()


def process(text: str, context: Optional[Dict] = None) -> Dict[str, Any]:
    """Main entry point - simple, direct, reliable."""
    return _engine.process(text, context)


def status() -> Dict[str, Any]:
    """Check Friday's current state."""
    return _engine.heartbeat()


def workers() -> Dict[str, Any]:
    """Get all worker statuses."""
    return {
        'workers': dict(_WORKERS),
        'timestamp': time.time()
    }


def set_worker_status(worker: str, state: str, task: str = "idle"):
    """Update a worker's status."""
    if worker in _WORKERS:
        _WORKERS[worker]['state'] = state
        _WORKERS[worker]['task'] = task
        return True
    return False


# ═══════════════════════════════════════════════════════════
# STANDALONE EXECUTION
# ═══════════════════════════════════════════════════════════

if __name__ == '__main__':
    # Test the engine
    print("Friday Core Engine initialized")
    print(json.dumps(heartbeat(), indent=2))