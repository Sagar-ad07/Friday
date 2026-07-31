"""
Friday — Self Model
Persistent, evolving understanding of the user and of Friday himself.
This is the "grows over time" layer: every interaction can update it,
and it is consulted before Friday acts so his behavior becomes more
aligned with the user the longer they work together.

Design rules (safety):
  * Read/write only under config.data_dir/self_model.json
  * Never deletes user facts without explicit user request
  * Learned preferences are suggestions; the user can override anytime
  * All mutations are append-only to an audit log
"""
import json
import os
import threading
import time
from typing import Any, Dict, List, Optional

try:
    from friday.config import config
    _DATA_DIR = getattr(config, "data_dir", None)
except Exception:
    _DATA_DIR = None

if _DATA_DIR is None:
    _DATA_DIR = os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "data")

_MODEL_PATH = os.path.join(_DATA_DIR, "self_model.json")
_AUDIT_PATH = os.path.join(_DATA_DIR, "self_model_audit.log")

_lock = threading.Lock()

_DEFAULT = {
    "version": 1,
    "created": None,
    "updated": None,
    "user": {
        "name": None,
        "salutation": "sir",
        "traits": [],          # observed working style, e.g. "prefers concise answers"
        "preferences": {},     # key -> value, e.g. {"ui_theme": "light"}
        "goals": [],           # what they're trying to achieve
        "do_not": [],          # explicit boundaries, e.g. "don't trade without asking"
        "expertise": {},       # domain -> level, e.g. {"trading": "beginner"}
    },
    "friday": {
        "identity": "Friday",
        "role": "Personal autonomous assistant",
        "capabilities": [],    # learned over time
        "limits": [],          # things he should not do
        "voice": "polite, direct, loyal",
        "active_bots": [],     # bot_id -> status tracking
        "total_earnings": 0.0, # cumulative bot earnings
        "last_self_review": None,
    },
    "relationship": {
        "trust_level": 0.2,    # 0..1, grows with successful interactions
        "interactions": 0,
        "inside_refs": [],     # shared context built over time
    },
    "learnings": [],           # {"ts":, "type":, "note":, "confidence":}
}


def _now() -> str:
    return time.strftime("%Y-%m-%dT%H:%M:%S", time.gmtime())


def _load() -> Dict[str, Any]:
    try:
        if os.path.isfile(_MODEL_PATH):
            with open(_MODEL_PATH, "r", encoding="utf-8") as f:
                data = json.load(f)
            # shallow-merge defaults to tolerate older files
            merged = json.loads(json.dumps(_DEFAULT))
            merged.update(data)
            for k in ("user", "friday", "relationship"):
                if k in data and isinstance(data[k], dict):
                    merged[k].update(data[k])
            return merged
    except Exception:
        pass
    d = json.loads(json.dumps(_DEFAULT))
    d["created"] = _now()
    return d


def _save(data: Dict[str, Any]):
    os.makedirs(_DATA_DIR, exist_ok=True)
    data["updated"] = _now()
    with open(_MODEL_PATH, "w", encoding="utf-8") as f:
        json.dump(data, f, indent=2, ensure_ascii=False)


def _audit(action: str, detail: str):
    try:
        os.makedirs(_DATA_DIR, exist_ok=True)
        with open(_AUDIT_PATH, "a", encoding="utf-8") as f:
            f.write(f"{_now()} | {action} | {detail}\n")
    except Exception:
        pass


# ── Public API ───────────────────────────────────────
def get_model() -> Dict[str, Any]:
    with _lock:
        return _load()


def get_user() -> Dict[str, Any]:
    return get_model()["user"]


def get_friday() -> Dict[str, Any]:
    return get_model()["friday"]


def get_relationship() -> Dict[str, Any]:
    return get_model()["relationship"]


def record_interaction(note: Optional[str] = None):
    """Call once per meaningful interaction; nudges trust + count."""
    with _lock:
        m = _load()
        m["relationship"]["interactions"] += 1
        # trust grows slowly, capped
        lvl = m["relationship"]["trust_level"]
        m["relationship"]["trust_level"] = min(1.0, round(lvl + 0.01, 3))
        if note:
            m["learnings"].append({
                "ts": _now(), "type": "interaction", "note": note, "confidence": 0.5,
            })
        _save(m)
        _audit("interaction", note or "")


def set_user_fact(field: str, value: Any, audit: bool = True):
    """Update a top-level user field (name, salutation, traits...)."""
    with _lock:
        m = _load()
        m["user"][field] = value
        _save(m)
        if audit:
            _audit("user_fact", f"{field}={value}")


def add_user_trait(trait: str):
    with _lock:
        m = _load()
        if trait not in m["user"]["traits"]:
            m["user"]["traits"].append(trait)
            _save(m)
            _audit("user_trait", trait)


def add_user_goal(goal: str):
    with _lock:
        m = _load()
        if goal not in m["user"]["goals"]:
            m["user"]["goals"].append(goal)
            _save(m)
            _audit("user_goal", goal)


