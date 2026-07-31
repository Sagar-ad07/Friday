"""
Friday Base - Enhanced Episodic Memory with Semantic Retrieval
Stores full interaction episodes with embeddings + outcome tracking.
Hybrid retrieval: semantic + keyword + temporal + outcome-based.
"""
import json
import os
import threading
import time
import re
import math
import hashlib
from typing import Callable, Dict, List, Optional, Any
from dataclasses import dataclass, field, asdict
from datetime import datetime

try:
    from sentence_transformers import SentenceTransformer
    _EMBEDDINGS_AVAILABLE = True
except Exception:
    _EMBEDDINGS_AVAILABLE = False

_EMBEDDER = None
_EMBEDDER_LOCK = threading.Lock()


def _get_embedder():
    global _EMBEDDER
    if _EMBEDDER is None and _EMBEDDINGS_AVAILABLE:
        with _EMBEDDER_LOCK:
            if _EMBEDDER is None:
                try:
                    _EMBEDDER = SentenceTransformer("all-MiniLM-L6-v2")
                except Exception:
                    pass
    return _EMBEDDER


def _embed(text: str) -> List[float]:
    model = _get_embedder()
    if model is None:
        return []
    try:
        return model.encode(text, normalize_embeddings=True).tolist()
    except Exception:
        return []


def _cosine_sim(a: List[float], b: List[float]) -> float:
    if not a or not b or len(a) != len(b):
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    norm_a = math.sqrt(sum(x * x for x in a))
    norm_b = math.sqrt(sum(y * y for y in b))
    if norm_a == 0.0 or norm_b == 0.0:
        return 0.0
    return dot / (norm_a * norm_b)


def _keyword_score(query: str, text: str) -> float:
    q_words = set(re.findall(r'\w+', query.lower()))
    t_words = set(re.findall(r'\w+', text.lower()))
    if not q_words or not t_words:
        return 0.0
    common = q_words & t_words
    return len(common) / math.sqrt(len(q_words) * len(t_words))


@dataclass
class Episode:
    """Full interaction episode with outcome tracking."""
    id: str
    ts: float
    user_input: str
    assistant_response: str
    tools_used: List[Dict] = field(default_factory=list)
    tools_outcome: Dict = field(default_factory=dict)
    success: bool = True
    error: str = ""
    duration_ms: float = 0.0
    context_summary: str = ""
    tags: List[str] = field(default_factory=list)
    embedding: List[float] = field(default_factory=list)
    access_count: int = 0
    last_accessed: float = 0.0
    importance: float = 1.0

    def to_text(self) -> str:
        """Full text representation for embedding."""
        parts = [f"User: {self.user_input}", f"Friday: {self.assistant_response}"]
        if self.tools_used:
            tools_desc = ", ".join(f"{t.get('name','?')}({t.get('args','')})" for t in self.tools_used)
            parts.append(f"Tools: {tools_desc}")
        if self.error:
            parts.append(f"Error: {self.error}")
        return "\n".join(parts)

    def outcome_score(self) -> float:
        """Outcome-based score for retrieval ranking."""
        if self.success:
            return 1.0
        return 0.0


class EpisodicMemory:
    """Enhanced episodic memory with hybrid retrieval + auto-summarization."""

    def __init__(self, data_dir: str, max_episodes: int = 5000, summarize_every: int = 20):
        self.dir = data_dir
        self.max_episodes = max_episodes
        self.summarize_every = summarize_every
        self._episode_counter = 0
        os.makedirs(data_dir, exist_ok=True)
        self._lock = threading.RLock()
        self.episodes: List[Episode] = []
        self._path = os.path.join(data_dir, "episodic_enhanced.json")
        self._load()

    def _load(self):
        try:
            with open(self._path, "r", encoding="utf-8") as f:
                data = json.load(f)
            self.episodes = []
            for ep_data in data.get("episodes", []):
                ep = Episode(**ep_data)
                self.episodes.append(ep)
        except Exception:
            self.episodes = []

    def _save(self):
        try:
            with self._lock:
                data = {"episodes": [asdict(ep) for ep in self.episodes]}
                tmp_path = self._path + ".tmp"
                with open(tmp_path, "w", encoding="utf-8") as f:
                    json.dump(data, f, ensure_ascii=False, indent=2)
                os.replace(tmp_path, self._path)
        except Exception as e:
            print(f"[episodic] save failed: {e}")

    def add_episode(self, user_input: str, assistant_response: str,
                    tools_used: List[Dict] = None, tools_outcome: Dict = None,
                    success: bool = True, error: str = "",
                    duration_ms: float = 0.0, context_summary: str = "",
                    tags: List[str] = None) -> Episode:
        """Store a complete interaction episode."""
