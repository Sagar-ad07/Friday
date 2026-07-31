"""
Friday Base - Screen Watcher (local laptop eye)

The watcher CAPTURES the local screen (pyautogui) and SUBMITS frames to the
brain's eye store (friday.eye.submit_frame). The brain does the Gemini vision
call. This keeps vision cost flat and lets remote eyes (phone) feed the same
store, so Friday "sees" consistently whether you're on laptop or phone.

Session memory management:
  * Captures are stored per-session under config.data_dir/screen_sessions/
  * After a session ends, Friday extracts knowledge from the screenshots,
    stores it in memory, then DELETES the raw images to save disk space.
  * Only the extracted insights remain; raw pixels are discarded.

Safety / smoothness:
  * SCREEN_WATCH is OFF by default. You turn it on.
  * Cadence is SCREEN_WATCH_INTERVAL and it SKIPS entirely when the screen is
    unchanged or when you're already mid-conversation/executing - so idle time
    costs ~0 Gemini calls. This protects your free Gemini tier.
  * Vision runs on Gemini ONLY (separate from Groq chat), so a vision limit can
    never break your normal chat.
  * If vision fails, the loop just waits - it can never crash Friday.
  * PROACTIVE_ACT / EYE_GUIDE_ONLY keep Friday from acting unless you command.
"""
import base64
import hashlib
import json
import logging
import os
import threading
import time
from typing import Callable, Optional

from .config import config

logger = logging.getLogger("Friday.ScreenWatcher")

_STOP = threading.Event()
_LAST_HASH = [None]
_ACTIVITY = {"busy": False}  # set True while a turn/execution runs
_ON_RESULT: Optional[Callable[[str, str], None]] = None
_MODE = "normal"  # "normal"(slow), "rapid"(fast for trading co-pilot)
_COPILOT_LAST_ADVICE = [0.0]
_COPILOT_COOLDOWN = 30  # seconds between co-pilot advice prompts
_RAPID_INTERVAL = 3
_SLOW_INTERVAL = 30

# The local watcher identifies itself as the "laptop" eye device.
_LIVE_DEVICE = "laptop"

# Session-based capture storage.
_SESSION_DIR = os.path.join(config.data_dir, "screen_sessions")
_CURRENT_SESSION = [None]
_SESSION_LOCK = threading.Lock()


def set_busy(flag: bool) -> None:
    """Mark when the user is mid-conversation/executing so the watcher
    stays quiet (and spends no API calls)."""
    _ACTIVITY["busy"] = bool(flag)


def set_mode(mode: str) -> None:
    """Switch between 'normal' (slow, 30s) and 'rapid' (fast, 3s) screen watch."""
    global _MODE
    if mode in ("normal", "rapid"):
        _MODE = mode
        logger.info("Screen watch mode: %s", mode)


def get_mode() -> str:
    return _MODE


def on_offer(cb: Callable[[str, str], None]) -> None:
    """Register a callback(reason, detail) fired when Friday offers help."""
    global _ON_RESULT
    _ON_RESULT = cb


# ── Session management ─────────────────────────────────────────────────────────
def start_session(session_name: str = None) -> str:
    """Start a new screen capture session. Returns session ID."""
    with _SESSION_LOCK:
        if _CURRENT_SESSION[0] is None:
            session_id = session_name or f"session_{int(time.time())}"
            session_dir = os.path.join(_SESSION_DIR, session_id)
            os.makedirs(session_dir, exist_ok=True)
            _CURRENT_SESSION[0] = {
                "id": session_id,
                "dir": session_dir,
                "start_time": time.time(),
                "captures": [],
            }
            logger.info("Screen session started: %s", session_id)
            return session_id
        return _CURRENT_SESSION[0]["id"]


def end_session() -> dict:
    """End the current session, extract knowledge, delete raw screenshots.
    Returns summary of extracted knowledge."""
    with _SESSION_LOCK:
        session = _CURRENT_SESSION[0]
        if session is None:
            return {"status": "no_active_session"}
        _CURRENT_SESSION[0] = None

    session_id = session["id"]
    session_dir = session["dir"]
    captures = session.get("captures", [])

    logger.info("Ending screen session: %s (%d captures)", session_id, len(captures))

    # Extract knowledge from the session's descriptions.
    knowledge = _extract_session_knowledge(session_id, captures)

    # Delete raw screenshots.
    deleted = 0
    try:
        if os.path.exists(session_dir):
            for f in os.listdir(session_dir):
                if f.endswith(".jpg") or f.endswith(".jpeg") or f.endswith(".png"):
                    os.remove(os.path.join(session_dir, f))
                    deleted += 1
            # Remove session directory if empty.
            if not os.listdir(session_dir):
                os.rmdir(session_dir)
    except Exception as e:
        logger.warning("Session cleanup failed: %s", e)

    logger.info("Session %s ended: extracted %d insights, deleted %d screenshots",
                session_id, len(knowledge), deleted)

    return {
        "status": "ended",
        "session_id": session_id,
        "captures": len(captures),
        "knowledge": knowledge,
        "deleted_screenshots": deleted,
    }


