"""
Friday Base - Resilience: circuit breaker + safe wrapper.
Makes Friday 'never crash': bad providers get isolated, safe() returns an English
fallback instead of raising.
"""
import time
import threading
import logging

logger = logging.getLogger("Friday.Resilience")

EN_FALLBACK = "My language providers are briefly rate-limited (free-tier quota). Give me ~30s and try again — I'll answer then."
NE_FALLBACK = EN_FALLBACK


class CircuitBreaker:
    def __init__(self, threshold=10, cooldown=30):
        self.threshold = threshold
        self.cooldown = cooldown
        self._fail = {}
        self._open_until = {}
        self._lock = threading.Lock()

    def is_open(self, key):
        with self._lock:
            until = self._open_until.get(key, 0)
            if until and time.time() < until:
                return True
            if until and time.time() >= until:
                self._open_until[key] = 0
                self._fail[key] = 0
            return False

    def record_success(self, key):
        with self._lock:
            self._fail[key] = 0
            self._open_until[key] = 0

    def record_failure(self, key):
        with self._lock:
            self._fail[key] = self._fail.get(key, 0) + 1
            if self._fail[key] >= self.threshold:
                self._open_until[key] = time.time() + self.cooldown
                logger.warning("breaker OPEN for %s (%ds)", key, self.cooldown)

    def status(self):
        keys = set(list(self._fail.keys()) + list(self._open_until.keys()))
        return {k: ("open" if self.is_open(k) else "closed") for k in keys}


try:
    from .config import config
    breaker = CircuitBreaker(config.breaker_threshold, config.breaker_cooldown)
except Exception:
    breaker = CircuitBreaker()


def safe(fn, *args, fallback=NE_FALLBACK, **kwargs):
    try:
        return fn(*args, **kwargs)
    except Exception as e:
        logger.error("safe() caught: %s", e)
        return fallback
