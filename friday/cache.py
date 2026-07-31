"""
Friday Base - Local answer cache
Zero-cost repeat handling: exact + normalized-key cache of recent Q/A plus a
cheap token-overlap "semantic-ish" match so repeats/cached facts cost 0 calls.
No API, no network. TTL via config.local_cache_ttl.
"""
import hashlib
import math
import os
import threading
import time


def _normalize(text: str) -> str:
    """Lowercase, strip punctuation, collapse whitespace -> stable key."""
    s = (text or "").lower()
    out = []
    for ch in s:
        if ch.isalnum() or ch.isspace():
            out.append(ch)
    return " ".join("".join(out).split())


def _norm_key(text: str) -> str:
    return _normalize(text)


def _fuzzy_key(text: str) -> str:
    """Loose key: sorted set of significant tokens (len >= 3) so word-order-
    insensitive queries hit the cache."""
    toks = [t for t in _normalize(text).split() if len(t) >= 3]
    return " ".join(sorted(set(toks)))


class AnswerCache:
    def __init__(self, ttl: float = 600, max_items: int = 200):
        self.ttl = ttl
        self.max_items = max_items
        self._lock = threading.Lock()
        self._store: dict = {}   # norm_key -> (ts, answer)
        self._fuzzy: dict = {}    # fuzzy_key -> norm_key

    def set_ttl(self, ttl: float):
        self.ttl = ttl

    def get(self, text: str):
        if self.ttl <= 0:
            return None
        nk = _norm_key(text)
        with self._lock:
            ent = self._store.get(nk)
            if ent and (time.time() - ent[0]) < self.ttl:
                return ent[1]
            if ent:
                self._store.pop(nk, None)
            # Loose fallback: token-overlap match against stored fuzzy keys.
            # Only used for longer queries (>= 4 significant tokens) so short
            # queries like "what is 2+2" don't falsely match "what time is it".
            fk = _fuzzy_key(text)
            if not fk or len(fk.split()) < 4:
                return None
            best, best_score = None, 0.0
            for stored_fk, target_nk in self._fuzzy.items():
                ent2 = self._store.get(target_nk)
                if not ent2 or (time.time() - ent2[0]) >= self.ttl:
                    continue
                a = set(fk.split())
                b = set(stored_fk.split())
                if not a or not b:
                    continue
                score = len(a & b) / math.sqrt(len(a) * len(b))
                if score >= 0.75 and score > best_score:
                    best, best_score = ent2[1], score
            return best

    def put(self, text: str, answer: str):
        if self.ttl <= 0 or not answer:
            return
        nk = _norm_key(text)
        fk = _fuzzy_key(text)
        with self._lock:
            self._store[nk] = (time.time(), answer)
            if fk:
                self._fuzzy[fk] = nk
            if len(self._store) > self.max_items:
                # Drop the oldest.
                oldest = min(self._store.items(), key=lambda kv: kv[1][0])
                self._store.pop(oldest[0], None)

    def clear(self):
        with self._lock:
            self._store.clear()
            self._fuzzy.clear()


_cache = None


def get_cache() -> AnswerCache:
    global _cache
    if _cache is None:
        from .config import config
        _cache = AnswerCache(ttl=getattr(config, "local_cache_ttl", 600))
    return _cache