def _extract_session_knowledge(session_id: str, captures: list) -> list:
    """Extract high-level knowledge from session descriptions.
    Stores insights in memory as facts for future reference."""
    if not captures:
        return []

    # Collect unique descriptions.
    descriptions = []
    seen = set()
    for cap in captures:
        desc = cap.get("description", "").strip()
        if desc and desc not in seen:
            descriptions.append(desc)
            seen.add(desc)

    if not descriptions:
        return []

    # Combine descriptions into a session summary.
    summary_prompt = (
        "You are analyzing a trading/work session. Below are screen descriptions "
        "captured during the session. Extract the most important insights as "
        "bullet points (max 5). Focus on: errors encountered, patterns noticed, "
        "tasks completed, decisions made. Be concise.\n\n"
        "Screen descriptions:\n" + "\n".join(f"- {d}" for d in descriptions[:20])
    )

    insights = []
    try:
        from .llm import chat
        messages = [{"role": "user", "content": summary_prompt}]
        summary, _ = chat(messages, role="companion", temperature=0.3, max_tokens=500)
        # Parse bullet points.
        for line in summary.splitlines():
            line = line.strip()
            if line.startswith("- ") or line.startswith("• "):
                insights.append(line[2:].strip())
    except Exception as e:
        logger.warning("Session knowledge extraction failed: %s", e)
        insights = descriptions[:3]

    # Store insights in memory.
    try:
        from .memory import Memory
        mem = Memory(config.data_dir)
        for insight in insights:
            if insight:
                mem.remember_fact(f"Session {session_id}: {insight}")
    except Exception:
        pass

    return insights


def get_active_session() -> dict:
    """Get current session info or None."""
    with _SESSION_LOCK:
        if _CURRENT_SESSION[0]:
            return {
                "session_id": _CURRENT_SESSION[0]["id"],
                "start_time": _CURRENT_SESSION[0]["start_time"],
                "captures": len(_CURRENT_SESSION[0].get("captures", [])),
            }
    return None


# ── Local capture → brain eye store ──────────────────────────────────────────
def get_live_description() -> str:
    """Best current screen description for injecting into a turn."""
    from .eye import local_description
    return local_description()


def get_live_state() -> dict:
    """Merged + per-device live eye state (delegates to brain eye store)."""
    from .eye import get_state
    return get_state()


def has_live_eye() -> bool:
    from .eye import get_state
    return get_state().get("active", False)


def _downscale(img) -> Optional[bytes]:
    """Return a small JPEG of the screenshot (max ~640px wide) for cheap vision."""
    try:
        from PIL import Image
        import io
        w, h = img.size
        scale = min(1.0, 640.0 / w) if w else 1.0
        if scale < 1.0:
            img = img.resize((int(w * scale), int(h * scale)))
        buf = io.BytesIO()
        img.save(buf, format="JPEG", quality=55)
        return buf.getvalue()
    except Exception:
        return None


def _hash_bytes(b: bytes) -> str:
    import hashlib
    return hashlib.md5(b).hexdigest()


def _capture_frame() -> Optional[bytes]:
    """Screenshot + return downscaled JPEG bytes (or None on failure)."""
    try:
        import pyautogui
        img = pyautogui.screenshot()
        return _downscale(img)
    except Exception as e:
        logger.warning("screenshot failed: %s", e)
        return None


def _submit(jpeg: bytes) -> Optional[str]:
    """Submit a local frame to the brain eye store; returns description.
    Also saves the frame to the current session directory if active."""
    from .eye import submit_frame
    if not jpeg:
        return None
    img_b64 = base64.b64encode(jpeg).decode()
    res = submit_frame(_LIVE_DEVICE, img_b64, kind="screen")
    desc = res.get("description") or None

    # Save to current session directory if active.
    with _SESSION_LOCK:
        session = _CURRENT_SESSION[0]
        if session:
            try:
                session_dir = session["dir"]
                ts = int(time.time())
                path = os.path.join(session_dir, f"cap_{ts}_{_hash_bytes(jpeg)[:8]}.jpg")
                with open(path, "wb") as f:
                    f.write(jpeg)
                session.setdefault("captures", []).append({
                    "ts": ts,
                    "path": path,
                    "description": desc,
                })
            except Exception as e:
                logger.warning("Session capture save failed: %s", e)

    return desc


