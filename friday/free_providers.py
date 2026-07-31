"""
Friday Base - Free Tier Provider Auto-Discovery
Automatically discovers, validates, and adds free LLM providers to the chain.
No manual key chain.
Supports: NVIDIA, GitHub Models, OpenRouter, Qwen/DashScope, Zhipu, Groq, Gemini, etc.
"""
import json
import logging
import os
import subprocess
import sys
import threading
import time
from typing import Dict, List, Optional

import requests

from .config import config, PROVIDER_ENDPOINTS, Candidate

logger = logging.getLogger("Friday.FreeProviders")

FREE_PROVIDER_REGISTRY = {
    "nvidia": {
        "name": "NVIDIA NIM",
        "signup_url": "https://build.nvidia.com/settings/api-keys",
        "models_url": "https://integrate.api.nvidia.com/v1/models",
        "default_model": "meta/llama-3.3-70b-instruct",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "github": {
        "name": "GitHub Models",
        "signup_url": "https://github.com/settings/tokens",
        "models_url": "https://models.inference.ai.azure.com/models",
        "default_model": "gpt-4o-mini",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "openrouter": {
        "name": "OpenRouter",
        "signup_url": "https://openrouter.ai/keys",
        "models_url": "https://openrouter.ai/api/v1/models",
        "default_model": "meta-llama/llama-3.3-70b-instruct:free",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "qwen": {
        "name": "Qwen (DashScope)",
        "signup_url": "https://dashscope-intl.aliyuncs.com/",
        "models_url": "https://dashscope-intl.aliyuncs.com/compatible-mode/v1/models",
        "default_model": "qwen-plus",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "zhipu": {
        "name": "Zhipu AI (GLM)",
        "signup_url": "https://open.bigmodel.cn/",
        "models_url": "https://open.bigmodel.cn/api/paas/v4/models",
        "default_model": "glm-5.2",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "groq": {
        "name": "Groq",
        "signup_url": "https://console.groq.com/keys",
        "models_url": "https://api.groq.com/openai/v1/models",
        "default_model": "llama-3.3-70b-versatile",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"Authorization": f"Bearer {key}"},
    },
    "gemini": {
        "name": "Google Gemini",
        "signup_url": "https://aistudio.google.com/app/apikey",
        "models_url": "https://generativelanguage.googleapis.com/v1beta/models",
        "default_model": "gemini-2.0-flash",
        "free_tier": True,
        "no_cc": True,
        "headers": lambda key: {"x-goog-api-key": key},
    },
}

DISCOVERY_CACHE = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "data", "free_providers.json"
)
os.makedirs(os.path.dirname(DISCOVERY_CACHE), exist_ok=True)

_cache_lock = threading.Lock()
_last_discovery = 0
_discovery_cache: Dict = {}


def _load_cache() -> Dict:
    global _discovery_cache, _last_discovery
    try:
        with open(DISCOVERY_CACHE, "r", encoding="utf-8") as f:
            data = json.load(f)
            _discovery_cache = data.get("providers", {})
            _last_discovery = data.get("timestamp", 0)
    except Exception:
        _discovery_cache = {}
        _last_discovery = 0
    return _discovery_cache


def _save_cache():
    with _cache_lock:
        try:
            with open(DISCOVERY_CACHE, "w", encoding="utf-8") as f:
                json.dump({
                    "timestamp": time.time(),
                    "providers": _discovery_cache,
                }, f, indent=2)
        except Exception as e:
            logger.warning(f"Failed to save provider cache: {e}")


def test_provider(provider: str, api_key: str, timeout: int = 10) -> bool:
    """Test if a provider key works by making a lightweight call."""
    reg = FREE_PROVIDER_REGISTRY.get(provider)
    if not reg:
        return False
    
    endpoint = PROVIDER_ENDPOINTS.get(provider)
    if not endpoint:
        return False
    
    base_url = endpoint[0]
    headers = reg["headers"](api_key)
    
    try:
        # Try models endpoint first (lightweight)
        resp = requests.get(f"{base_url}/models", headers=headers, timeout=timeout)
        if resp.status_code == 200:
            return True
        # Fallback: try a minimal chat completion
        resp = requests.post(
            f"{base_url}/chat/completions",
            headers={**headers, "Content-Type": "application/json"},
            json={
                "model": reg["default_model"],
                "messages": [{"role": "user", "content": "hi"}],
                "max_tokens": 1,
            },
            timeout=timeout,
        )
        return resp.status_code == 200
    except Exception as e:
        logger.debug(f"Provider {provider} test failed: {e}")
        return False


def discover_available_models(provider: str, api_key: str) -> List[str]:
    """Fetch available models from provider."""
    reg = FREE_PROVIDER_REGISTRY.get(provider)
    if not reg:
        return []
    
    endpoint = PROVIDER_ENDPOINTS.get(provider)
    if not endpoint:
        return []
    
    base_url = endpoint[0]
    headers = reg["headers"](api_key)
    
    try:
        resp = requests.get(f"{base_url}/models", headers=headers, timeout=10)
        if resp.status_code == 200:
            data = resp.json()
            # Handle different response formats
            if isinstance(data, dict) and "data" in data:
                return [m.get("id", "") for m in data["data"] if m.get("id")]
            elif isinstance(data, list):
                return [m.get("id", "") for m in data if m.get("id")]
    except Exception as e:
        logger.debug(f"Model discovery failed for {provider}: {e}")
    return []


def auto_discover_and_register() -> Dict[str, bool]:
    """
    Scan environment for free provider keys, test them, and register working ones.
    Returns dict of provider -> success.
    """
    _load_cache()
    results = {}
    
    for provider, reg in FREE_PROVIDER_REGISTRY.items():
        env_var = PROVIDER_ENDPOINTS.get(provider, (None, None))[1]
        if not env_var:
            continue
            
        api_key = os.getenv(env_var, "").strip()
        if not api_key or api_key.startswith("your-") or "placeholder" in api_key.lower():
            results[provider] = False
            continue
        
        # Test the key
        logger.info(f"Testing {reg['name']} ({provider})...")
        works = test_provider(provider, api_key)
        results[provider] = works
        
        if works:
            models = discover_available_models(provider, api_key)
            _discovery_cache[provider] = {
                "works": True,
                "models": models,
                "tested_at": time.time(),
                "default_model": reg["default_model"],
            }
            logger.info(f"✓ {reg['name']} working - {len(models)} models discovered")
        else:
            _discovery_cache[provider] = {
                "works": False,
                "tested_at": time.time(),
            }
            logger.warning(f"✗ {reg['name']} key invalid or quota exhausted")
    
    _save_cache()
    return results


def get_working_free_providers() -> List[Candidate]:
    """Get list of working free providers as Candidates for chain insertion."""
    _load_cache()
    candidates = []
    
    for provider, info in _discovery_cache.items():
        if not info.get("works"):
            continue
        reg = FREE_PROVIDER_REGISTRY.get(provider)
        if not reg:
            continue
        model = info.get("models", [reg["default_model"]])[0] if info.get("models") else reg["default_model"]
        candidates.append(Candidate(provider=provider, model=model))
    
    # Priority order: fastest/most reliable first
    priority = ["groq", "gemini", "openrouter", "nvidia", "github", "qwen", "zhipu"]
    candidates.sort(key=lambda c: priority.index(c.provider) if c.provider in priority else 99)
    return candidates


def inject_free_providers_into_chains():
    """Inject discovered free providers at the front of role chains."""
    free_candidates = get_working_free_providers()
    if not free_candidates:
        logger.info("No working free providers discovered")
        return
    
    # Insert at front of each role chain (highest priority)
    for role in config.role_chains:
        existing = config.role_chains[role]
        # Remove duplicates
        existing_providers = {c.provider for c in existing}
        new_chain = []
        for c in free_candidates:
            if c.provider not in existing_providers:
                new_chain.append(c)
        new_chain.extend(existing)
        config.role_chains[role] = new_chain
    
    logger.info(f"Injected {len(free_candidates)} free providers into chains: {[c.provider for c in free_candidates]}")


def print_free_provider_status():
    """Print a nice status table of free providers."""
    _load_cache()
    print("\n=== FREE TIER PROVIDER STATUS ===")
    print(f"{'Provider':<12} {'Status':<10} {'Models':<6} {'Default Model'}")
    print("-" * 70)
    for provider, reg in FREE_PROVIDER_REGISTRY.items():
        info = _discovery_cache.get(provider, {})
        works = info.get("works", False)
        models = info.get("models", [])
        status = "✓ WORKING" if works else "✗ NOT CONFIGURED"
        env_var = PROVIDER_ENDPOINTS.get(provider, (None, None))[1]
        has_key = bool(os.getenv(env_var, "").strip()) if env_var else False
        if not has_key:
            status = "⚠ NO KEY"
        print(f"{reg['name']:<12} {status:<10} {len(models):<6} {info.get('default_model', reg['default_model'])}")
    print()


def setup_free_provider_keys_interactive():
    """Interactive helper to guide user through getting free keys."""
    print("\n🔑 FREE TIER PROVIDER SETUP GUIDE")
    print("=" * 50)
    print("All below are FREE, no credit card required:\n")
    
    for provider, reg in FREE_PROVIDER_REGISTRY.items():
        env_var = PROVIDER_ENDPOINTS.get(provider, (None, None))[1]
        current = os.getenv(env_var, "").strip() if env_var else ""
        has_key = bool(current) and not current.startswith("your-")
        status = "✓ SET" if has_key else "✗ MISSING"
        print(f"  {reg['name']:<20} {status}")
        if not has_key:
            print(f"    → Get key: {reg['signup_url']}")
            print(f"    → Add to .env: {env_var}=your_key_here")
        print()
    
    print("After adding keys, restart Friday to auto-discover them.")


# Auto-run on import if enabled
if os.getenv("AUTO_DISCOVER_FREE_PROVIDERS", "true").lower() == "true":
    try:
        auto_discover_and_register()
        inject_free_providers_into_chains()
    except Exception as e:
        logger.debug(f"Auto-discovery deferred: {e}")


_discovery_thread = None
_discovery_stop = threading.Event()


def _discovery_loop():
    """Background thread that periodically re-discovers providers."""
    while not _discovery_stop.is_set():
        # Sleep in chunks to allow quick shutdown
        for _ in range(3600):  # 1 hour
            if _discovery_stop.is_set():
                break
            time.sleep(1)
        if not _discovery_stop.is_set():
            try:
                logger.info("Running scheduled free provider re-discovery...")
                auto_discover_and_register()
                inject_free_providers_into_chains()
            except Exception as e:
                logger.warning(f"Scheduled discovery failed: {e}")


def start_auto_discovery(interval_seconds: int = 3600):
    """Start background auto-discovery of free providers."""
    global _discovery_thread
    if _discovery_thread is not None and _discovery_thread.is_alive():
        return
    _discovery_stop.clear()
    _discovery_thread = threading.Thread(target=_discovery_loop, daemon=True, name="FridayFreeProviderDiscovery")
    _discovery_thread.start()
    logger.info(f"Free provider auto-discovery started (interval: {interval_seconds}s)")


def stop_auto_discovery():
    """Stop the background discovery thread."""
    _discovery_stop.set()
    if _discovery_thread:
        _discovery_thread.join(timeout=5)


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    setup_free_provider_keys_interactive()
    print("\nRunning discovery...")
    results = auto_discover_and_register()
    print(f"\nResults: {results}")
    print_free_provider_status()