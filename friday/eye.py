"""
Friday Base - Eye core (brain-side)

The BRAIN owns vision. Thin eye agents (laptop / phone) only CAPTURE an image
and UPLOAD it via POST /eye/submit. The brain describes it here (via a VISION
PROVIDER CHAIN) and stores the latest live context per device + a merged view.

This keeps vision cost flat: N eyes with unchanged screens = ~1 vision call.
Eye agents never call vision APIs directly. The chain falls back across
providers (OpenAI -> Gemini -> OpenRouter -> Groq) so a single exhausted free
tier does not kill the eye. On total failure the caller just gets None (eye
goes stale) — never crashes Friday.
"""
import base64
import logging
import threading
import time
import hashlib
from typing import Dict, Optional

from .config import config

logger = logging.getLogger("Friday.Eye")

# Per-device live context. Each entry: {description, ts, kind, hash}
_LIVE: Dict[str, dict] = {}
_LIVE_LOCK = threading.Lock()

# Vision provider chain: first that returns text wins. Keeps the eye alive
# across quota limits. Ollama (local vision model) is used first when enabled
# in local mode, then DeepSeek cloud fallback.
def _vision_chain():
    chain = []
    if config.provider_mode == "local" and getattr(config, "local_vision_enabled", False):
        chain.append(("ollama", None))
    # In local mode we do NOT append cloud vision providers; the eye stays fully
    # offline. Cloud providers are only used when explicitly configured.
    if config.provider_mode != "local":
        for prov in ("openai", "gemini", "openrouter", "deepseek"):
            if config.has_key(prov):
                chain.append((prov, None))
    return chain


def _hash_bytes(b: bytes) -> str:
    return hashlib.md5(b).hexdigest()


def _prompt_for(kind: str) -> str:
    if kind == "camera":
        return ("You are Friday's eye looking through the user's phone camera at "
                "their desk/workspace. Describe briefly what you can see (person, "
                "desk, device, anything notable). 2-3 short sentences. If nothing "
                "notable, say 'nothing notable'.")
    return ("You are Friday's live eye. The user is working at their computer "
            "while talking to you. Describe concisely WHAT the user is currently "
            "looking at and doing (app/window, content, any task in progress, "
            "errors, or anything notable). 2-4 short sentences. If nothing "
            "notable, say 'nothing notable'.")


def describe_image(image_b64: str, kind: str = "screen") -> Optional[str]:
    """Describe an image using the configured vision model.

    Returns None if no vision model is configured or all providers fail.
    """
    # Skip entirely if no vision model is configured.
    vision_model = getattr(config, "ollama_vision_model", None)
    if not vision_model:
        return None
    """Describe a base64 image via a VISION PROVIDER CHAIN. Returns text or None.

    Falls back across providers so a single exhausted free tier does not kill
    the eye. Used ONLY by the brain; eye agents never call vision themselves.
    On total failure the caller just gets None (eye goes stale) — never crashes.
    """
    prompt = _prompt_for(kind)
    last_err = None
    for prov, model in _vision_chain():
        if prov == "local":
            pass
        elif not config.has_key(prov):
            continue
        if prov == "gemini" and not config.gemini_model:
            continue
        if prov == "openai" and not getattr(config, "openai_vision_model", None):
            continue
        try:
            from openai import OpenAI
            client = OpenAI(
                base_url=config.endpoint(prov),
                api_key=config.keys.get(prov, "dummy"),
            )
            if model:
                m = model
            elif prov == "openai":
                m = config.openai_vision_model
            elif prov == "ollama":
                m = getattr(config, "ollama_vision_model", None) or "moondream:latest"
            else:
                m = config.gemini_model
            resp = client.chat.completions.create(
                model=m,
                messages=[{
                    "role": "user",
                    "content": [
                        {"type": "text", "text": prompt},
                        {"type": "image_url", "image_url": {
                            "url": f"data:image/jpeg;base64,{image_b64}"}},
                    ],
                }],
                temperature=0.2,
                max_tokens=300,
                timeout=getattr(config, "llm_call_timeout", 25),
            )
            text = (resp.choices[0].message.content or "").strip()
            if text:
                logger.info("Eye vision ok via %s/%s", prov, m)
                return text
            last_err = "empty vision response"
        except Exception as e:
            last_err = e
            logger.warning("Eye vision %s/%s failed: %s", prov, model, e)
    if last_err:
        logger.warning("Eye vision all providers failed: %s", last_err)
    return None


def submit_frame(device: str, image_b64: str, kind: str = "screen") -> dict:
    """Brain stores a frame from an eye agent. Describes only if changed.

    Returns {description, active, changed}. This is the single vision call point.
    """
    if not image_b64:
        return {"description": "", "active": False, "changed": False}
    raw = base64.b64decode(image_b64)
    h = _hash_bytes(raw)
    with _LIVE_LOCK:
        prev = _LIVE.get(device)
        if prev and prev.get("hash") == h:
            # Unchanged frame: no vision call, just refresh ts lightly.
            return {"description": prev.get("description", ""), "active": True,
                    "changed": False}
    desc = describe_image(image_b64, kind=kind)
    with _LIVE_LOCK:
        _LIVE[device] = {
            "description": desc or "",
            "ts": time.time(),
            "kind": kind,
            "hash": h,
        }
    return {"description": desc or "", "active": bool(desc), "changed": True}


def get_state() -> dict:
    """Merged + per-device live eye state for clients. No API call."""
    with _LIVE_LOCK:
        devices = {}
        for dev, v in _LIVE.items():
            devices[dev] = {
                "active": bool(v.get("description")),
                "description": v.get("description", ""),
                "kind": v.get("kind"),
                "age_seconds": int(time.time() - v.get("ts", 0)) if v.get("ts") else None,
            }
        merged = ""
        latest = 0.0
        for v in _LIVE.values():
            if v.get("description") and v.get("ts", 0) >= latest:
                merged = v["description"]
                latest = v["ts"]
        any_active = any(d["active"] for d in devices.values())
    return {
        "active": any_active,
        "description": merged,
        "guide_only": getattr(config, "eye_guide_only", True),
        "devices": devices,
    }


def local_description() -> str:
    """Best current description for injecting into a chat/voice turn."""
    st = get_state()
    if not st.get("description"):
        return ""
    return f"[Live screen context — what I see right now]\n{st['description']}"
