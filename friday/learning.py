"""
Friday Base - Autonomous Learning Engine

Learns from every interaction: captures patterns, user preferences, tool usage
effectiveness, and conversation strategies. Improves Friday over time without
manual retraining.

Key capabilities:
  1. Pattern Recognition - detects recurring user requests and optimal responses
  2. Skill Acquisition - learns which tools work best for which types of tasks
  3. Preference Learning - builds detailed user model from implicit signals
  4. Error Learning - remembers what went wrong and how to fix it
  5. Strategy Optimization - picks the best conversation strategy per context
"""
import json
import logging
import math
import os
import re
import threading
import time
from typing import Dict, List, Optional, Tuple

logger = logging.getLogger("Friday.Learning")

LEARNING_DIR = os.path.join(
    os.path.dirname(os.path.dirname(os.path.abspath(__file__))),
    "data", "learning"
)
os.makedirs(LEARNING_DIR, exist_ok=True)


class Pattern:
    def __init__(self, pattern_id: str, trigger: str, response_strategy: str,
                 confidence: float = 0.5, uses_tool: str = "", count: int = 1):
        self.id = pattern_id
        self.trigger = trigger
        self.response_strategy = response_strategy
        self.confidence = confidence
        self.uses_tool = uses_tool
        self.count = count
        self.last_seen = time.time()
        self.first_seen = time.time()

    def to_dict(self):
        return {
            "id": self.id, "trigger": self.trigger,
            "response_strategy": self.response_strategy,
            "confidence": self.confidence, "uses_tool": self.uses_tool,
            "count": self.count, "last_seen": self.last_seen,
            "first_seen": self.first_seen,
        }

    @classmethod
    def from_dict(cls, d):
        p = cls(d["id"], d["trigger"], d["response_strategy"],
                d.get("confidence", 0.5), d.get("uses_tool", ""),
                d.get("count", 1))
        p.last_seen = d.get("last_seen", time.time())
        p.first_seen = d.get("first_seen", time.time())
        return p


class Skill:
    def __init__(self, name: str, category: str, description: str,
                 prerequisites: List[str] = None):
        self.name = name
        self.category = category
        self.description = description
        self.prerequisites = prerequisites or []
        self.mastery = 0.0
        self.attempts = 0
        self.successes = 0

    def to_dict(self):
        return {
            "name": self.name, "category": self.category,
            "description": self.description,
            "prerequisites": self.prerequisites,
            "mastery": self.mastery, "attempts": self.attempts,
            "successes": self.successes,
        }

    @classmethod
    def from_dict(cls, d):
        s = cls(d["name"], d["category"], d["description"], d.get("prerequisites", []))
        s.mastery = d.get("mastery", 0.0)
        s.attempts = d.get("attempts", 0)
        s.successes = d.get("successes", 0)
        return s