# Generate embedding OUTSIDE lock to avoid blocking
        embedding = []
        if _EMBEDDINGS_AVAILABLE:
            text_for_embed = f"User: {user_input}\nFriday: {assistant_response}"
            if tools_used:
                text_for_embed += f"\nTools: {', '.join(t.get('name', '?') for t in tools_used)}"
            embedding = _embed(text_for_embed)

        with self._lock:
            episode = Episode(
                id=hashlib.md5(f"{user_input}{time.time()}".encode()).hexdigest()[:12],
                ts=time.time(),
                user_input=user_input,
                assistant_response=assistant_response,
                tools_used=tools_used or [],
                tools_outcome=tools_outcome or {},
                success=success,
                error=error or "",
                duration_ms=duration_ms,
                context_summary=context_summary,
                tags=tags or [],
                embedding=embedding,
            )

            self.episodes.append(episode)
            self._episode_counter += 1

            # Trigger summarization every N episodes
            if self._episode_counter >= self.summarize_every:
                self._episode_counter = 0
                # Create summary outside lock to avoid blocking
                summary_text = self._create_summary_internal()
                if summary_text:
                    summary_episode = Episode(
                        id=hashlib.md5(f"summary{time.time()}".encode()).hexdigest()[:12],
                        ts=time.time(),
                        user_input="[AUTO-SUMMARY]",
                        assistant_response=summary_text,
                        tools_used=[],
                        tools_outcome={},
                        success=True,
                        error="",
                        duration_ms=0.0,
                        context_summary="Auto-generated summary of recent interactions",
                        tags=["auto-summary"],
                        importance=2.0,  # Higher importance for summaries
                    )
                    if _EMBEDDINGS_AVAILABLE:
                        summary_episode.embedding = _embed(summary_text)
                    self.episodes.append(summary_episode)

            # Prune old episodes (keep summaries + recent)
            if len(self.episodes) > self.max_episodes:
                # Keep all summary episodes + most recent regular episodes
                summaries = [ep for ep in self.episodes if "auto-summary" in ep.tags]
                regular = [ep for ep in self.episodes if "auto-summary" not in ep.tags]
                keep_regular = regular[-(self.max_episodes - len(summaries)):]
                self.episodes = summaries + keep_regular

            self._save()
            return episode

    def _create_summary_internal(self) -> str:
        """Create a concise summary of recent episodes for auto-summarization."""
        if not self.episodes:
            return ""
        
        # Get recent non-summary episodes
        recent = [ep for ep in self.episodes if "auto-summary" not in ep.tags]
        if len(recent) < self.summarize_every // 2:
            return ""
        
        recent = recent[-self.summarize_every:]
        
        # Build conversation text
        parts = []
        for ep in recent:
            parts.append(f"User: {ep.user_input}")
            parts.append(f"Friday: {ep.assistant_response[:200]}")
            if ep.tools_used:
                tools = ", ".join(t.get('name', '?') for t in ep.tools_used)
                parts.append(f"Tools: {tools}")
        
        text = "\n".join(parts)
        
        # Use LLM to summarize if available
        try:
            from . import llm
            messages = [
                {"role": "system", "content": "Summarize the following interaction history into ONE concise bullet point (max 30 words). Focus on what was accomplished, key decisions, and any issues resolved."},
                {"role": "user", "content": text}
            ]
            summary, _ = llm.chat(messages, role="companion", temperature=0.3, max_tokens=100)
            return summary.strip()
        except Exception:
            # Fallback: simple heuristic summary
            topics = []
            for ep in recent:
                words = ep.user_input.lower().split()
                for w in ["trade", "code", "file", "search", "fix", "config", "deploy", "test"]:
                    if w in " ".join(words):
                        topics.append(w)
            if topics:
                return f"Discussed: {', '.join(set(topics))}. {len(recent)} interactions completed."
            return f"Completed {len(recent)} interactions covering various tasks."
        """Temporal decay - recent episodes weighted higher."""
    def _decay_factor(self, episode: Episode) -> float:
        age_hours = (time.time() - episode.ts) / 3600
        # Half-life of ~7 days
        return math.exp(-age_hours / (7 * 24))

    def _importance_weight(self, episode: Episode) -> float:
        """Importance weighting based on access pattern and outcome."""
        base = episode.importance
        access_boost = min(episode.access_count * 0.1, 0.5)
        outcome_boost = 0.2 if episode.success else -0.1
        decay = self._decay_factor(episode)
        return (base + access_boost + outcome_boost) * decay

    def retrieve(self, query: str, k: int = 5,
                 include_failed: bool = False,
                 time_window_hours: float = None) -> List[Episode]:
        """Hybrid retrieval: semantic + keyword + temporal + outcome. Thread-safe."""
        with self._lock:
            if not self.episodes:
                return []

            query_embed = _embed(query)
            now = time.time()

            scored = []
            for ep in self.episodes:
                if time_window_hours:
                    age_hours = (now - ep.ts) / 3600
                    if age_hours > time_window_hours:
                        continue

                if not include_failed and not ep.success:
                    continue

                sem_score = 0.0
                if _EMBEDDINGS_AVAILABLE and ep.embedding and query_embed:
                    sem_score = _cosine_sim(query_embed, ep.embedding)

                kw_score = _keyword_score(query, ep.to_text())
                outcome_score = ep.outcome_score()
                temporal_weight = self._decay_factor(ep)
                importance = self._importance_weight(ep)

                combined = (
                    sem_score * 0.35 +
                    kw_score * 0.25 +
                    outcome_score * 0.20 +
                    importance * 0.20
                ) * temporal_weight

                if combined > 0.05:
                    scored.append((combined, ep))

            scored.sort(reverse=True, key=lambda x: x[0])
            top = scored[:k]

            # Track access counts (create copies to avoid mutation issues)
            result = []
            for _, ep in top:
                ep.access_count += 1
                ep.last_accessed = now
                result.append(ep)

            if result:
                self._save()
            return result

            if combined > 0.05:
                scored.append((combined, ep))

        scored.sort(reverse=True, key=lambda x: x[0])
        top = scored[:k]

        # Update access tracking
        for _, ep in top:
            ep.access_count += 1
            ep.last_accessed = time.time()

        self._save()
        return [ep for _, ep in top]

    def get_by_tags(self, tags: List[str], k: int = 10) -> List[Episode]:
        """Retrieve episodes by tag intersection."""
        with self._lock:
            scored = []
            for ep in self.episodes:
                if ep.tags:
                    overlap = len(set(ep.tags) & set(tags))
                    if overlap:
                        scored.append((overlap, ep))
            scored.sort(reverse=True)
            return [ep for _, ep in scored[:k]]

    def get_recent(self, n: int = 20) -> List[Episode]:
        with self._lock:
            return list(self.episodes[-n:])

    def get_failed_episodes(self, k: int = 10) -> List[Episode]:
        with self._lock:
            failed = [ep for ep in self.episodes if not ep.success]
            failed.sort(key=lambda x: x.ts, reverse=True)
            return list(failed[:k])

    def get_outcome_stats(self) -> Dict:
        """Statistics on episode outcomes."""
        with self._lock:
            total = len(self.episodes)
            if total == 0:
                return {"total": 0, "success_rate": 0.0}
            success = sum(1 for ep in self.episodes if ep.success)
            return {
                "total": total,
                "success": success,
                "failed": total - success,
                "success_rate": success / total,
                "avg_duration_ms": sum(ep.duration_ms for ep in self.episodes) / total,
            }


