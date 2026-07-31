"""
Friday Base - Phone Agent Configuration
Manages per-agent identity: name, voice, personality, role.
Each phone agent gets its own config stored on the server.
"""
import json
import logging
import os
import time
from typing import Dict, Optional

logger = logging.getLogger("Friday.PhoneAgents")

# Default config directory
_CONFIG_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "data", "phone_agents")
os.makedirs(_CONFIG_DIR, exist_ok=True)


def _path(agent_id: str) -> str:
    return os.path.join(_CONFIG_DIR, f"{agent_id}.json")


def _default_config(agent_id: str) -> dict:
    return {
        "agent_id": agent_id,
        "name": "Friday",
        "voice": "en-IN-NeerjaNeural",
        "personality": "warm",
        "role": "companion",
        "platform": "unknown",
        "created_at": time.time(),
        "updated_at": time.time(),
    }


def get_config(agent_id: str) -> dict:
    """Get agent config, creating default if missing."""
    path = _path(agent_id)
    try:
        with open(path, "r", encoding="utf-8") as f:
            cfg = json.load(f)
        cfg.setdefault("agent_id", agent_id)
        cfg.setdefault("name", "Friday")
        cfg.setdefault("voice", "en-IN-NeerjaNeural")
        cfg.setdefault("personality", "warm")
        cfg.setdefault("role", "companion")
        return cfg
    except Exception:
        cfg = _default_config(agent_id)
        save_config(agent_id, cfg)
        return cfg


def save_config(agent_id: str, cfg: dict) -> dict:
    """Save agent config."""
    cfg["agent_id"] = agent_id
    cfg["updated_at"] = time.time()
    path = _path(agent_id)
    try:
        with open(path, "w", encoding="utf-8") as f:
            json.dump(cfg, f, ensure_ascii=False, indent=2)
        logger.info("Saved config for agent %s", agent_id)
        return cfg
    except Exception as e:
        logger.error("Failed to save config for %s: %s", agent_id, e)
        return cfg


def update_config(agent_id: str, updates: dict) -> dict:
    """Update specific fields in agent config."""
    cfg = get_config(agent_id)
    allowed = {"name", "voice", "personality", "role", "platform"}
    for k, v in updates.items():
        if k in allowed and v is not None:
            cfg[k] = v
    return save_config(agent_id, cfg)


def list_agents() -> list:
    """List all configured agents."""
    agents = []
    try:
        for fname in os.listdir(_CONFIG_DIR):
            if fname.endswith(".json"):
                try:
                    with open(os.path.join(_CONFIG_DIR, fname), "r", encoding="utf-8") as f:
                        data = json.load(f)
                    agents.append({
                        "agent_id": data.get("agent_id", fname[:-5]),
                        "name": data.get("name", "Friday"),
                        "platform": data.get("platform", "unknown"),
                        "role": data.get("role", "companion"),
                    })
                except Exception:
                    pass
    except Exception:
        pass
    return agents
