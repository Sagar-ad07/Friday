"""
Friday Base - Device Control
Backend APIs for controlling devices through local agents.
"""
import os
import json
import logging
import time
import uuid
from typing import Dict, List, Optional
from datetime import datetime

logger = logging.getLogger("Friday.DeviceControl")


class DeviceController:
    def __init__(self):
        self.devices: Dict[str, dict] = {}
        self.commands: Dict[str, dict] = {}
        self.screenshots: Dict[str, str] = {}

    def register_device(self, device_id: str, device_info: dict) -> dict:
        self.devices[device_id] = {
            "info": device_info,
            "last_seen": time.time(),
            "online": True,
            "pending_commands": [],
        }
        return {"status": "registered", "device_id": device_id}

    def get_device_status(self, device_id: str) -> dict:
        if device_id not in self.devices:
            return {"online": False, "error": "device not found"}
        dev = self.devices[device_id]
        return {
            "online": dev["online"],
            "last_seen": dev["last_seen"],
            "info": dev["info"],
        }

    def send_command(self, device_id: str, command: dict) -> dict:
        if device_id not in self.devices:
            return {"status": "failed", "error": "device not found"}
        cmd_id = uuid.uuid4().hex[:8]
        cmd = {
            "id": cmd_id,
            "command": command,
            "timestamp": time.time(),
            "status": "pending",
        }
        self.commands[cmd_id] = cmd
        self.devices[device_id]["pending_commands"].append(cmd_id)
        return {"status": "queued", "command_id": cmd_id}

    def get_pending_commands(self, device_id: str) -> list:
        if device_id not in self.devices:
            return []
        dev = self.devices[device_id]
        pending = []
        for cmd_id in dev["pending_commands"]:
            if cmd_id in self.commands:
                cmd = self.commands[cmd_id]
                if cmd["status"] == "pending":
                    pending.append(cmd)
        return pending

    def mark_command_done(self, cmd_id: str, result: dict):
        if cmd_id in self.commands:
            self.commands[cmd_id]["status"] = "completed"
            self.commands[cmd_id]["result"] = result

    def report_screenshot(self, device_id: str, image_b64: str):
        self.screenshots[device_id] = {
            "data": image_b64,
            "timestamp": time.time(),
        }


device_controller = DeviceController()