class Memory:
    """Enhanced Memory with episodic retrieval."""

    def __init__(self, data_dir: str, summarize_interval: int = 8,
                 llm_chat: Optional[Callable] = None):
        self.dir = data_dir
        os.makedirs(self.dir, exist_ok=True)
        self._lock = threading.Lock()
        self.short_term: List[Dict] = []
        self.long_term: Dict = {"facts": [], "preferences": {}}
        self.profile: Dict = {}
        self.summaries: List[str] = []
        self.working: Dict = {}
        self.working_max: int = int(os.environ.get("FRIDAY_WORKING_MAX", "40"))
        self.summarize_interval = summarize_interval
        self.llm_chat = llm_chat
        self._since_last_summary = 0
        self._conversation_topics: List[str] = []

        # Enhanced episodic memory
        self.episodic = EpisodicMemory(os.path.join(data_dir, "episodic"))

        self._load()
        self._embed_facts_cache()

    def _path(self, name):
        return os.path.join(self.dir, f"{name}.json")

    def _load(self):
        for name, default in [
            ("short_term", []), ("long_term", {"facts": [], "preferences": {}}),
            ("profile", {}), ("summaries", []),
        ]:
            try:
                with open(self._path(name), "r", encoding="utf-8") as f:
                    setattr(self, name, json.load(f))
            except Exception:
                pass

    def _save(self, name):
        try:
            with open(self._path(name), "w", encoding="utf-8") as f:
                json.dump(getattr(self, name), f, ensure_ascii=False, indent=2)
        except Exception as e:
            print(f"[memory] save {name} failed: {e}")

    def _embed_facts_cache(self):
        with self._lock:
            facts = self.long_term.get("facts", [])
            for f in facts:
                if isinstance(f, str):
                    if not hasattr(self, "_fact_embeddings"):
                        self._fact_embeddings = {}
                    if f not in self._fact_embeddings:
                        self._fact_embeddings[f] = _embed(f)

    def maybe_summarize(self):
        if self._since_last_summary >= self.summarize_interval:
            self.summarize_turns()
            self._since_last_summary = 0

    def summarize_turns(self):
        if not self.llm_chat or len(self.short_term) < 2:
            return
        turns_to_summarize = self.short_term[:self.summarize_interval]
        remaining = self.short_term[self.summarize_interval:]
        conversation_text = "\n\n".join(
            f"User: {t['user']}\nFriday: {t['assistant']}"
            for t in turns_to_summarize
        )
        prompt = (
            "You are Friday's memory. Compress into "
            "EXACTLY ONE short bullet point (<=40 words) capturing "
            "the single most useful fact/decision/preference. "
            "Example: '- User prefers concise replies, no bullet lists.'\n\n"
            f"{conversation_text}"
        )
        messages = [{"role": "user", "content": prompt}]
        try:
            summary, _ = self.llm_chat(messages)
            self.summaries.append(summary.strip())
            if len(self.summaries) > 200:
                self.summaries = self.summaries[-150:]
            self.short_term = remaining
            self._save("short_term")
            self._save("summaries")
        except Exception as e:
            print(f"[memory] summarize failed: {e}")

    def add_turn(self, user: str, assistant: str, language: str = "en",
                 tools_used: List[Dict] = None, tools_outcome: Dict = None,
                 success: bool = True, error: str = "",
                 duration_ms: float = 0.0, context_summary: str = "",
                 tags: List[str] = None):
        """Add turn AND store as episode for episodic retrieval."""
        with self._lock:
            turn = {
                "user": user, "assistant": assistant,
                "language": language, "ts": time.time(),
            }
            self.short_term.append(turn)
            self.short_term = self.short_term[-20:]
            self._since_last_summary += 1

            # Store as episode for episodic retrieval
            self.episodic.add_episode(
                user_input=user,
                assistant_response=assistant,
                tools_used=tools_used,
                tools_outcome=tools_outcome,
                success=success,
                error=error,
                duration_ms=duration_ms,
                context_summary=self._summarize_context(),
                tags=self._extract_tags(user)
            )

            user_lower = user.lower()
            if len(user_lower) > 5:
                words = [w for w in re.findall(r'\w+', user_lower) if len(w) > 3]
                if words:
                    self._conversation_topics.extend(words)
                    self._conversation_topics = self._conversation_topics[-50:]

            self.maybe_summarize()
            self._save("short_term")

    def _summarize_context(self) -> str:
        """Brief context summary for episode."""
        if not self.short_term:
            return ""
        recent = self.short_term[-3:]
        return "; ".join(f"{t['user'][:50]}" for t in recent)

    def _extract_tags(self, text: str) -> List[str]:
        """Extract relevant tags from user input."""
        tags = []
        t = text.lower()
        tag_keywords = {
            "trading": ["trade", "buy", "sell", "mt5", "forex", "eurusd"],
            "coding": ["code", "script", "python", "function", "debug"],
            "research": ["search", "find", "research", "look up"],
            "files": ["file", "read", "write", "delete", "list"],
            "system": ["time", "system", "info", "status"],
            "error": ["error", "failed", "bug", "issue", "problem"],
        }
        for tag, keywords in tag_keywords.items():
            if any(k in t for k in keywords):
                tags.append(tag)
        return tags

    def get_relevant_facts(self, query: str, k: int = 5) -> List[str]:
        facts = self.long_term.get("facts", [])
        if not facts:
            return []

        query_embed = _embed(query)
        scored = []
        for f in facts:
            if _EMBEDDINGS_AVAILABLE and hasattr(self, "_fact_embeddings"):
                f_emb = self._fact_embeddings.get(f)
                if f_emb:
                    semantic_score = _cosine_sim(query_embed, f_emb)
                else:
                    self._fact_embeddings[f] = _embed(f)
                    semantic_score = _cosine_sim(query_embed, self._fact_embeddings[f])
            else:
                semantic_score = 0.0
            kw_score = _keyword_score(query, f)
            combined = (semantic_score * 0.7) + (kw_score * 0.3)
            if combined > 0.1:
                scored.append((combined, f))

        for key, value in self.long_term.get("preferences", {}).items():
            fact = f"{key}: {value}"
            if _EMBEDDINGS_AVAILABLE:
                p_embed = _embed(fact)
                p_score = _cosine_sim(query_embed, p_embed)
            else:
                p_score = 0.0
            kw = _keyword_score(query, fact)
            combined = (p_score * 0.7) + (kw * 0.3)
            if combined > 0.1:
                scored.append((combined, fact))

        scored.sort(reverse=True)
        seen = set()
        unique = []
        for score, fact in scored:
            if fact not in seen:
                seen.add(fact)
                unique.append(fact)
                if len(unique) >= k:
                    break
        return unique

    def set_preference(self, key: str, value):
        with self._lock:
            self.long_term["preferences"][key] = value
            self._save("long_term")

    def learn_lesson(self, situation: str, fix: str):
        with self._lock:
            lesson = {"situation": situation, "fix": fix, "ts": time.time()}
            self.episodic.episodes.append(
                Episode(
                    id=hashlib.md5(f"{situation}{time.time()}".encode()).hexdigest()[:12],
                    ts=time.time(),
                    user_input=situation,
                    assistant_response=fix,
                    tools_used=[],
                    success=True,
                    tags=["lesson"],
                    context_summary=situation,
                )
            )
            self.episodic._save()

    def recent_lessons(self, n: int = 3) -> str:
        lessons = self.episodic.get_recent(n)
        if not lessons:
            return ""
        return "Lessons learned:\n" + "\n".join(
            f"  - {l.context_summary} -> {l.assistant_response}" for l in lessons)

    def get_relevant_lessons(self, query: str, k: int = 3) -> str:
        episodes = self.episodic.retrieve(query, k=k)
        if not episodes:
            return ""
        return "Relevant lessons:\n" + "\n".join(
            f"  - {e.user_input[:80]} -> {e.assistant_response[:80]}" for e in episodes)

    def get_relevant_history(self, query: str, k: int = 5) -> str:
        episodes = self.episodic.retrieve(query, k=k)
        if not episodes:
            return ""
        return "Relevant conversation history:\n" + "\n".join(
            f"User: {e.user_input[:100]}\nFriday: {e.assistant_response[:100]}" for e in episodes)

    def get_hybrid_context(self, query: str, fact_k: int = 5,
                           lesson_k: int = 3, history_k: int = 3) -> str:
        parts = []
        facts = self.get_relevant_facts(query, k=fact_k)
        if facts:
            parts.append("What I know about you:\n" + "\n".join(f"  - {f}" for f in facts))
        lessons = self.get_relevant_lessons(query, k=lesson_k)
        if lessons:
            parts.append(lessons)
        history = self.get_relevant_history(query, k=history_k)
        if history:
            parts.append(history)
        return "\n\n".join(parts)

    def remember_profile(self, key: str, value):
        with self._lock:
            self.profile[key] = value
            self._save("profile")

    def get_profile(self, key: str = None):
        with self._lock:
            if key is None:
                return dict(self.profile)
            return self.profile.get(key)

    def remember_user(self, attribute: str, value):
        with self._lock:
            self.long_term["preferences"][attribute] = value
            self._save("long_term")

    def note_working(self, key: str, value):
        with self._lock:
            if not hasattr(self, "working"):
                self.working = {}
            self.working[key] = value
            if len(self.working) > self.working_max:
                excess = len(self.working) - self.working_max
                for _ in range(excess):
                    self.working.pop(next(iter(self.working)))

    def get_working(self, key: str = None):
        if not hasattr(self, "working"):
            self.working = {}
        if key is None:
            return dict(self.working)
        return self.working.get(key)

    def get_conversation_topics(self) -> List[str]:
        with self._lock:
            return list(self._conversation_topics[-10:]) if hasattr(self, "_conversation_topics") else []

    def get_session_summary(self) -> str:
        parts = []
        with self._lock:
            if self.profile:
                profile_str = "; ".join(f"{k}={v}" for k, v in self.profile.items())
                parts.append(f"User profile: {profile_str}")
            recent = self.short_term[-3:]
            if recent:
                topics = set()
                for t in recent:
                    words = re.findall(r'\w+', t.get("user", "").lower())
                    topics.update(w for w in words if len(w) > 4)
                if topics:
                    parts.append(f"Recent topics: {', '.join(list(topics)[:5])}")
        return "\n".join(parts)

    def system_context(self, query: str = "") -> str:
        if query:
            return self.get_hybrid_context(query)
        parts = []
        if self.summaries:
            parts.append("Summary of our previous conversations:\n" + "\n".join(f"  - {s}" for s in self.summaries))
        facts = self.long_term["facts"][-5:]
        if facts:
            parts.append("What I know about you:\n" + "\n".join(f"  - {f}" for f in facts))
        lessons = self.recent_lessons()
        if lessons:
            parts.append(lessons)
        rc = self.recent_context()
        if rc:
            parts.append("Our recent conversation:\n" + rc)
        return "\n\n".join(parts)

    def recent_context(self, n: int = 6) -> str:
        turns = self.short_term[-n:]
        return "\n".join(f"User: {t['user']}\nFriday: {t['assistant']}"
                         for t in turns)

    def search_episodes(self, query: str, k: int = 5,
                        include_failed: bool = False,
                        hours_back: float = None) -> List[Dict]:
        """Search episodic memory - returns dict for API compatibility."""
        episodes = self.episodic.retrieve(query, k=k,
                                           include_failed=include_failed,
                                           time_window_hours=hours_back)
        return [{
            "id": e.id,
            "ts": e.ts,
            "user": e.user_input,
            "assistant": e.assistant_response,
            "tools": e.tools_used,
            "success": e.success,
            "error": e.error,
            "tags": e.tags,
        } for e in episodes]

    def get_episode_stats(self) -> Dict:
        return self.episodic.get_outcome_stats()