def describe_now() -> str:
    """Instantly capture + submit the screen (for an on-demand 'look now').
    Returns the description (may be '')."""
    jpeg = _capture_frame()
    return _submit(jpeg) or ""


def _should_offer(desc: str) -> bool:
    if not desc:
        return False
    d = desc.lower()
    if "nothing notable" in d:
        return False
    stuck_words = ["stuck", "error", "waiting", "blank", "failed",
                   "can't", "cannot", "blocked", "timeout", "half-done",
                   "incomplete", "stalled"]
    return any(w in d for w in stuck_words)


def _trading_copilot(desc: str) -> str | None:
    """Analyze screen description for trading context and return guidance.
    Detects chart setups, FOMO risks, and common mistakes."""
    if not desc:
        return None
    now = time.time()
    if now - _COPILOT_LAST_ADVICE[0] < _COPILOT_COOLDOWN:
        return None
    d = desc.lower()
    triggers = []
    # Detect potential FOMO / chasing price
    fomo_words = ["green candle", "strong move", "breaking out", "shooting up",
                  "dropping fast", "red candle", "crashing", "pumping"]
    if any(w in d for w in fomo_words):
        triggers.append("FOMO DETECTED: Market moving fast. Step back. Check your ORB setup before entering.")
    # Detect common mistakes
    mistakes = ["no stop loss", "no sl", "huge lot", "overleveraged", "margin call",
                "negative balance", "revenge trading", "averaging down"]
    if any(w in d for w in mistakes):
        triggers.append("WARNING: Common mistake pattern detected. Stick to your plan ($18 risk, 8 pip SL).")
    # Chart pattern detection
    chart_setups = ["range is forming", "consolidation", "sideways", "support",
                    "resistance", "retest", "double top", "double bottom"]
    if any(w in d for w in chart_setups):
        patterns = [w for w in chart_setups if w in d]
        triggers.append(f"Setup watch: {', '.join(patterns[:3])} visible on chart.")
    # Trading platform detection
    if "mt5" in d or "metatrader" in d or "trading" in d or "chart" in d:
        triggers.append("Trading session active — I'm watching with you.")
    if not triggers:
        return None
    _COPILOT_LAST_ADVICE[0] = now
    return " | ".join(triggers[:3])


def _loop(interval: int, allow_act: bool) -> None:
    logger.info("Screen watcher loop started (interval=%ss, allow_act=%s)",
                 interval, allow_act)
    while not _STOP.is_set():
        # Dynamic interval based on current mode
        current_interval = _RAPID_INTERVAL if _MODE == "rapid" else _SLOW_INTERVAL
        if _STOP.wait(timeout=current_interval):
            break
        # Skip when the user is already talking/executing - no wasted calls.
        if _ACTIVITY.get("busy"):
            continue
        img = _capture_frame()
        if not img:
            continue
        h = _hash_bytes(img)
        if h == _LAST_HASH[0]:
            continue  # screen unchanged -> spend nothing
        _LAST_HASH[0] = h

        desc = _submit(img)
        if not desc:
            continue

        # Trading co-pilot: in rapid mode, analyze for trading guidance
        if _MODE == "rapid":
            advice = _trading_copilot(desc)
            if advice:
                logger.info("Co-pilot: %s", advice)
                if _ON_RESULT:
                    try:
                        _ON_RESULT("copilot", advice)
                    except Exception:
                        pass

        # Keep the live eye fresh regardless of offer logic.
        if _should_offer(desc):
            logger.info("Watcher offers help: %s", desc[:120])
            if _ON_RESULT:
                try:
                    _ON_RESULT("screen", desc)
                except Exception:
                    pass
            if allow_act and config.confirm_destructive is not None:
                # Reserved hook: non-destructive proactive action would go here,
                # gated and behind confirmation. Default OFF.
                pass


def start(interval: int = None, allow_act: bool = None) -> None:
    """Start the watcher thread. No-op if disabled or already running."""
    if not getattr(config, "screen_watch", False):
        logger.info("Screen watcher disabled (SCREEN_WATCH=false).")
        return
    if _STOP.is_set():
        _STOP.clear()
    # Fast/trading mode uses a much shorter interval for near-real-time capture.
    if getattr(config, "screen_watch_fast", False):
        interval = interval or int(getattr(config, "screen_watch_fast_interval", 1))
    else:
        interval = interval or int(getattr(config, "screen_watch_interval", 20))
    allow_act = allow_act if allow_act is not None else getattr(
        config, "proactive_act", False)
    t = threading.Thread(
        target=_loop, args=(interval, allow_act), daemon=True)
    t.start()


def stop() -> None:
    _STOP.set()
