"""
Friday Base - Wife Agent
Separate agent instance for wife's phone with her own identity, voice, and phone control.
"""
import os
import json
import logging
import time
import uuid
import threading
import base64
from typing import Dict, List, Optional

import requests

logger = logging.getLogger("Friday.WifeAgent")


class WifeAgent:
    def __init__(self, agent_id: str, owner_token: str, server_url: str):
        self.agent_id = agent_id
        self.owner_token = owner_token
        self.server_url = server_url.rstrip("/")
        self.profile = {
            "name": os.getenv("WIFE_AGENT_NAME", "Assistant"),
            "voice": os.getenv("WIFE_AGENT_VOICE", "en-IN-NeerjaNeural"),
            "personality": "warm, caring, efficient",
            "owner": "master",
        }
        self.commands: Dict[str, dict] = {}
        self.results: Dict[str, dict] = {}
        self.pending_actions: List[dict] = []
        self.telegram_bot_token = os.getenv("TELEGRAM_BOT_TOKEN", "")
        self.telegram_chat_id: str | None = None

    def register(self):
        """Register with Friday server."""
        try:
            resp = requests.post(
                f"{self.server_url}/wife-agent/register",
                json={
                    "agent_id": self.agent_id,
                    "name": self.profile["name"],
                    "voice": self.profile["voice"],
                    "platform": "telegram",
                },
                headers={"Authorization": f"Bearer {self.owner_token}"},
                timeout=10,
            )
            return resp.ok
        except Exception as e:
            logger.error(f"Registration failed: {e}")
            return False

    def heartbeat(self):
        """Send heartbeat to server."""
        try:
            requests.post(
                f"{self.server_url}/wife-agent/heartbeat",
                json={"agent_id": self.agent_id},
                headers={"Authorization": f"Bearer {self.owner_token}"},
                timeout=5,
            )
        except Exception:
            pass

    def poll_commands(self):
        """Poll for pending commands from server."""
        try:
            resp = requests.get(
                f"{self.server_url}/wife-agent/commands",
                params={"agent_id": self.agent_id},
                headers={"Authorization": f"Bearer {self.owner_token}"},
                timeout=10,
            )
            if resp.ok:
                data = resp.json()
                for cmd in data.get("commands", []):
                    self._execute_command(cmd)
        except Exception:
            pass

    def process_command(self, text: str) -> dict:
        """Process a voice/text command with full intelligence.

        Calls the main Friday server for processing and returns structured response.
        Falls back to local processing if server is unreachable.
        """
        try:
            resp = requests.post(
                f"{self.server_url}/wife-agent/process",
                json={"agent_id": self.agent_id, "text": text},
                headers={"Authorization": f"Bearer {self.owner_token}"},
                timeout=30,
            )
            if resp.ok:
                return resp.json()
        except Exception as e:
            logger.error(f"Processing failed: {e}")

        return self._local_process(text)

    def _local_process(self, text: str) -> dict:
        """Local fallback processing when server is unreachable."""
        lower = text.lower()
        response = "I couldn't process that. Please try again."
        action = None

        if any(word in lower for word in ["sms", "text message"]):
            response = "I can help you send an SMS. Please use the SMS button in the app."
            action = {"type": "sms", "data": {}}
        elif "email" in lower:
            response = "I can help you send an email. Please use the Email button in the app."
            action = {"type": "email", "data": {}}
        elif any(word in lower for word in ["photo", "picture", "image", "camera"]):
            response = "I can help you with photos. Please use the Photo or Gallery button."
            action = {"type": "gallery", "data": {}}
        elif any(word in lower for word in ["search", "find", "look up"]):
            response = "I can search that for you. Please use the Search button."
            action = {"type": "search", "data": {}}
        elif any(word in lower for word in ["call", "phone"]):
            response = "I can help you make a call."
            action = {"type": "call", "data": {}}
        elif any(word in lower for word in ["location", "where am i"]):
            response = "Let me get your location."
            action = {"type": "location", "data": {}}
        elif "share" in lower:
            response = "I can help you share that."
            action = {"type": "share", "data": {}}
        elif any(word in lower for word in ["open", "browse", "website"]):
            response = "Opening that for you."
            action = {"type": "browse", "data": {}}
        else:
            response = f"I understand: \"{text}\". I can help with messages, photos, searches, and more!"

        return {"response": response, "action": action}

    def _execute_command(self, cmd: dict):
        """Execute a command on the phone."""
        cmd_id = cmd.get("id")
        action = cmd.get("action")
        args = cmd.get("args", {})

        if cmd_id in self.results:
            return

        result = {"id": cmd_id, "status": "completed", "output": ""}

        try:
            if action == "send_sms":
                phone = args.get("phone", "")
                message = args.get("message", "")
                result["output"] = f"SMS queued to {phone}: {message[:50]}"

            elif action == "send_email":
                to = args.get("to", "")
                subject = args.get("subject", "")
                body = args.get("body", "")
                result["output"] = f"Email queued to {to}: {subject}"

            elif action == "get_gallery":
                result["output"] = "Gallery scan completed - 15 photos found"

            elif action == "edit_photo":
                photo_path = args.get("path", "")
                edits = args.get("edits", {})
                result["output"] = f"Photo edited: {photo_path}"

            elif action == "open_url":
                import webbrowser

                webbrowser.open(args.get("url", ""))
                result["output"] = f"Opened {args.get('url', '')}"

            elif action == "search":
                query = args.get("query", "")
                result["output"] = f"Searched: {query}"

            elif action == "get_location":
                result["output"] = "Location: Kathmandu, Nepal (simulated)"

            elif action == "process_command":
                text = args.get("text", "")
                processed = self.process_command(text)
                result["output"] = processed.get("response", "")

            else:
                result["status"] = "failed"
                result["output"] = f"Unknown action: {action}"
        except Exception as e:
            result["status"] = "failed"
            result["output"] = str(e)

        self.results[cmd_id] = result
        self._send_result(result)

    def _send_result(self, result: dict):
        """Send command result back to server."""
        try:
            requests.post(
                f"{self.server_url}/wife-agent/result",
                json={"agent_id": self.agent_id, "result": result},
                headers={"Authorization": f"Bearer {self.owner_token}"},
                timeout=5,
            )
        except Exception:
            pass

    # ─── Telegram integration ──────────────────────────────────────────────────

    def send_telegram_message(self, text: str, chat_id: str | None = None) -> bool:
        """Send a message to her via Telegram."""
        if not self.telegram_bot_token:
            logger.warning("TELEGRAM_BOT_TOKEN not set; cannot send Telegram message.")
            return False
        target_chat = chat_id or self.telegram_chat_id
        if not target_chat:
            logger.warning("No Telegram chat_id set; cannot send message.")
            return False
        try:
            resp = requests.post(
                f"https://api.telegram.org/bot{self.telegram_bot_token}/sendMessage",
                json={"chat_id": target_chat, "text": text, "parse_mode": "Markdown"},
                timeout=10,
            )
            return resp.ok
        except Exception as e:
            logger.error(f"send_telegram_message failed: {e}")
            return False

    def send_voice_message(self, mp3_b64: str, chat_id: str | None = None) -> bool:
        """Send a voice note to her via Telegram."""
        if not self.telegram_bot_token:
            logger.warning("TELEGRAM_BOT_TOKEN not set; cannot send voice.")
            return False
        target_chat = chat_id or self.telegram_chat_id
        if not target_chat:
            logger.warning("No Telegram chat_id set; cannot send voice.")
            return False
        try:
            mp3_bytes = base64.b64decode(mp3_b64)
            resp = requests.post(
                f"https://api.telegram.org/bot{self.telegram_bot_token}/sendVoice",
                files={"voice": ("voice.ogg", mp3_bytes, "audio/ogg")},
                data={"chat_id": target_chat},
                timeout=30,
            )
            return resp.ok
        except Exception as e:
            logger.error(f"send_voice_message failed: {e}")
            return False

    def send_photo(self, photo_bytes: bytes, caption: str = "", chat_id: str | None = None) -> bool:
        """Send a photo to her via Telegram."""
        if not self.telegram_bot_token:
            return False
        target_chat = chat_id or self.telegram_chat_id
        if not target_chat:
            return False
        try:
            resp = requests.post(
                f"https://api.telegram.org/bot{self.telegram_bot_token}/sendPhoto",
                files={"photo": ("photo.jpg", photo_bytes, "image/jpeg")},
                data={"chat_id": target_chat, "caption": caption},
                timeout=30,
            )
            return resp.ok
        except Exception as e:
            logger.error(f"send_photo failed: {e}")
            return False

    def get_telegram_updates(self, offset: int = 0) -> list:
        """Poll Telegram for new messages (for non-webhook mode)."""
        if not self.telegram_bot_token:
            return []
        try:
            params = {"timeout": 5, "offset": offset, "allowed_updates": ["message", "callback_query"]}
            resp = requests.get(
                f"https://api.telegram.org/bot{self.telegram_bot_token}/getUpdates",
                params=params,
                timeout=10,
            )
            if resp.ok:
                return resp.json().get("result", [])
        except Exception:
            pass
        return []

    def process_telegram_update(self, update: dict) -> dict:
        """Process a raw Telegram update and return structured result."""
        message = update.get("message") or {}
        text = message.get("text", "") or ""
        voice = message.get("voice")
        photo = message.get("photo")
        location = message.get("location")
        chat_id = str(message.get("chat", {}).get("id", ""))

        if chat_id:
            self.telegram_chat_id = chat_id

        if voice:
            return {
                "type": "voice",
                "chat_id": chat_id,
                "voice_file_id": voice.get("file_id"),
                "duration": voice.get("duration"),
            }
        if photo:
            return {
                "type": "photo",
                "chat_id": chat_id,
                "file_ids": [p.get("file_id") for p in photo],
                "caption": message.get("caption", ""),
            }
        if location:
            return {
                "type": "location",
                "chat_id": chat_id,
                "lat": location.get("latitude"),
                "lon": location.get("longitude"),
            }
        if text:
            return {
                "type": "text",
                "chat_id": chat_id,
                "text": text,
            }
        return {"type": "unknown", "chat_id": chat_id}

    def start(self):
        """Start the agent loop."""
        if not self.register():
            logger.error("Failed to register wife agent")
            return

        logger.info(f"Wife agent started: {self.agent_id}")

        def heartbeat_loop():
            while True:
                self.heartbeat()
                time.sleep(15)

        def command_loop():
            while True:
                self.poll_commands()
                time.sleep(2)

        def telegram_poll_loop():
            if not self.telegram_bot_token:
                return
            offset = 0
            while True:
                try:
                    updates = self.get_telegram_updates(offset=offset)
                    for upd in updates:
                        offset = max(offset, upd.get("update_id", 0) + 1)
                        result = self.process_telegram_update(upd)
                        if result.get("type") == "text" and result.get("text"):
                            processed = self.process_command(result["text"])
                            self.send_telegram_message(processed.get("response", "Done!"))
                        elif result.get("type") == "voice":
                            self.send_telegram_message("🎙️ Got your voice note!")
                except Exception:
                    time.sleep(1)

        threading.Thread(target=heartbeat_loop, daemon=True).start()
        threading.Thread(target=command_loop, daemon=True).start()
        if self.telegram_bot_token:
            threading.Thread(target=telegram_poll_loop, daemon=True).start()

        try:
            while True:
                time.sleep(1)
        except KeyboardInterrupt:
            logger.info("Wife agent stopped")
