"""
Friday Base - Ambient Intelligence Background Loop

Wires together screen_watcher, anticipate, and proactive into a continuous
background loop that observes the user's context and offers timely, non-intrusive
suggestions.

Loop cadence: every 30 seconds (configurable via AMBIENT_LOOP_INTERVAL)
Triggers:
  1. Screen changed (detected by screen_watcher)
  2. User idle (no conversation for N seconds)
  3. Time-of-day patterns (morning check-in, evening wrap-up)
  4. Task completion (after agentic_run finishes)

When a suggestion with confidence > 0.7 is generated, it's pushed as a
non-intrusive notification via proactive.announce().
"""
import logging
import threading
import time
from typing import Dict, List, Optional

from .config import config

logger = logging.getLogger("Friday.Ambient")

_AMBIENT_LOOP_INTERVAL = getattr(config, "ambient_loop_interval", 30)
_CONFIDENCE_THRESHOLD = getattr(config, "ambient_confidence_threshold", 0.7)

_STOP = threading.Event()
_THREAD: Optional[threading.Thread] = None
_LAST_SCREEN_HASH: Optional[str] = None
_LAST_CONVERSATION_TURN: float = 0.0
_CONVERSATION_TURNS: int = 0
_LAST_SUGGESTION_TIME: float = 0.0
_SUGGESTION_COOLDOWN = 120  # Don't repeat similar suggestions within 2 min


def _get_screen_hash() -> Optional[str]:
    """Get current screen hash from screen_watcher."""
    try:
        from . import screen_watcher as sw
        if sw._LAST_HASH[0] is not None:
            return sw._LAST_HASH[0]
    except Exception:
        pass
    return None


def _is_user_busy() -> bool:
    """Check if user is mid-conversation or executing."""
    try:
        from . import screen_watcher as sw
        return sw._ACTIVITY.get("busy", False)
    except Exception:
        return False


def _describe_screen() -> str:
    """Get a description of the current screen context."""
    try:
        from . import screen_watcher as sw
        # Use the last captured frame's description if available
        session = sw._CURRENT_SESSION[0]
        if session and session.get("captures"):
            last_capture = session["captures"][-1]
            if isinstance(last_capture, dict) and "description" in last_capture:
                return last_capture["description"]
    except Exception:
        pass
    return ""


def _generate_suggestions(user_text: str = "", screen_context: str = "",
                          conversation_state: str = "idle") -> List[Dict]:
    """Generate proactive suggestions based on current context."""
    suggestions = []

    # Time-based suggestions
    now = time.time()
    hour = time.localtime(now).tm_hour

    # Morning check-in (8-10 AM)
    if 8 <= hour <= 10 and now - _LAST_SUGGESTION_TIME > _SUGGESTION_COOLDOWN:
        suggestions.append({
            "text": "Good morning! Want me to check your schedule for today?",
            "confidence": 0.8,
            "type": "time_based",
            "action": "check_schedule"
        })

    # Evening wrap-up (6-8 PM)
    elif 18 <= hour <= 20 and now - _LAST_SUGGESTION_TIME > _SUGGESTION_COOLDOWN:
        suggestions.append({
            "text": "Evening check-in - anything you need to wrap up today?",
            "confidence": 0.75,
            "type": "time_based",
            "action": "wrap_up"
        })

    # Screen-based suggestions
    if screen_context:
        screen_lower = screen_context.lower()

        # Debugging context
        if any(w in screen_lower for w in ["error", "traceback", "exception", "500"]):
            suggestions.append({
                "text": "I see an error on screen. Want me to check the logs and suggest a fix?",
                "confidence": 0.85,
                "type": "screen_based",
                "action": "debug_error"
            })

        # Terminal context
        elif any(w in screen_lower for w in ["terminal", "command", "bash", "shell"]):
            if now - _LAST_SUGGESTION_TIME > _SUGGESTION_COOLDOWN:
                suggestions.append({
                    "text": "You're in a terminal. Need me to run a command or explain something?",
                    "confidence": 0.7,
                    "type": "screen_based",
                    "action": "terminal_help"
                })

        # Code editor context
        elif any(w in screen_lower for w in ["code", "python", "javascript", "function", "class"]):
            suggestions.append({
                "text": "You're coding. Want me to review this code or write a test?",
                "confidence": 0.75,
                "type": "screen_based",
                "action": "code_review"
            })

    # Conversation-based suggestions
    if _CONVERSATION_TURNS > 0 and now - _LAST_CONVERSATION_TURN > 60:
        suggestions.append({
            "text": "Want me to do anything else with what we just discussed?",
            "confidence": 0.65,
            "type": "conversation_based",
            "action": "follow_up"
        })

    # Idle suggestions (long time since last interaction)
    idle_time = now - _LAST_CONVERSATION_TURN
    if idle_time > 300 and _CONVERSATION_TURNS > 0:  # 5+ minutes idle
        suggestions.append({
            "text": "I'm here if you need anything. Want me to summarize our conversation?",
            "confidence": 0.6,
            "type": "idle",
            "action": "summarize"
        })

    return suggestions