class LearningEngine:
    def __init__(self):
        self._lock = threading.Lock()
        self.patterns: Dict[str, Pattern] = {}
        self.skills: Dict[str, Skill] = {}
        self.user_preferences: Dict[str, float] = {}
        self.user_routines: List[Dict] = []
        self.interaction_log: List[Dict] = []
        self._load()

    def _path(self, name: str) -> str:
        return os.path.join(LEARNING_DIR, f"{name}.json")

    def _load(self):
        for fname, attr in [
            ("patterns", "patterns"), ("skills", "skills"),
            ("preferences", "user_preferences"),
            ("routines", "user_routines"),
        ]:
            try:
                with open(self._path(fname), "r", encoding="utf-8") as f:
                    data = json.load(f)
                if fname == "patterns":
                    self.patterns = {k: Pattern.from_dict(v) for k, v in data.items()}
                elif fname == "skills":
                    self.skills = {k: Skill.from_dict(v) for k, v in data.items()}
                elif fname == "preferences":
                    self.user_preferences = data
                elif fname == "routines":
                    self.user_routines = data
            except Exception:
                pass

    def _save(self, name: str, data):
        try:
            with open(self._path(name), "w", encoding="utf-8") as f:
                json.dump(data, f, ensure_ascii=False, indent=2)
        except Exception as e:
            logger.warning(f"Learning save {name} failed: {e}")

    def _save_all(self):
        with self._lock:
            self._save("patterns", {k: v.to_dict() for k, v in self.patterns.items()})
            self._save("skills", {k: v.to_dict() for k, v in self.skills.items()})
            self._save("preferences", self.user_preferences)
            self._save("routines", self.user_routines)

    def observe_interaction(self, user_input: str, response: str,
                            tool_used: str = "", success: bool = True,
                            latency: float = 0.0):
        """Learn from each user interaction."""
        with self._lock:
            entry = {
                "input": user_input[:200],
                "response": response[:200],
                "tool": tool_used,
                "success": success,
                "latency": latency,
                "ts": time.time(),
            }
            self.interaction_log.append(entry)
            if len(self.interaction_log) > 500:
                self.interaction_log = self.interaction_log[-400:]

        self._learn_pattern(user_input, response, tool_used, success)
        self._learn_preference(user_input, response, tool_used)
        self._update_skill_mastery(tool_used, success)

    def _learn_pattern(self, user_input: str, response: str,
                       tool: str, success: bool):
        if not success or not user_input:
            return
        key_words = [w for w in re.findall(r'\w+', user_input.lower()) if len(w) > 3]
        if len(key_words) < 2:
            return
        trigger = " ".join(sorted(set(key_words[:5])))
        strategy = "direct" if not tool else f"tool_{tool}"

        with self._lock:
            if trigger in self.patterns:
                p = self.patterns[trigger]
                p.count += 1
                p.confidence = min(0.95, p.confidence + 0.05)
                p.last_seen = time.time()
            else:
                pid = f"p_{int(time.time())}_{len(self.patterns)}"
                conf = 0.3 if len(self.patterns) < 10 else 0.15
                self.patterns[trigger] = Pattern(pid, trigger, strategy, conf, tool)
            self._save("patterns", {k: v.to_dict() for k, v in self.patterns.items()})

    def _learn_preference(self, user_input: str, response: str, tool: str):
        low = user_input.lower()
        signals = {
            "concise": any(w in low for w in ["short", "brief", "quick", "summarize", "tl;dr"]),
            "detailed": any(w in low for w in ["detailed", "explain", "elaborate", "more detail"]),
            "speed": any(w in low for w in ["fast", "quick", "hurry", "urgent"]),
            "code": any(w in low for w in ["code", "python", "script", "function"]),
            "search": any(w in low for w in ["search", "find", "look up", "google"]),
        }
        with self._lock:
            for pref, detected in signals.items():
                if detected:
                    current = self.user_preferences.get(pref, 0.5)
                    self.user_preferences[pref] = min(1.0, current + 0.05)
            self._save("preferences", self.user_preferences)

    def _update_skill_mastery(self, tool: str, success: bool):
        if not tool:
            return
        with self._lock:
            if tool not in self.skills:
                self.skills[tool] = Skill(tool, "tool", f"Using {tool} tool")
            skill = self.skills[tool]
            skill.attempts += 1
            if success:
                skill.successes += 1
            ratio = skill.successes / max(1, skill.attempts)
            skill.mastery = ratio * min(1.0, skill.attempts / 10)
            self._save("skills", {k: v.to_dict() for k, v in self.skills.items()})

    def suggest_strategy(self, user_input: str) -> Dict:
        """Suggest the best response strategy based on learned patterns."""
        low = user_input.lower()
        key_words = set(w for w in re.findall(r'\w+', low) if len(w) > 3)
        if not key_words:
            return {"strategy": "chat", "confidence": 0.5, "suggested_tool": ""}

        best_match = None
        best_score = 0.0
        with self._lock:
            for trigger, pattern in self.patterns.items():
                p_words = set(trigger.split())
                if not p_words:
                    continue
                overlap = len(key_words & p_words)
                if overlap == 0:
                    continue
                score = (overlap / math.sqrt(len(key_words) * len(p_words))) * pattern.confidence
                if score > best_score:
                    best_score = score
                    best_match = pattern

        strategy = "chat"
        tool = ""
        confidence = 0.5
        if best_match and best_score > 0.3:
            strategy = best_match.response_strategy
            tool = best_match.uses_tool
            confidence = min(best_match.confidence, best_score)

        return {
            "strategy": strategy,
            "confidence": confidence,
            "suggested_tool": tool,
            "pattern_match": best_match.trigger if best_match else "",
        }

    def get_user_preference(self, key: str, default: float = 0.5) -> float:
        with self._lock:
            return self.user_preferences.get(key, default)

    def get_skill_mastery(self, tool: str) -> float:
        with self._lock:
            skill = self.skills.get(tool)
            return skill.mastery if skill else 0.0

    def get_top_skills(self, n: int = 5) -> List[Dict]:
        with self._lock:
            sorted_skills = sorted(
                self.skills.values(),
                key=lambda s: s.mastery,
                reverse=True
            )
            return [s.to_dict() for s in sorted_skills[:n]]

    def get_stats(self) -> Dict:
        with self._lock:
            return {
                "patterns_learned": len(self.patterns),
                "skills_developed": len(self.skills),
                "preferences_detected": len(self.user_preferences),
                "total_interactions": len(self.interaction_log),
                "top_preferences": dict(sorted(
                    self.user_preferences.items(),
                    key=lambda x: x[1], reverse=True
                )[:5]),
            }


_engine: Optional[LearningEngine] = None
_engine_lock = threading.Lock()


def get_engine() -> LearningEngine:
    global _engine
    if _engine is None:
        with _engine_lock:
            if _engine is None:
                _engine = LearningEngine()
    return _engine


def observe(user_input: str, response: str, tool: str = "",
            success: bool = True, latency: float = 0.0):
    get_engine().observe_interaction(user_input, response, tool, success, latency)


def suggest_strategy(user_input: str) -> Dict:
    return get_engine().suggest_strategy(user_input)


def get_stats() -> Dict:
    return get_engine().get_stats()