def set_preference(key: str, value: Any):
    with _lock:
        m = _load()
        m["user"]["preferences"][key] = value
        _save(m)
        _audit("preference", f"{key}={value}")


def add_boundary(boundary: str):
    """Explicit 'do not' from the user — high priority, never auto-removed."""
    with _lock:
        m = _load()
        if boundary not in m["user"]["do_not"]:
            m["user"]["do_not"].append(boundary)
            _save(m)
            _audit("boundary", boundary)


def learn(note: str, confidence: float = 0.5):
    """Record a general learning with confidence (0..1)."""
    with _lock:
        m = _load()
        m["learnings"].append({"ts": _now(), "type": "learning", "note": note, "confidence": confidence})
        # cap history
        if len(m["learnings"]) > 500:
            m["learnings"] = m["learnings"][-500:]
        _save(m)
        _audit("learn", f"{confidence:.2f} {note}")


def should_avoid(text: str) -> Optional[str]:
    """If `text` relates to a known user boundary, return that boundary.

    Matches if the boundary string is a substring OR if they share a strong
    topic keyword (trade, trade live, delete, format disk, etc.) — so Friday
    respects boundaries even when phrased differently.
    """
    m = get_model()
    low = text.lower()
    for b in m["user"]["do_not"]:
        bl = b.lower()
        if bl in low:
            return b
        # topic-keyword overlap check
        topics = ["trade", "trading", "live", "delete", "remove", "format",
                  "send", "transfer", "pay", "buy", "sell", "invest", "risk"]
        if any(t in bl and t in low for t in topics):
            return b
    return None


# ── Capabilities & Bot Tracking ──

def set_capability(cap_id: str, name: str):
    with _lock:
        m = _load()
        existing = [c for c in m["friday"]["capabilities"] if c.get("id") == cap_id]
        if not existing:
            m["friday"]["capabilities"].append({"id": cap_id, "name": name})
            _save(m)
            _audit("capability", f"{cap_id}: {name}")


def remove_capability(cap_id: str):
    with _lock:
        m = _load()
        m["friday"]["capabilities"] = [c for c in m["friday"]["capabilities"] if c.get("id") != cap_id]
        _save(m)
        _audit("capability_removed", cap_id)


def refresh_capabilities():
    """Sync all capabilities from the registry into self-model."""
    from friday.capabilities import get_capabilities
    registered = {c["id"]: c["name"] for c in get_capabilities()}
    with _lock:
        m = _load()
        m["friday"]["capabilities"] = [{"id": cid, "name": cname} for cid, cname in registered.items()]
        _save(m)
        _audit("capabilities_refresh", f"{len(registered)} capabilities loaded")


def set_friday_limit(limit: str):
    with _lock:
        m = _load()
        if limit not in m["friday"]["limits"]:
            m["friday"]["limits"].append(limit)
            _save(m)
            _audit("friday_limit", limit)


def remove_friday_limit(limit: str):
    with _lock:
        m = _load()
        m["friday"]["limits"] = [l for l in m["friday"]["limits"] if l != limit]
        _save(m)
        _audit("friday_limit_removed", limit)


def track_bot(bot_id: str, name: str, status: str = "starting"):
    with _lock:
        m = _load()
        bots = [b for b in m["friday"]["active_bots"] if b.get("id") != bot_id]
        bots.append({"id": bot_id, "name": name, "status": status})
        m["friday"]["active_bots"] = bots
        _save(m)
        _audit("bot_track", f"{bot_id}: {name}")


def untrack_bot(bot_id: str):
    with _lock:
        m = _load()
        m["friday"]["active_bots"] = [b for b in m["friday"]["active_bots"] if b.get("id") != bot_id]
        _save(m)
        _audit("bot_untrack", bot_id)


def update_earnings(amount: float):
    with _lock:
        m = _load()
        m["friday"]["total_earnings"] = round(m["friday"]["total_earnings"] + amount, 2)
        _save(m)
        _audit("earnings", f"+{amount:.2f}")


def summarize() -> str:
    m = get_model()
    u = m["user"]
    f = m["friday"]
    r = m["relationship"]
    caps = [c.get("name", c.get("id")) for c in f.get("capabilities", [])]
    bots = f.get("active_bots", [])
    lines = [
        f"Friday self-model (updated {m.get('updated')}):",
        f"  User: {u.get('name') or 'unknown'} (calls you '{u.get('salutation')}')",
        f"  Traits: {', '.join(u.get('traits') or []) or 'none yet'}",
        f"  Goals: {', '.join(u.get('goals') or []) or 'none yet'}",
        f"  Boundaries: {', '.join(u.get('do_not') or []) or 'none'}",
        f"  Trust: {r.get('trust_level')} | Interactions: {r.get('interactions')}",
        f"  Capabilities: {', '.join(caps) if caps else 'none'}",
        f"  Limits: {', '.join(f.get('limits') or []) or 'none'}",
        f"  Active bots: {len(bots)} — total earnings: ${f.get('total_earnings', 0):.2f}",
        f"  Learnings recorded: {len(m.get('learnings', []))}",
    ]
    return "\n".join(lines)


if __name__ == "__main__":
    print(summarize())