def _push_notification(suggestion: Dict) -> None:
    """Push a suggestion as a non-intrusive notification."""
    global _LAST_SUGGESTION_TIME
    try:
        from . import proactive as pro
        notif_type = "question" if suggestion.get("action") in ("debug_error", "follow_up") else "info"
        pro.announce(suggestion["text"], notif_type)
        _LAST_SUGGESTION_TIME = time.time()
        logger.info("Ambient suggestion pushed: %s", suggestion["text"])
    except Exception as e:
        logger.warning("Failed to push ambient notification: %s", e)


def _ambient_loop() -> None:
    """Main background loop."""
    global _LAST_SCREEN_HASH

    while not _STOP.is_set():
        try:
            # Skip if user is busy (mid-conversation or executing)
            if _is_user_busy():
                time.sleep(_AMBIENT_LOOP_INTERVAL)
                continue

            # Check if screen changed
            current_hash = _get_screen_hash()
            screen_changed = current_hash != _LAST_SCREEN_HASH
            if screen_changed:
                _LAST_SCREEN_HASH = current_hash

            # Only generate suggestions if screen changed or periodic check
            if screen_changed or time.time() - _LAST_SUGGESTION_TIME > 300:
                screen_context = _describe_screen() if screen_changed else ""

                suggestions = _generate_suggestions(
                    user_text="",
                    screen_context=screen_context,
                    conversation_state="idle"
                )

                # Push highest confidence suggestion
                if suggestions:
                    best = max(suggestions, key=lambda s: s["confidence"])
                    if best["confidence"] >= _CONFIDENCE_THRESHOLD:
                        _push_notification(best)

        except Exception as e:
            logger.error("Ambient loop error: %s", e)

        # Wait for next cycle or stop
        _STOP.wait(_AMBIENT_LOOP_INTERVAL)


def start_ambient_loop() -> None:
    """Start the ambient intelligence background loop."""
    global _THREAD
    if _THREAD is not None and _THREAD.is_alive():
        logger.info("Ambient loop already running")
        return

    _STOP.clear()
    _THREAD = threading.Thread(target=_ambient_loop, daemon=True, name="AmbientLoop")
    _THREAD.start()
    logger.info("Ambient intelligence loop started (interval=%ss)", _AMBIENT_LOOP_INTERVAL)


def stop_ambient_loop() -> None:
    """Stop the ambient intelligence background loop."""
    _STOP.set()
    if _THREAD is not None:
        _THREAD.join(timeout=5)
    logger.info("Ambient intelligence loop stopped")


def record_conversation_turn(user_text: str) -> None:
    """Record a conversation turn for idle detection."""
    global _LAST_CONVERSATION_TURN, _CONVERSATION_TURNS
    _LAST_CONVERSATION_TURN = time.time()
    _CONVERSATION_TURNS += 1


def get_ambient_status() -> dict:
    """Get current ambient intelligence status."""
    return {
        "running": _THREAD is not None and _THREAD.is_alive(),
        "last_screen_hash": _LAST_SCREEN_HASH,
        "last_conversation_turn": _LAST_CONVERSATION_TURN,
        "conversation_turns": _CONVERSATION_TURNS,
        "last_suggestion_time": _LAST_SUGGESTION_TIME,
        "interval": _AMBIENT_LOOP_INTERVAL,
        "confidence_threshold": _CONFIDENCE_THRESHOLD,
    }
