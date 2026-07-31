"""
Friday Base - Proactive Notifications and Announcements
"""
import logging
import threading
import time
import uuid
from dataclasses import dataclass
from enum import Enum
from typing import List, Optional

logger = logging.getLogger("Friday.Proactive")


class NotificationType(Enum):
    INFO = "info"
    SUCCESS = "success"
    WARNING = "warning"
    QUESTION = "question"


_PRIORITY_MAP = {
    NotificationType.WARNING: 0,
    NotificationType.QUESTION: 1,
    NotificationType.INFO: 2,
    NotificationType.SUCCESS: 3,
}


@dataclass(order=False)
class Notification:
    id: str
    type: NotificationType
    message: str
    timestamp: float
    requires_acknowledgment: bool = False
    acknowledged: bool = False

    def priority_key(self):
        return (_PRIORITY_MAP.get(self.type, 99), self.timestamp)


class ProactiveNotifier:
    def __init__(self, voice_enabled: bool = True):
        self._lock = threading.Lock()
        self._condition = threading.Condition(self._lock)
        self._pending: List[Notification] = []
        self._ack_events: dict = {}
        self._voice_enabled = voice_enabled

    def _speak(self, message: str):
        try:
            from .voice import synthesize
            audio = synthesize(message)
            if audio:
                logger.debug("Voice synthesis completed for notification")
        except Exception as e:
            logger.warning("Voice synthesis failed: %s", e)

    def announce(self, message: str, notification_type: str = "info") -> Notification:
        notif_type = NotificationType(notification_type.lower())
        notif = Notification(
            id=str(uuid.uuid4()),
            type=notif_type,
            message=message,
            timestamp=time.time(),
            requires_acknowledgment=(notif_type == NotificationType.QUESTION),
            acknowledged=False,
        )
        with self._condition:
            self._pending.append(notif)
            self._pending.sort(key=lambda n: n.priority_key())
            self._condition.notify_all()

        logger.info("Announcement [%s]: %s", notif_type.value, message)

        if self._voice_enabled:
            threading.Thread(target=self._speak, args=(message,), daemon=True).start()

        return notif

    def get_pending_notifications(self) -> List[dict]:
        with self._lock:
            return [
                {
                    "id": n.id,
                    "type": n.type.value,
                    "message": n.message,
                    "timestamp": n.timestamp,
                    "requires_acknowledgment": n.requires_acknowledgment,
                    "acknowledged": n.acknowledged,
                }
                for n in self._pending
            ]

    def acknowledge_notification(self, notification_id: str) -> bool:
        with self._condition:
            for n in self._pending:
                if n.id == notification_id:
                    n.acknowledged = True
                    ev = self._ack_events.pop(notification_id, None)
                    self._condition.notify_all()
                    if ev:
                        ev.set()
                    logger.info("Notification acknowledged: %s", notification_id)
                    return True
            return False

    def wait_for_acknowledgment(self, notification_id: str, timeout: float = 60) -> bool:
        ev = threading.Event()
        with self._condition:
            self._ack_events[notification_id] = ev
            for n in self._pending:
                if n.id == notification_id and n.acknowledged:
                    ev.set()
                    break
        return ev.wait(timeout)


_notifier = ProactiveNotifier()


def announce(message: str, notification_type: str = "info") -> dict:
    n = _notifier.announce(message, notification_type)
    return {
        "id": n.id,
        "type": n.type.value,
        "message": n.message,
        "timestamp": n.timestamp,
        "requires_acknowledgment": n.requires_acknowledgment,
    }


def get_pending_notifications() -> List[dict]:
    return _notifier.get_pending_notifications()


def acknowledge_notification(notification_id: str) -> bool:
    return _notifier.acknowledge_notification(notification_id)


def wait_for_acknowledgment(notification_id: str, timeout: float = 60) -> bool:
    return _notifier.wait_for_acknowledgment(notification_id, timeout)
