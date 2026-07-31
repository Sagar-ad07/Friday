"""
Friday Base - Local Agent
Runs on user's device, connects to Friday server, executes commands with approval.
Uses /device/* endpoints (the native phone control path).
"""
import asyncio
import json
import logging
import os
import platform
import socket
import subprocess
import sys
import threading
import time
import uuid
from typing import Dict, List, Optional

import requests

logger = logging.getLogger("Friday.Agent")

SERVER_URL = os.getenv("FRIDAY_SERVER_URL", "")
AGENT_TOKEN = os.getenv("FRIDAY_TOKEN", "")
AGENT_ID = os.getenv("FRIDAY_AGENT_ID", socket.gethostname() + "_" + uuid.uuid4().hex[:8])
HEARTBEAT_INTERVAL = 15


class LocalAgent:
    def __init__(self):
        self.running = False
        self.commands: Dict[str, dict] = {}
        self.results: Dict[str, dict] = {}
        self.pending_approval: Dict[str, dict] = {}

    def start(self):
        if not SERVER_URL or not AGENT_TOKEN:
            logger.error("FRIDAY_SERVER_URL and FRIDAY_TOKEN required")
            return
        self.running = True
        threading.Thread(target=self._register, daemon=True).start()
        threading.Thread(target=self._heartbeat_loop, daemon=True).start()
        threading.Thread(target=self._poll_commands, daemon=True).start()

    def _headers(self):
        return {"Authorization": f"Bearer {AGENT_TOKEN}"}

    def _device_url(self, path: str) -> str:
        return f"{SERVER_URL}/device/{AGENT_ID}{path}"

    def _device_base(self) -> str:
        return f"{SERVER_URL}/device"

    def _register(self):
        try:
            resp = requests.post(
                f"{self._device_base()}/register",
                json={"device_id": AGENT_ID, "info": {
                    "hostname": socket.gethostname(),
                    "platform": platform.system(),
                    "agent": "friday-local",
                }},
                headers=self._headers(),
                timeout=10,
            )
            if resp.ok:
                logger.info(f"Agent registered: {AGENT_ID}")
            else:
                logger.error(f"Registration failed: {resp.status_code} {resp.text}")
        except Exception as e:
            logger.error(f"Registration error: {e}")

    def _heartbeat_loop(self):
        while self.running:
            try:
                requests.get(
                    f"{self._device_base()}/{AGENT_ID}/status",
                    headers=self._headers(),
                    timeout=5,
                )
            except Exception:
                pass
            time.sleep(HEARTBEAT_INTERVAL)

    def _poll_commands(self):
        while self.running:
            try:
                resp = requests.get(
                    f"{self._device_base()}/{AGENT_ID}/commands",
                    headers=self._headers(),
                    timeout=10,
                )
                if resp.ok:
                    data = resp.json()
                    for cmd in data.get("commands", []):
                        self._execute_command(cmd)
            except Exception:
                pass
            time.sleep(2)

    def _execute_command(self, cmd: dict):
        cmd_id = cmd.get("id")
        command = cmd.get("command", {})
        action = command.get("action")
        args = command.get("args", {})

        if cmd_id in self.results:
            return

        if cmd.get("requires_approval", False):
            self.pending_approval[cmd_id] = cmd
            if action in ["open_url", "get_time", "get_system_info"]:
                pass
            else:
                logger.info(f"Command {cmd_id} requires approval: {action}")
                return

        result = {"id": cmd_id, "status": "completed", "output": ""}

        try:
            if action == "open_url":
                import webbrowser
                webbrowser.open(args.get("url", ""))
                result["output"] = f"Opened {args.get('url', '')}"

            elif action == "run_command":
                cmd_str = args.get("command", "")
                timeout = args.get("timeout", 30)
                r = subprocess.run(
                    cmd_str,
                    shell=True,
                    capture_output=True,
                    text=True,
                    timeout=timeout,
                    cwd=os.path.expanduser("~"),
                )
                result["output"] = (r.stdout or "") + (r.stderr or "")
                result["returncode"] = r.returncode

            elif action == "open_app":
                app = args.get("app", "")
                if platform.system() == "Windows":
                    os.startfile(app)
                elif platform.system() == "Darwin":
                    subprocess.run(["open", app])
                else:
                    subprocess.run(["xdg-open", app])
                result["output"] = f"Opened {app}"

            elif action == "type_text":
                try:
                    import pyautogui
                    text = args.get("text", "")
                    pyautogui.typewrite(text, interval=0.05)
                    result["output"] = f"Typed: {text[:50]}"
                except ImportError:
                    result["status"] = "failed"
                    result["output"] = "pyautogui not installed"

            elif action == "screenshot":
                try:
                    import pyautogui
                    img = pyautogui.screenshot()
                    path = os.path.join(os.path.expanduser("~"), "friday_screenshot.png")
                    img.save(path)
                    result["output"] = path
                except ImportError:
                    result["status"] = "failed"
                    result["output"] = "pyautogui not installed"

            else:
                result["status"] = "failed"
                result["output"] = f"Unknown action: {action}"
        except Exception as e:
            result["status"] = "failed"
            result["output"] = str(e)

        self.results[cmd_id] = result
        self._send_result(result)

    def _send_result(self, result: dict):
        try:
            requests.post(
                f"{self._device_base()}/{AGENT_ID}/result",
                json={"command_id": result["id"], "result": result},
                headers=self._headers(),
                timeout=5,
            )
        except Exception:
            pass


def main():
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(name)s] %(message)s")
    agent = LocalAgent()
    agent.start()
    logger.info(f"Friday Agent started: {AGENT_ID}")
    try:
        while True:
            time.sleep(1)
    except KeyboardInterrupt:
        agent.running = False
        logger.info("Agent stopped")


if __name__ == "__main__":
    main()
